//go:build windows

package platform

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Shell file-operation constants from shellapi.h.
const (
	shellOperationDelete      = 0x0003
	shellFlagSilent           = 0x0004
	shellFlagNoConfirmation   = 0x0010
	shellFlagAllowUndo        = 0x0040
	shellFlagNoErrorUI        = 0x0400
	shellFlagWantNukeWarning  = 0x4000
	shellMaximumPath          = 260
	errorSharingViolation     = 32
	errorLockViolation        = 33
	shellErrorAccessDeniedSrc = 0x78
	shellErrorPathTooDeep     = 0xB7
)

var (
	shell32              = windows.NewLazySystemDLL("shell32.dll")
	procSHFileOperationW = shell32.NewProc("SHFileOperationW")
)

// shellFileOperation mirrors SHFILEOPSTRUCTW on 64-bit Windows, where the header uses
// natural alignment.
type shellFileOperation struct {
	window               uintptr
	function             uint32
	from                 *uint16
	to                   *uint16
	flags                uint16
	anyOperationsAborted int32
	nameMappings         uintptr
	progressTitle        *uint16
}

// MoveToRecycleBin sends the paths to the Recycle Bin through the shell, which is what
// Explorer does. If Windows cannot recycle an item (for example it is too large for the
// bin), the shell asks the user before deleting it permanently instead of doing so
// silently (FOF_WANTNUKEWARNING); declining is reported as ErrRecycleDeclined.
func MoveToRecycleBin(paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	var list []uint16
	for _, path := range paths {
		plain := strings.TrimPrefix(path, `\\?\`)
		if len(plain) >= shellMaximumPath {
			return fmt.Errorf("%w: %s", ErrPathTooLongForRecycleBin, plain)
		}
		encoded, err := windows.UTF16FromString(plain)
		if err != nil {
			return err
		}
		list = append(list, encoded...)
	}
	list = append(list, 0)
	operation := shellFileOperation{function: shellOperationDelete, from: &list[0],
		flags: shellFlagAllowUndo | shellFlagNoConfirmation | shellFlagSilent |
			shellFlagNoErrorUI | shellFlagWantNukeWarning}
	result, _, _ := procSHFileOperationW.Call(uintptr(unsafe.Pointer(&operation)))
	if operation.anyOperationsAborted != 0 {
		return ErrRecycleDeclined
	}
	return shellError(result)
}

func shellError(code uintptr) error {
	switch code {
	case 0:
		return nil
	case errorSharingViolation, errorLockViolation:
		return fmt.Errorf("%w (code %d)", ErrInUse, code)
	case uintptr(windows.ERROR_ACCESS_DENIED), shellErrorAccessDeniedSrc:
		return fmt.Errorf("%w (code %d)", ErrAccessDenied, code)
	case shellErrorPathTooDeep:
		return ErrPathTooLongForRecycleBin
	default:
		return fmt.Errorf("the shell reported error 0x%X", code)
	}
}

// IsInUse reports whether err means another program holds the file open.
func IsInUse(err error) bool {
	var errno syscall.Errno
	if errors.As(err, &errno) && (errno == errorSharingViolation ||
		errno == errorLockViolation) {
		return true
	}
	return errors.Is(err, ErrInUse)
}

// CanonicalPath returns the form of a path used to compare it with protected locations:
// no `\\?\` prefix, 8.3 short names expanded, trailing dots and spaces removed from each
// component (Win32 ignores them, so "Windows." names the same folder as "Windows").
// Callers compare the result case-insensitively.
func CanonicalPath(path string) string {
	path = strings.TrimPrefix(path, `\\?\`)
	if strings.HasPrefix(path, `UNC\`) {
		path = `\\` + path[len(`UNC\`):]
	}
	if long, err := longPathName(path); err == nil {
		path = long
	}
	path = filepath.Clean(path)
	volume := filepath.VolumeName(path)
	parts := strings.Split(path[len(volume):], `\`)
	for index, part := range parts {
		if trimmed := strings.TrimRight(part, ". "); trimmed != "" || part == "" {
			parts[index] = trimmed
		}
	}
	return volume + strings.Join(parts, `\`)
}

func longPathName(path string) (string, error) {
	encoded, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	buffer := make([]uint16, 1024)
	for {
		length, err := windows.GetLongPathName(encoded, &buffer[0], uint32(len(buffer)))
		if err != nil {
			return "", err
		}
		if int(length) <= len(buffer) {
			return windows.UTF16ToString(buffer[:length]), nil
		}
		buffer = make([]uint16, length)
	}
}

// ProtectedTrees are the folders Ullage never deletes from (safety rule 4): the Windows
// folder and both Program Files folders, as the Known Folder API reports them.
func ProtectedTrees(locations KnownLocations) []string {
	var trees []string
	for _, variable := range []string{"SystemRoot", "ProgramFiles", "ProgramFilesX86"} {
		for _, path := range locations.Variables[variable] {
			if path != "" {
				trees = append(trees, path)
			}
		}
	}
	return trees
}
