//go:build linux

package platform

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// The Linux implementation exists so the platform boundary is exercised from day one and
// so the scanner can be tested against a real file system in the Linux dev container.

// Name identifies the platform in recipe files ([paths] linux = ...).
const Name = "linux"

type linuxDirectoryReader struct{}

// NewDirectoryReader returns the native directory reader.
func NewDirectoryReader() DirectoryReader {
	return linuxDirectoryReader{}
}

func (linuxDirectoryReader) ReadDirectory(path string) ([]DirectoryEntry, error) {
	directory, err := os.Open(path)
	if err != nil {
		return nil, classifyOpenError(path, err)
	}
	defer directory.Close()

	var directoryStat unix.Stat_t
	if err := unix.Fstat(int(directory.Fd()), &directoryStat); err != nil {
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}
	names, err := directory.Readdirnames(-1)
	if err != nil {
		return nil, classifyOpenError(path, err)
	}

	entries := make([]DirectoryEntry, 0, len(names))
	for _, name := range names {
		var stat unix.Stat_t
		statErr := unix.Fstatat(int(directory.Fd()), name, &stat, unix.AT_SYMLINK_NOFOLLOW)
		if statErr != nil {
			// The entry vanished between listing and stat; it no longer uses space.
			continue
		}
		entry := entryFromStat(path, name, &stat, directoryStat.Dev)
		entry.CreatedUnixNanos = birthTime(int(directory.Fd()), name)
		entries = append(entries, entry)
	}
	return entries, nil
}

// birthTime returns the creation time where the file system records one (ext4, btrfs,
// xfs with statx support), or 0.
func birthTime(directoryDescriptor int, name string) int64 {
	var extended unix.Statx_t
	err := unix.Statx(directoryDescriptor, name, unix.AT_SYMLINK_NOFOLLOW, unix.STATX_BTIME,
		&extended)
	if err != nil || extended.Mask&unix.STATX_BTIME == 0 {
		return 0
	}
	return extended.Btime.Sec*1_000_000_000 + int64(extended.Btime.Nsec)
}

func entryFromStat(parent, name string, stat *unix.Stat_t, parentDevice uint64) DirectoryEntry {
	entry := DirectoryEntry{
		Name:              name,
		AllocatedBytes:    stat.Blocks * 512,
		LogicalBytes:      stat.Size,
		ModifiedUnixNanos: stat.Mtim.Nano(),
		ID:                FileID{Volume: stat.Dev, Low: stat.Ino},
		LinkCount:         uint32(stat.Nlink),
	}
	switch stat.Mode & unix.S_IFMT {
	case unix.S_IFDIR:
		entry.Kind = EntryDirectory
		if stat.Dev != parentDevice {
			// A mount point of another file system: recorded, never traversed, the same
			// way junctions to other volumes are handled on Windows.
			entry.Kind = EntryLink
			entry.LinkTarget = "(mount point)"
			entry.AllocatedBytes = 0
			entry.LogicalBytes = 0
		}
	case unix.S_IFLNK:
		entry.Kind = EntryLink
		entry.AllocatedBytes = 0
		entry.LogicalBytes = 0
		if target, err := os.Readlink(filepath.Join(parent, name)); err == nil {
			entry.LinkTarget = target
		}
	default:
		entry.Kind = EntryFile
		if entry.AllocatedBytes < entry.LogicalBytes {
			entry.Attributes |= AttributeSparse
		}
	}
	return entry
}

func classifyOpenError(path string, err error) error {
	if errors.Is(err, os.ErrPermission) {
		return &AccessDeniedError{Path: path, Cause: err}
	}
	return err
}

// DescribeRoot returns identity and file system information for a scan root.
func DescribeRoot(path string) (RootInfo, error) {
	var stat unix.Stat_t
	if err := unix.Stat(path, &stat); err != nil {
		return RootInfo{}, classifyOpenError(path, err)
	}
	return RootInfo{
		ID:                FileID{Volume: stat.Dev, Low: stat.Ino},
		FileSystem:        fileSystemOf(path),
		ModifiedUnixNanos: stat.Mtim.Nano(),
		CreatedUnixNanos:  birthTime(unix.AT_FDCWD, path),
	}, nil
}

var realFileSystems = map[string]bool{
	"ext2": true, "ext3": true, "ext4": true, "xfs": true, "btrfs": true, "zfs": true,
	"vfat": true, "exfat": true, "ntfs": true, "ntfs3": true, "f2fs": true,
}

