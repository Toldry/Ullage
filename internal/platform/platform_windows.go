//go:build windows

package platform

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Name identifies the platform in recipe files ([paths] windows = ...).
const Name = "windows"

// Information classes for GetFileInformationByHandleEx, from FILE_INFO_BY_HANDLE_CLASS.
const (
	fileIDBothDirectoryInfo  = 0x0A
	fileIDInfo               = 0x12
	fileIDExtdDirectoryInfo  = 0x13
	ioReparseTagMountPoint   = 0xA0000003
	ioReparseTagSymlink      = 0xA000000C
	ioReparseTagWof          = 0x80000017
	ioReparseTagDedup        = 0x80000013
	fileAttributeRecallOnOpn = 0x00040000
	fileAttributeRecallOnDA  = 0x00400000
	directoryBufferBytes     = 64 * 1024
	windowsToUnixEpochTicks  = 116444736000000000
	processModeBackgroundBeg = 0x00100000
	processModeBackgroundEnd = 0x00200000
	invalidFileSize          = 0xFFFFFFFF
)

var (
	kernel32                   = windows.NewLazySystemDLL("kernel32.dll")
	procGetCompressedFileSizeW = kernel32.NewProc("GetCompressedFileSizeW")
)

// ToLongPath converts a path to the `\\?\` form so Win32 calls accept more than 260
// characters. Paths already in that form are returned unchanged.
func ToLongPath(path string) string {
	if strings.HasPrefix(path, `\\?\`) || strings.HasPrefix(path, `\\.\`) {
		return path
	}
	cleaned := filepath.Clean(path)
	if strings.HasPrefix(cleaned, `\\`) {
		return `\\?\UNC\` + cleaned[2:]
	}
	if len(cleaned) == 2 && cleaned[1] == ':' {
		cleaned += `\`
	}
	return `\\?\` + cleaned
}

type windowsDirectoryReader struct{}

// NewDirectoryReader returns the native directory reader.
func NewDirectoryReader() DirectoryReader {
	return windowsDirectoryReader{}
}

func openForMetadata(path string, access uint32) (windows.Handle, error) {
	pathPointer, err := windows.UTF16PtrFromString(ToLongPath(path))
	if err != nil {
		return windows.InvalidHandle, err
	}
	const shareAll = windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE |
		windows.FILE_SHARE_DELETE
	// BACKUP_SEMANTICS is required to open directories, and lets an elevated process with
	// SeBackupPrivilege read protected folders. OPEN_REPARSE_POINT keeps us on the link
	// itself instead of following it.
	const flags = windows.FILE_FLAG_BACKUP_SEMANTICS | windows.FILE_FLAG_OPEN_REPARSE_POINT
	handle, err := windows.CreateFile(pathPointer, access, shareAll, nil,
		windows.OPEN_EXISTING, flags, 0)
	if err != nil {
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) ||
			errors.Is(err, windows.ERROR_PRIVILEGE_NOT_HELD) {
			return windows.InvalidHandle, &AccessDeniedError{Path: path, Cause: err}
		}
		return windows.InvalidHandle, err
	}
	return handle, nil
}

func (windowsDirectoryReader) ReadDirectory(path string) ([]DirectoryEntry, error) {
	handle, err := openForMetadata(path, windows.FILE_LIST_DIRECTORY|
		windows.FILE_READ_ATTRIBUTES)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(handle)

	directoryID, err := fileIdentityOfHandle(handle)
	if err != nil {
		return nil, fmt.Errorf("identify %s: %w", path, err)
	}

	// A []uint64 backing array guarantees the 8-byte alignment the API requires.
	backing := make([]uint64, directoryBufferBytes/8)
	buffer := unsafe.Slice((*byte)(unsafe.Pointer(&backing[0])), directoryBufferBytes)

	informationClass := uint32(fileIDExtdDirectoryInfo)
	var entries []DirectoryEntry
	for firstCall := true; ; firstCall = false {
		err := windows.GetFileInformationByHandleEx(handle, informationClass, &buffer[0],
			uint32(len(buffer)))
		if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
			break
		}
		if err != nil && firstCall && informationClass == fileIDExtdDirectoryInfo &&
			errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			// FAT and exFAT do not support the extended class; fall back to 64-bit IDs.
			informationClass = fileIDBothDirectoryInfo
			firstCall = true
			err = windows.GetFileInformationByHandleEx(handle, informationClass, &buffer[0],
				uint32(len(buffer)))
			if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
				break
			}
		}
		if err != nil {
			if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
				return nil, &AccessDeniedError{Path: path, Cause: err}
			}
			return nil, fmt.Errorf("enumerate %s: %w", path, err)
		}
		entries = appendParsedEntries(entries, buffer, informationClass, directoryID.Volume)
	}

	for index := range entries {
		completeEntry(path, &entries[index])
	}
	return entries, nil
}

// rawEntry carries fields that only the Windows parser needs.
type rawEntry struct {
	attributes uint32
	reparseTag uint32
}

func appendParsedEntries(entries []DirectoryEntry, buffer []byte, informationClass uint32,
	volume uint64) []DirectoryEntry {
	offset := 0
	for {
		record := buffer[offset:]
		next := binary.LittleEndian.Uint32(record[0:])
		entry, include := parseRecord(record, informationClass, volume)
		if include {
			entries = append(entries, entry)
		}
		if next == 0 {
			return entries
		}
		offset += int(next)
	}
}

func parseRecord(record []byte, informationClass uint32, volume uint64) (DirectoryEntry,
	bool) {
	little := binary.LittleEndian
	creation := int64(little.Uint64(record[8:]))
	lastWrite := int64(little.Uint64(record[24:]))
	endOfFile := int64(little.Uint64(record[40:]))
	allocation := int64(little.Uint64(record[48:]))
	raw := rawEntry{attributes: little.Uint32(record[56:])}
	nameBytes := int(little.Uint32(record[60:]))

	var id FileID
	var nameOffset int
	if informationClass == fileIDExtdDirectoryInfo {
		raw.reparseTag = little.Uint32(record[68:])
		id = FileID{Volume: volume, Low: little.Uint64(record[72:]),
			High: little.Uint64(record[80:])}
		nameOffset = 88
	} else {
		// In FILE_ID_BOTH_DIR_INFO the EaSize field holds the reparse tag.
		raw.reparseTag = little.Uint32(record[64:])
		id = FileID{Volume: volume, Low: little.Uint64(record[96:])}
		nameOffset = 104
	}
	nameUnits := unsafe.Slice((*uint16)(unsafe.Pointer(&record[nameOffset])), nameBytes/2)
	name := windows.UTF16ToString(nameUnits)
	if name == "." || name == ".." {
		return DirectoryEntry{}, false
	}

	entry := DirectoryEntry{
		Name:              name,
		AllocatedBytes:    allocation,
		LogicalBytes:      endOfFile,
		ModifiedUnixNanos: (lastWrite - windowsToUnixEpochTicks) * 100,
		CreatedUnixNanos:  (creation - windowsToUnixEpochTicks) * 100,
		ID:                id,
		Attributes:        translateAttributes(raw.attributes),
	}
	isReparse := raw.attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0
	isLink := isReparse && (raw.reparseTag == ioReparseTagMountPoint ||
		raw.reparseTag == ioReparseTagSymlink)
	switch {
	case isLink:
		entry.Kind = EntryLink
		entry.AllocatedBytes = 0
		entry.LogicalBytes = 0
	case raw.attributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0:
		entry.Kind = EntryDirectory
		entry.AllocatedBytes = 0
		entry.LogicalBytes = 0
	default:
		entry.Kind = EntryFile
	}
	// WOF (CompactOS) and deduplicated files report misleading allocation in the directory
	// entry; mark them so completeEntry asks the file system directly.
	compression := raw.reparseTag == ioReparseTagWof || raw.reparseTag == ioReparseTagDedup
	if isReparse && compression {
		entry.Attributes |= AttributeCompressed
	}
	if isReparse && !isLink && !compression {
		entry.Attributes |= AttributeManagedReparsePoint
	}
	return entry, true
}

func translateAttributes(attributes uint32) EntryAttributes {
	var result EntryAttributes
	mapping := []struct {
		native   uint32
		portable EntryAttributes
	}{
		{windows.FILE_ATTRIBUTE_COMPRESSED, AttributeCompressed},
		{windows.FILE_ATTRIBUTE_SPARSE_FILE, AttributeSparse},
		{fileAttributeRecallOnDA, AttributeRecallOnDataAccess},
		{fileAttributeRecallOnOpn, AttributeRecallOnOpen},
		{windows.FILE_ATTRIBUTE_OFFLINE, AttributeOffline},
		{windows.FILE_ATTRIBUTE_HIDDEN, AttributeHidden},
		{windows.FILE_ATTRIBUTE_SYSTEM, AttributeSystem},
	}
	for _, pair := range mapping {
		if attributes&pair.native != 0 {
			result |= pair.portable
		}
	}
	return result
}

// completeEntry fills in values that need a per-entry call. Only compressed, sparse and
// link entries pay that cost, so ordinary files are never opened.
func completeEntry(directory string, entry *DirectoryEntry) {
	fullPath := filepath.Join(directory, entry.Name)
	switch {
	case entry.Kind == EntryLink:
		if target, err := os.Readlink(fullPath); err == nil {
			entry.LinkTarget = target
		}
	case entry.Kind == EntryFile &&
		entry.Attributes&(AttributeCompressed|AttributeSparse) != 0:
		if size, err := compressedFileSize(fullPath); err == nil {
			entry.AllocatedBytes = size
		}
	}
}

func compressedFileSize(path string) (int64, error) {
	pathPointer, err := windows.UTF16PtrFromString(ToLongPath(path))
	if err != nil {
		return 0, err
	}
	var high uint32
	low, _, callErr := procGetCompressedFileSizeW.Call(uintptr(unsafe.Pointer(pathPointer)),
		uintptr(unsafe.Pointer(&high)))
	if uint32(low) == invalidFileSize && callErr != windows.ERROR_SUCCESS {
		return 0, callErr
	}
	return int64(high)<<32 | int64(uint32(low)), nil
}

// fileIDInformation mirrors FILE_ID_INFO.
type fileIDInformation struct {
	VolumeSerialNumber uint64
	FileID             [16]byte
}

func fileIdentityOfHandle(handle windows.Handle) (FileID, error) {
	var information fileIDInformation
	err := windows.GetFileInformationByHandleEx(handle, fileIDInfo,
		(*byte)(unsafe.Pointer(&information)), uint32(unsafe.Sizeof(information)))
	if err == nil {
		return FileID{
			Volume: information.VolumeSerialNumber,
			Low:    binary.LittleEndian.Uint64(information.FileID[0:]),
			High:   binary.LittleEndian.Uint64(information.FileID[8:]),
		}, nil
	}
	var basic windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &basic); err != nil {
		return FileID{}, err
	}
	return FileID{
		Volume: uint64(basic.VolumeSerialNumber),
		Low:    uint64(basic.FileIndexHigh)<<32 | uint64(basic.FileIndexLow),
	}, nil
}

// HandleInformation is what GetFileInformationByHandle reports for one path. It is used
// for roots and, in later phases, for redaction anchors and pre-deletion re-checks.
type HandleInformation struct {
	ID                FileID
	LinkCount         uint32
	ModifiedUnixNanos int64
	CreatedUnixNanos  int64
}

// InspectPath opens path without following links and reports its identity.
func InspectPath(path string) (HandleInformation, error) {
	handle, err := openForMetadata(path, windows.FILE_READ_ATTRIBUTES)
	if err != nil {
		return HandleInformation{}, err
	}
	defer windows.CloseHandle(handle)
	identity, err := fileIdentityOfHandle(handle)
	if err != nil {
		return HandleInformation{}, err
	}
	var basic windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &basic); err != nil {
		return HandleInformation{}, err
	}
	return HandleInformation{
		ID:                identity,
		LinkCount:         basic.NumberOfLinks,
		ModifiedUnixNanos: basic.LastWriteTime.Nanoseconds(),
		CreatedUnixNanos:  basic.CreationTime.Nanoseconds(),
	}, nil
}

// DescribeRoot returns identity and file system information for a scan root.
func DescribeRoot(path string) (RootInfo, error) {
	information, err := InspectPath(path)
	if err != nil {
		return RootInfo{}, err
	}
	return RootInfo{
		ID:                information.ID,
		FileSystem:        fileSystemName(filepath.VolumeName(path) + `\`),
		ModifiedUnixNanos: information.ModifiedUnixNanos,
		CreatedUnixNanos:  information.CreatedUnixNanos,
	}, nil
}

func fileSystemName(root string) string {
	_, fileSystem := volumeLabelAndFileSystem(root)
	return fileSystem
}

func volumeLabelAndFileSystem(root string) (string, string) {
	rootPointer, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return "", ""
	}
	label := make([]uint16, windows.MAX_PATH+1)
	fileSystem := make([]uint16, windows.MAX_PATH+1)
	err = windows.GetVolumeInformation(rootPointer, &label[0], uint32(len(label)), nil, nil,
		nil, &fileSystem[0], uint32(len(fileSystem)))
	if err != nil {
		return "", ""
	}
	return windows.UTF16ToString(label), windows.UTF16ToString(fileSystem)
}

// ListVolumes lists local fixed and removable drives. Network drives are excluded on
// purpose: scanning them would generate network traffic, which Ullage never does.
func ListVolumes() ([]Volume, error) {
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return nil, err
	}
	var volumes []Volume
	for letterIndex := 0; letterIndex < 26; letterIndex++ {
		if mask&(1<<uint(letterIndex)) == 0 {
			continue
		}
		root := string(rune('A'+letterIndex)) + `:\`
		volume, ok := describeDrive(root)
		if ok {
			volumes = append(volumes, volume)
		}
	}
	return volumes, nil
}

func describeDrive(root string) (Volume, bool) {
	rootPointer, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return Volume{}, false
	}
	kind := ""
	switch windows.GetDriveType(rootPointer) {
	case windows.DRIVE_FIXED:
		kind = "Local disk"
	case windows.DRIVE_REMOVABLE:
		kind = "Removable"
	default:
		return Volume{}, false
	}
	var freeToCaller, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(rootPointer, &freeToCaller, &total,
		&totalFree); err != nil {
		return Volume{}, false
	}
	label, fileSystem := volumeLabelAndFileSystem(root)
	return Volume{
		RootPath:    root,
		DisplayName: root[:2],
		Label:       label,
		FileSystem:  fileSystem,
		Kind:        kind,
		TotalBytes:  int64(total),
		FreeBytes:   int64(totalFree),
	}, true
}

// VolumeFor returns the volume containing path.
func VolumeFor(path string) (Volume, error) {
	root := filepath.VolumeName(filepath.Clean(path)) + `\`
	volume, ok := describeDrive(root)
	if !ok {
		return Volume{}, fmt.Errorf("%s is not a local drive", root)
	}
	return volume, nil
}

// IsElevated reports whether the process token is elevated (UAC "Run as administrator").
func IsElevated() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}

// EnableBackupPrivilege lets an elevated process read folders whose ACLs exclude
// administrators, such as System Volume Information. It never grants write access.
func EnableBackupPrivilege() bool {
	var token windows.Token
	err := windows.OpenProcessToken(windows.CurrentProcess(),
		windows.TOKEN_ADJUST_PRIVILEGES|windows.TOKEN_QUERY, &token)
	if err != nil {
		return false
	}
	defer token.Close()
	var luid windows.LUID
	name, _ := windows.UTF16PtrFromString("SeBackupPrivilege")
	if err := windows.LookupPrivilegeValue(nil, name, &luid); err != nil {
		return false
	}
	privileges := windows.Tokenprivileges{PrivilegeCount: 1}
	privileges.Privileges[0] = windows.LUIDAndAttributes{Luid: luid,
		Attributes: windows.SE_PRIVILEGE_ENABLED}
	err = windows.AdjustTokenPrivileges(token, false, &privileges, 0, nil, nil)
	// AdjustTokenPrivileges reports partial success through ERROR_NOT_ALL_ASSIGNED.
	return err == nil
}

// BeginBackgroundMode lowers the process's I/O and memory priority during a scan.
func BeginBackgroundMode() (restore func()) {
	process := windows.CurrentProcess()
	if err := windows.SetPriorityClass(process, processModeBackgroundBeg); err != nil {
		return func() {}
	}
	return func() { _ = windows.SetPriorityClass(process, processModeBackgroundEnd) }
}

func knownFolder(folder *windows.KNOWNFOLDERID) string {
	path, err := windows.KnownFolderPath(folder, 0)
	if err != nil {
		return ""
	}
	return path
}

// DiscoverKnownLocations returns variable values used by recipe patterns.
func DiscoverKnownLocations() KnownLocations {
	systemRoot := knownFolder(windows.FOLDERID_Windows)
	profilesDirectory := knownFolder(windows.FOLDERID_UserProfiles)
	currentProfile := knownFolder(windows.FOLDERID_Profile)

	profiles := []string{}
	if entries, err := os.ReadDir(profilesDirectory); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				profiles = append(profiles, filepath.Join(profilesDirectory, entry.Name()))
			}
		}
	}
	if currentProfile != "" && !containsFold(profiles, currentProfile) {
		profiles = append(profiles, currentProfile)
	}

	var volumeRoots []string
	if mask, err := windows.GetLogicalDrives(); err == nil {
		for letterIndex := 0; letterIndex < 26; letterIndex++ {
			if mask&(1<<uint(letterIndex)) != 0 {
				volumeRoots = append(volumeRoots, string(rune('A'+letterIndex))+":")
			}
		}
	}

	return KnownLocations{
		Variables: map[string][]string{
			"SystemRoot":         {systemRoot},
			"SystemDrive":        {filepath.VolumeName(systemRoot)},
			"ProgramFiles":       {knownFolder(windows.FOLDERID_ProgramFilesX64)},
			"ProgramFilesX86":    {knownFolder(windows.FOLDERID_ProgramFilesX86)},
			"ProgramData":        {knownFolder(windows.FOLDERID_ProgramData)},
			"ProfilesDirectory":  {profilesDirectory},
			"UserProfile":        profiles,
			"CurrentUserProfile": {currentProfile},
			"AnyVolumeRoot":      volumeRoots,
		},
		CaseInsensitive: true,
		PathSeparator:   '\\',
	}
}

func containsFold(values []string, wanted string) bool {
	for _, value := range values {
		if strings.EqualFold(value, wanted) {
			return true
		}
	}
	return false
}

// OpenBrowser opens url in the default browser. Only ever called with the local URL.
func OpenBrowser(url string) error {
	return windows.ShellExecute(0, utf16("open"), utf16(url), nil, nil, windows.SW_SHOWNORMAL)
}

// RelaunchElevated starts this executable again through the UAC prompt.
func RelaunchElevated(arguments []string) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	quoted := make([]string, len(arguments))
	for index, argument := range arguments {
		quoted[index] = syscall.EscapeArg(argument)
	}
	return windows.ShellExecute(0, utf16("runas"), utf16(executable),
		utf16(strings.Join(quoted, " ")), nil, windows.SW_SHOWNORMAL)
}

func utf16(text string) *uint16 {
	pointer, _ := windows.UTF16PtrFromString(text)
	return pointer
}

// ConfigDirectory returns %APPDATA%\Ullage.
func ConfigDirectory() (string, error) {
	base := knownFolder(windows.FOLDERID_RoamingAppData)
	if base == "" {
		return "", errors.New("roaming AppData folder not found")
	}
	return filepath.Join(base, "Ullage"), nil
}

// LogDirectory returns %LOCALAPPDATA%\Ullage\logs.
func LogDirectory() (string, error) {
	base := knownFolder(windows.FOLDERID_LocalAppData)
	if base == "" {
		return "", errors.New("local AppData folder not found")
	}
	return filepath.Join(base, "Ullage", "logs"), nil
}

// UserName returns the account name without the domain prefix.
func UserName() string {
	current, err := user.Current()
	if err != nil {
		return os.Getenv("USERNAME")
	}
	name := current.Username
	if index := strings.LastIndex(name, `\`); index >= 0 {
		name = name[index+1:]
	}
	return name
}

// MachineName returns the computer name.
func MachineName() string {
	name, _ := os.Hostname()
	return name
}
