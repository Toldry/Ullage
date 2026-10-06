package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/toldry/ullage/internal/platform"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	environment := NativeEnvironment(t.TempDir())
	environment.InstalledApplications = func() ([]platform.InstalledApplication, error) {
		return nil, nil
	}
	manager, err := NewManager(environment)
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

func waitForState(t *testing.T, manager *Manager) Progress {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if progress := manager.Progress(); progress.State != StateRunning {
			return progress
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("scan did not finish")
	return Progress{}
}

func TestScanFolderEndToEnd(t *testing.T) {
	manager := newTestManager(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "project", "build"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "project", "build", "out.bin"),
		make([]byte, 2<<20), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "vault"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := manager.StartScan(ScanRequest{Roots: []string{root}, Exclusions: []string{"vault"}})
	if err != nil {
		t.Fatal(err)
	}
	progress := waitForState(t, manager)
	if progress.State != StateFinished || len(progress.SessionCode) != 6 {
		t.Fatalf("progress = %+v", progress)
	}
	completed := manager.Completed()
	if completed == nil || completed.Overview.Totals.Unknown < 2<<20 {
		t.Fatalf("overview = %+v", completed)
	}
	if manager.Config().Exclusions[0] != "vault" {
		t.Fatalf("exclusions not remembered")
	}
	foundExclusion := false
	for _, line := range manager.LogSince(0) {
		if line.Kind == "skip" && filepath.Base(line.Path) == "vault" {
			foundExclusion = true
		}
	}
	if !foundExclusion {
		t.Fatalf("exclusion not logged")
	}
}

func TestNetworkAndRelativeRootsAreRefused(t *testing.T) {
	manager := newTestManager(t)
	for _, root := range []string{`\\server\share`, "//server/share", "relative/path", ""} {
		if err := manager.StartScan(ScanRequest{Roots: []string{root}}); err == nil {
			t.Errorf("root %q was accepted", root)
		}
	}
	if err := manager.StartScan(ScanRequest{}); err == nil {
		t.Errorf("empty request accepted")
	}
}

func TestSessionCodesUseTheUnambiguousAlphabet(t *testing.T) {
	for attempt := 0; attempt < 200; attempt++ {
		code, err := newSessionCode()
		if err != nil {
			t.Fatal(err)
		}
		for _, character := range code {
			switch character {
			case '0', 'O', '1', 'I', 'L':
				t.Fatalf("ambiguous character in %s", code)
			}
		}
	}
}
