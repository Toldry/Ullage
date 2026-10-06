//go:build linux

package scanner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFixtureSparseFileUsesAllocatedSize(t *testing.T) {
	root := t.TempDir()
	sparsePath := filepath.Join(root, "sparse.img")
	sparseFile, err := os.Create(sparsePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := sparseFile.Truncate(1 << 30); err != nil {
		t.Fatal(err)
	}
	sparseFile.Close()

	result := scanDirectory(t, root)
	if total := totalAcrossRoots(result); total > 1<<20 {
		t.Fatalf("sparse 1 GiB file counted as %d bytes", total)
	}
}

func TestFixtureAccessDeniedFolder(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permissions; covered by the Windows ACL fixture in CI")
	}
	root := t.TempDir()
	locked := filepath.Join(root, "locked")
	writeFile(t, filepath.Join(locked, "inside.bin"), 1<<20)
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })

	result := scanDirectory(t, root)
	if result.DeniedCount != 1 || findNode(t, result, locked).Flags&FlagAccessDenied == 0 {
		t.Fatalf("denied = %d", result.DeniedCount)
	}
}
