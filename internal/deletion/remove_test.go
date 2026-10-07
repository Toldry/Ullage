package deletion

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/toldry/ullage/internal/platform"
)

// makeDirectoryLink creates a junction on Windows (what mklink /J makes, and what a
// non-elevated user can create) and a symbolic link elsewhere.
func makeDirectoryLink(t *testing.T, link, target string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		output, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput()
		if err != nil {
			t.Fatalf("mklink: %v %s", err, output)
		}
		return
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A permanent delete removes a link inside the target as a link; what it points to stays.
func TestPermanentRemoveDoesNotFollowLinks(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(root, "outside")
	writeFile(t, filepath.Join(outside, "keep.txt"))
	target := filepath.Join(root, "target")
	writeFile(t, filepath.Join(target, "nested", "a.txt"))
	makeDirectoryLink(t, filepath.Join(target, "nested", "escape"), outside)

	remover := PermanentRemover{Reader: platform.NewDirectoryReader()}
	if err := remover.RemovePermanently(target); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("target still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "keep.txt")); err != nil {
		t.Fatalf("the link's target was touched: %v", err)
	}
}

func TestPermanentRemoveOfALinkKeepsItsTarget(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(root, "outside")
	writeFile(t, filepath.Join(outside, "keep.txt"))
	link := filepath.Join(root, "link")
	makeDirectoryLink(t, link, outside)
	remover := PermanentRemover{Reader: platform.NewDirectoryReader()}
	if err := remover.RemovePermanently(link); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(outside, "keep.txt")); err != nil {
		t.Fatalf("the link's target was touched: %v", err)
	}
}

func TestMoveToRecycleBin(t *testing.T) {
	root := t.TempDir()
	if runtime.GOOS != "windows" {
		// Keep the trash on the same file system as the test files, and out of $HOME.
		t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	}
	file := filepath.Join(root, "recycle-me.txt")
	folder := filepath.Join(root, "recycle-folder")
	writeFile(t, file)
	writeFile(t, filepath.Join(folder, "inside.txt"))
	if err := platform.MoveToRecycleBin([]string{file, folder}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{file, folder} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s still exists: %v", path, err)
		}
	}
	if runtime.GOOS != "windows" {
		trashed := filepath.Join(root, "data", "Trash", "files", "recycle-me.txt")
		if _, err := os.Stat(trashed); err != nil {
			t.Fatalf("not in the trash: %v", err)
		}
	}
}
