//go:build windows

package scanner

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/toldry/ullage/internal/platform"
)

const (
	controlSetSparse      = 0x000900C4
	controlSetCompression = 0x0009C040
	compressionDefault    = 1
)

func deviceControl(t *testing.T, path string, code uint32, input []byte) {
	t.Helper()
	pathPointer, _ := windows.UTF16PtrFromString(path)
	handle, err := windows.CreateFile(pathPointer, windows.GENERIC_READ|windows.GENERIC_WRITE,
		0, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	var inputPointer *byte
	if len(input) > 0 {
		inputPointer = &input[0]
	}
	var returned uint32
	if err := windows.DeviceIoControl(handle, code, inputPointer, uint32(len(input)), nil, 0,
		&returned, nil); err != nil {
		t.Skipf("device control %#x unsupported: %v", code, err)
	}
}

func TestFixtureSparseFileUsesAllocatedSize(t *testing.T) {
	root := t.TempDir()
	sparsePath := filepath.Join(root, "sparse.img")
	if err := os.WriteFile(sparsePath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	deviceControl(t, sparsePath, controlSetSparse, nil)
	if err := os.Truncate(sparsePath, 1<<30); err != nil {
		t.Fatal(err)
	}
	result := scanRetainingEveryFile(t, root)
	node := findNode(t, result, sparsePath)
	if node.Attributes&platform.AttributeSparse == 0 {
		t.Fatalf("sparse attribute not detected")
	}
	if node.TotalBytes() > 1<<20 {
		t.Fatalf("sparse 1 GiB file counted as %d bytes", node.TotalBytes())
	}
}

func TestFixtureCompressedFileUsesCompressedSize(t *testing.T) {
	root := t.TempDir()
	compressedPath := filepath.Join(root, "compressible.txt")
	if err := os.WriteFile(compressedPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	// Enable compression first: NTFS compresses data as it is written.
	format := uint16(compressionDefault)
	deviceControl(t, compressedPath, controlSetCompression,
		unsafe.Slice((*byte)(unsafe.Pointer(&format)), 2))
	// NTFS reserves full clusters for compressed data until the cache is flushed.
	compressedFile, err := os.OpenFile(compressedPath, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compressedFile.Write(make([]byte, 8<<20)); err != nil {
		t.Fatal(err)
	}
	if err := compressedFile.Sync(); err != nil {
		t.Fatal(err)
	}
	compressedFile.Close()
	result := scanRetainingEveryFile(t, root)
	node := findNode(t, result, compressedPath)
	if node.Attributes&platform.AttributeCompressed == 0 {
		t.Fatalf("compressed attribute not detected")
	}
	if node.TotalBytes() >= 8<<20 {
		t.Fatalf("compressed file counted at apparent size %d", node.TotalBytes())
	}
}

func TestFixtureJunctionIsNotFollowed(t *testing.T) {
	root := t.TempDir()
	target := t.TempDir()
	writeFile(t, filepath.Join(target, "payload.bin"), 2<<20)
	junction := filepath.Join(root, "junction")
	if output, err := exec.Command("cmd", "/c", "mklink", "/J", junction,
		target).CombinedOutput(); err != nil {
		t.Fatalf("mklink /J failed: %v %s", err, output)
	}
	result := scanDirectory(t, root)
	node := findNode(t, result, junction)
	if node.Kind != platform.EntryLink || node.LinkTarget == "" {
		t.Fatalf("junction node = %+v", node)
	}
	if totalAcrossRoots(result) != 0 {
		t.Fatalf("junction was followed")
	}
}

func TestFixtureOfflineAttributeCountsAsNotLocal(t *testing.T) {
	root := t.TempDir()
	offlinePath := filepath.Join(root, "offline.bin")
	writeFile(t, offlinePath, 1<<20)
	pathPointer, _ := windows.UTF16PtrFromString(offlinePath)
	if err := windows.SetFileAttributes(pathPointer, windows.FILE_ATTRIBUTE_OFFLINE); err != nil {
		t.Skipf("cannot set offline attribute: %v", err)
	}
	result := scanDirectory(t, root)
	if result.Placeholders != 1 || result.NotLocalBytes != 1<<20 {
		t.Fatalf("placeholders %d notLocal %d", result.Placeholders, result.NotLocalBytes)
	}
}

func TestFixtureAccessDeniedFolder(t *testing.T) {
	root := t.TempDir()
	locked := filepath.Join(root, "locked")
	writeFile(t, filepath.Join(locked, "inside.bin"), 1<<20)
	// Deny listing to Everyone. Without SeBackupPrivilege enabled the deny entry applies
	// even to administrators. The test disables it explicitly: a parent such as Git Bash
	// (the Cygwin runtime) enables it for elevated users, and children inherit that.
	withoutBackupPrivilege(t)
	const everyone = "*S-1-1-0"
	if output, err := exec.Command("icacls", locked, "/deny",
		everyone+":(RD)").CombinedOutput(); err != nil {
		t.Fatalf("icacls failed: %v %s", err, output)
	}
	t.Cleanup(func() { exec.Command("icacls", locked, "/remove:d", everyone).Run() })

	result := scanDirectory(t, root)
	if result.DeniedCount != 1 || findNode(t, result, locked).Flags&FlagAccessDenied == 0 {
		t.Fatalf("denied = %d paths %v", result.DeniedCount, result.DeniedPaths)
	}
}

// withoutBackupPrivilege disables SeBackupPrivilege for the test and restores the
// previous state afterwards.
func withoutBackupPrivilege(t *testing.T) {
	t.Helper()
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(),
		windows.TOKEN_ADJUST_PRIVILEGES|windows.TOKEN_QUERY, &token); err != nil {
		t.Fatal(err)
	}
	var luid windows.LUID
	name, _ := windows.UTF16PtrFromString("SeBackupPrivilege")
	if err := windows.LookupPrivilegeValue(nil, name, &luid); err != nil {
		token.Close()
		t.Fatal(err)
	}
	disabled := windows.Tokenprivileges{PrivilegeCount: 1}
	disabled.Privileges[0] = windows.LUIDAndAttributes{Luid: luid}
	var previous windows.Tokenprivileges
	var length uint32
	if err := windows.AdjustTokenPrivileges(token, false, &disabled,
		uint32(unsafe.Sizeof(previous)), &previous, &length); err != nil {
		token.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if previous.PrivilegeCount > 0 {
			_ = windows.AdjustTokenPrivileges(token, false, &previous, 0, nil, nil)
		}
		token.Close()
	})
}
