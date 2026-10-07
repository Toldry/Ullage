//go:build linux

package platform

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

// MoveToRecycleBin moves the paths to the freedesktop.org Trash of the current user. Only
// the home trash is supported: a path on another file system cannot be renamed into it,
// which is reported as ErrNoRecycleBin rather than copied and deleted.
func MoveToRecycleBin(paths []string) error {
	trash, err := trashDirectory()
	if err != nil {
		return err
	}
	var failures []error
	for _, path := range paths {
		if err := moveToTrash(trash, path); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func trashDirectory() (string, error) {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "share")
	}
	trash := filepath.Join(base, "Trash")
	for _, directory := range []string{"files", "info"} {
		if err := os.MkdirAll(filepath.Join(trash, directory), 0o700); err != nil {
			return "", err
		}
	}
	return trash, nil
}

func moveToTrash(trash, path string) error {
	name := filepath.Base(path)
	for attempt := 1; ; attempt++ {
		info := filepath.Join(trash, "info", name+".trashinfo")
		file, err := os.OpenFile(info, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			name = filepath.Base(path) + "." + strconv.Itoa(attempt)
			continue
		}
		if err != nil {
			return err
		}
		_, writeErr := fmt.Fprintf(file, "[Trash Info]\nPath=%s\nDeletionDate=%s\n",
			(&url.URL{Path: path}).EscapedPath(), time.Now().Format("2006-01-02T15:04:05"))
		closeErr := file.Close()
		if err := errors.Join(writeErr, closeErr); err != nil {
			_ = os.Remove(info)
			return err
		}
		if err := os.Rename(path, filepath.Join(trash, "files", name)); err != nil {
			_ = os.Remove(info)
			if errors.Is(err, syscall.EXDEV) {
				return fmt.Errorf("%w: %s", ErrNoRecycleBin, path)
			}
			return err
		}
		return nil
	}
}

// IsInUse reports whether err means another program holds the file open.
func IsInUse(err error) bool {
	return errors.Is(err, syscall.EBUSY) || errors.Is(err, syscall.ETXTBSY) ||
		errors.Is(err, ErrInUse)
}

// CanonicalPath returns the cleaned path. Linux has no short names or ignored trailing
// characters, and links are checked separately by the deletion guard.
func CanonicalPath(path string) string {
	return filepath.Clean(path)
}

// ProtectedTrees are the system folders Ullage never deletes from.
func ProtectedTrees(KnownLocations) []string {
	return []string{"/bin", "/boot", "/etc", "/lib", "/lib32", "/lib64", "/opt", "/sbin",
		"/usr", "/var/lib"}
}
