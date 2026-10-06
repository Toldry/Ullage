package scanner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/toldry/ullage/internal/platform"
)

// Shared helpers for the real-file-system fixture tests in fixtures_<os>_test.go.

func scanDirectory(t *testing.T, root string) *Result {
	t.Helper()
	return runScan(t, Options{Roots: []string{root}, Reader: platform.NewDirectoryReader(),
		Workers: 4})
}

func writeFile(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	content := make([]byte, size)
	for index := range content {
		// Pseudo-random bytes so file-system compression cannot shrink regular fixtures.
		content[index] = byte(index*7919 + index/251)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFixtureHardLinksCountedOnce(t *testing.T) {
	root := t.TempDir()
	original := filepath.Join(root, "a", "original.bin")
	writeFile(t, original, 3<<20)
	if err := os.MkdirAll(filepath.Join(root, "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(original, filepath.Join(root, "b", "link.bin")); err != nil {
		t.Skipf("hard links unsupported here: %v", err)
	}
	result := scanDirectory(t, root)
	single := findNode(t, result, original).TotalBytes()
	if single < 3<<20 {
		t.Fatalf("original counted as %d bytes", single)
	}
	if total := totalAcrossRoots(result); total != single {
		t.Fatalf("hard link double counted: total %d, file %d", total, single)
	}
}

func TestFixtureSymlinkIsNotFollowed(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "payload.bin"), 2<<20)
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable (needs privilege or developer mode): %v", err)
	}
	result := scanDirectory(t, root)
	node := findNode(t, result, link)
	if node.Kind != platform.EntryLink || node.TotalBytes() != 0 {
		t.Fatalf("symlink node = %+v", node)
	}
	if !strings.Contains(node.LinkTarget, filepath.Base(outside)) {
		t.Fatalf("link target %q does not point at %q", node.LinkTarget, outside)
	}
	if totalAcrossRoots(result) != 0 {
		t.Fatalf("symlink was followed")
	}
}

func TestFixtureLongPaths(t *testing.T) {
	root := t.TempDir()
	deep := root
	for len(deep) < 400 {
		deep = filepath.Join(deep, strings.Repeat("long-directory-name-", 3))
	}
	writeFile(t, filepath.Join(deep, "deep.bin"), 2<<20)
	result := scanDirectory(t, root)
	if result.ErrorCount != 0 || totalAcrossRoots(result) < 2<<20 {
		t.Fatalf("long path not scanned: errors %d total %d", result.ErrorCount,
			totalAcrossRoots(result))
	}
	findNode(t, result, filepath.Join(deep, "deep.bin"))
}