type mountEntry struct{ mountPoint, fileSystem string }

func readMounts() []mountEntry {
	file, err := os.Open("/proc/self/mounts")
	if err != nil {
		return nil
	}
	defer file.Close()
	var mounts []mountEntry
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 3 {
			mountPoint := strings.ReplaceAll(fields[1], `\040`, " ")
			mounts = append(mounts, mountEntry{mountPoint: mountPoint, fileSystem: fields[2]})
		}
	}
	return mounts
}

// mountOf returns the mount that contains path: the longest mount point that is path
// itself or one of its parent folders.
func mountOf(path string) mountEntry {
	best := mountEntry{mountPoint: "/"}
	for _, mount := range readMounts() {
		point := mount.mountPoint
		inside := path == point || point == "/" ||
			strings.HasPrefix(path, strings.TrimSuffix(point, "/")+"/")
		if inside && len(point) >= len(best.mountPoint) {
			best = mount
		}
	}
	return best
}

func fileSystemOf(path string) string {
	return mountOf(path).fileSystem
}

// ListVolumes lists mounted local file systems.
func ListVolumes() ([]Volume, error) {
	var volumes []Volume
	for _, mount := range readMounts() {
		if !realFileSystems[mount.fileSystem] {
			continue
		}
		var stat unix.Statfs_t
		if err := unix.Statfs(mount.mountPoint, &stat); err != nil {
			continue
		}
		blockSize := int64(stat.Bsize)
		volumes = append(volumes, Volume{
			RootPath:    mount.mountPoint,
			DisplayName: mount.mountPoint,
			FileSystem:  mount.fileSystem,
			Kind:        "Local disk",
			TotalBytes:  int64(stat.Blocks) * blockSize,
			FreeBytes:   int64(stat.Bavail) * blockSize,
		})
	}
	return volumes, nil
}

// VolumeFor returns the volume containing path.
func VolumeFor(path string) (Volume, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return Volume{}, err
	}
	blockSize := int64(stat.Bsize)
	mount := mountOf(filepath.Clean(path))
	return Volume{
		RootPath:    mount.mountPoint,
		DisplayName: mount.mountPoint,
		FileSystem:  mount.fileSystem,
		TotalBytes:  int64(stat.Blocks) * blockSize,
		FreeBytes:   int64(stat.Bavail) * blockSize,
	}, nil
}

// IsElevated reports whether the process runs as root.
func IsElevated() bool {
	return os.Geteuid() == 0
}

// EnableBackupPrivilege is a Windows concept; root already bypasses permissions.
func EnableBackupPrivilege() bool { return IsElevated() }

// BeginBackgroundMode lowers I/O priority for the scan; restore undoes it.
func BeginBackgroundMode() (restore func()) {
	return func() {}
}

// DiscoverKnownLocations returns variable values used by recipe patterns.
func DiscoverKnownLocations() KnownLocations {
	home, _ := os.UserHomeDir()
	profiles := []string{}
	if matches, err := filepath.Glob("/home/*"); err == nil {
		profiles = append(profiles, matches...)
	}
	profiles = append(profiles, "/root")
	if home != "" && !containsString(profiles, home) {
		profiles = append(profiles, home)
	}
	return KnownLocations{
		Variables: map[string][]string{
			"CurrentUserProfile": {home},
			"UserProfile":        profiles,
			"ProfilesDirectory":  {"/home"},
			"AnyVolumeRoot":      {"/"},
		},
		CaseInsensitive: false,
		PathSeparator:   '/',
	}
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

// InstalledApplications is not implemented on Linux yet.
func InstalledApplications() ([]InstalledApplication, error) {
	return nil, nil
}

// OpenBrowser opens url in the default browser. Only ever called with the local URL.
func OpenBrowser(url string) error {
	return exec.Command("xdg-open", url).Start()
}

// RelaunchElevated is not supported on Linux.
func RelaunchElevated(arguments []string) error {
	return errors.New("relaunching as root is not supported; run with sudo")
}

// ConfigDirectory returns the directory for config, rules and history.
func ConfigDirectory() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "ullage"), nil
}

// LogDirectory returns the directory for log files.
func LogDirectory() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "ullage", "logs"), nil
}

// UserName and MachineName feed the redaction rules (Phase 2).
func UserName() string {
	return os.Getenv("USER")
}

// MachineName returns the host name.
func MachineName() string {
	name, _ := os.Hostname()
	return name
}
