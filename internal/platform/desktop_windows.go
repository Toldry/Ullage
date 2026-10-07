//go:build windows

package platform

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	unicode16 "unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// DataDirectory returns %LOCALAPPDATA%\Ullage, for data that should not roam.
func DataDirectory() (string, error) {
	base := knownFolder(windows.FOLDERID_LocalAppData)
	if base == "" {
		return "", errors.New("local AppData folder not found")
	}
	return filepath.Join(base, "Ullage"), nil
}

// ShowError reports a fatal problem. The release build has no console, so a message box
// is the only place the user would see it.
func ShowError(message string) {
	const iconError = 0x10
	_, _ = windows.MessageBox(0, utf16(message), utf16("Ullage"), iconError)
}

// ShortcutPath is where the Start menu shortcut lives for the current user.
func ShortcutPath() (string, error) {
	programs := knownFolder(windows.FOLDERID_Programs)
	if programs == "" {
		return "", errors.New("Start menu folder not found")
	}
	return filepath.Join(programs, "Ullage.lnk"), nil
}

// InstallShortcut creates the Start menu shortcut to the given executable.
func InstallShortcut(executable string) error {
	path, err := ShortcutPath()
	if err != nil {
		return err
	}
	idList, err := ItemIDList(executable)
	if err != nil {
		return err
	}
	content := ShellLink(executable, "Local disk space analyzer", idList)
	return os.WriteFile(path, content, 0o644)
}

// RemoveShortcut deletes the Start menu shortcut, if there is one.
func RemoveShortcut() error {
	path, err := ShortcutPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Shell link constants from [MS-SHLLINK].
const (
	linkHasTargetIDList    = 0x00000001
	linkHasLinkInfo        = 0x00000002
	linkHasName            = 0x00000004
	linkHasWorkingDir      = 0x00000010
	linkIsUnicode          = 0x00000080
	linkInfoVolumeAndLocal = 0x00000001
	linkInfoHeaderSize     = 0x24
	driveFixed             = 3
	showNormal             = 1
	fileAttributeArchive   = 0x20
)

// ShellLink writes a minimal .lnk file ([MS-SHLLINK]) pointing at target: a header, the
// shell's item ID list for the target (which is what the shell resolves), a LinkInfo
// with the local path in ANSI and Unicode as a fallback, a description and the working
// folder. Writing the format directly avoids COM objects and keeps the build free of cgo.
func ShellLink(target, description string, idList []byte) []byte {
	var out bytes.Buffer
	write := func(values ...any) {
		for _, value := range values {
			_ = binary.Write(&out, binary.LittleEndian, value)
		}
	}
	// Header: size, CLSID {00021401-0000-0000-C000-000000000046}, flags, attributes,
	// three file times, size, icon index, show command, hot key and reserved fields.
	write(uint32(0x4C))
	out.Write([]byte{0x01, 0x14, 0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0xC0, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x46})
	flags := uint32(linkHasLinkInfo | linkHasName | linkHasWorkingDir | linkIsUnicode)
	if len(idList) > 0 {
		flags |= linkHasTargetIDList
	}
	write(flags,
		uint32(fileAttributeArchive), uint64(0), uint64(0), uint64(0), uint32(0),
		int32(0), uint32(showNormal), uint16(0), uint16(0), uint32(0), uint32(0))

	if len(idList) > 0 {
		write(uint16(len(idList)))
		out.Write(idList)
	}
	out.Write(linkInfo(target))
	writeCountedString(&out, description)
	writeCountedString(&out, filepath.Dir(target))
	write(uint32(0)) // terminal block
	return out.Bytes()
}

func linkInfo(target string) []byte {
	volumeID := []byte{}
	volumeID = binary.LittleEndian.AppendUint32(volumeID, 17) // size, including the label
	volumeID = binary.LittleEndian.AppendUint32(volumeID, driveFixed)
	volumeID = binary.LittleEndian.AppendUint32(volumeID, 0)    // serial number
	volumeID = binary.LittleEndian.AppendUint32(volumeID, 0x10) // label offset
	volumeID = append(volumeID, 0)                              // empty label

	ansiPath := append([]byte(asciiApproximation(target)), 0)
	ansiSuffix := []byte{0}
	unicodePath := utf16Bytes(target)
	unicodeSuffix := []byte{0, 0}

	volumeOffset := uint32(linkInfoHeaderSize)
	basePathOffset := volumeOffset + uint32(len(volumeID))
	suffixOffset := basePathOffset + uint32(len(ansiPath))
	unicodePathOffset := suffixOffset + uint32(len(ansiSuffix))
	unicodeSuffixOffset := unicodePathOffset + uint32(len(unicodePath))
	size := unicodeSuffixOffset + uint32(len(unicodeSuffix))

	info := []byte{}
	for _, value := range []uint32{size, linkInfoHeaderSize, linkInfoVolumeAndLocal,
		volumeOffset, basePathOffset, 0, suffixOffset, unicodePathOffset,
		unicodeSuffixOffset} {
		info = binary.LittleEndian.AppendUint32(info, value)
	}
	info = append(info, volumeID...)
	info = append(info, ansiPath...)
	info = append(info, ansiSuffix...)
	info = append(info, unicodePath...)
	return append(info, unicodeSuffix...)
}

// asciiApproximation fills the legacy ANSI path; Windows reads the Unicode one, which is
// exact, whenever it is present.
func asciiApproximation(text string) string {
	return strings.Map(func(character rune) rune {
		if character > 0x7E {
			return '_'
		}
		return character
	}, text)
}

func utf16Bytes(text string) []byte {
	encoded := []byte{}
	for _, unit := range unicode16.Encode([]rune(text)) {
		encoded = binary.LittleEndian.AppendUint16(encoded, unit)
	}
	return append(encoded, 0, 0)
}

func writeCountedString(out *bytes.Buffer, text string) {
	units := unicode16.Encode([]rune(text))
	_ = binary.Write(out, binary.LittleEndian, uint16(len(units)))
	for _, unit := range units {
		_ = binary.Write(out, binary.LittleEndian, unit)
	}
}

var (
	procSHParseDisplayName = shell32.NewProc("SHParseDisplayName")
	procILGetSize          = shell32.NewProc("ILGetSize")
)

// ItemIDList asks the shell for the item ID list of an existing file, including the
// two-byte terminator, as stored in a shortcut.
func ItemIDList(path string) ([]byte, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED); err == nil {
		defer windows.CoUninitialize()
	}
	var list *byte
	result, _, _ := procSHParseDisplayName.Call(uintptr(unsafe.Pointer(utf16(path))), 0,
		uintptr(unsafe.Pointer(&list)), 0, 0)
	if result != 0 || list == nil {
		return nil, fmt.Errorf("the shell could not resolve %s (0x%X)", path, result)
	}
	defer windows.CoTaskMemFree(unsafe.Pointer(list))
	size, _, _ := procILGetSize.Call(uintptr(unsafe.Pointer(list)))
	return append([]byte{}, unsafe.Slice(list, size)...), nil
}
