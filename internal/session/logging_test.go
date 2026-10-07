package session

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/toldry/ullage/internal/applog"
	"github.com/toldry/ullage/internal/platform"
	"github.com/toldry/ullage/internal/redact"
	"github.com/toldry/ullage/internal/storage"
)

// loggedSession scans a folder whose names include a redacted client, an always-redacted
// word and a link pointing outside the scan, with logging on, and returns the log text.
func loggedSession(t *testing.T, configDirectory string, rule bool) (*Manager, string,
	func() string) {
	t.Helper()
	logDirectory := t.TempDir()
	environment := NativeEnvironment(configDirectory)
	environment.InstalledApplications = func() ([]platform.InstalledApplication, error) {
		return nil, nil
	}
	environment.SensitiveWords = []string{"zorblax"}
	environment.Logger = applog.Open(logDirectory)
	t.Cleanup(environment.Logger.Close)
	manager, err := NewManager(environment)
	if err != nil {
		t.Fatal(err)
	}
	if rule {
		if _, err := manager.SaveRule(RuleRequest{Type: redact.RuleName, Pattern: "northwind",
			Treatment: redact.TreatmentSubtree}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := manager.UpdateConfig(func(config *storage.Config) {
		config.PromptMinimumMegabytes = 1
	}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	for _, file := range []string{"clients/northwind/contract.bin",
		"clients/northwind/build/out.bin", "zorblax-notes/x.bin", "photos/2019/trip.bin"} {
		writeSized(t, filepath.Join(root, filepath.FromSlash(file)), 2<<20)
	}
	outside := filepath.Join(t.TempDir(), "northwind-archive")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		// A link event carries its target, which lies outside the scan.
		if err := os.Symlink(outside, filepath.Join(root, "clients", "northwind",
			"escape")); err != nil {
			t.Fatal(err)
		}
	}
	if err := manager.StartScan(ScanRequest{Roots: []string{root}}); err != nil {
		t.Fatal(err)
	}
	waitForState(t, manager)
	read := func() string {
		entries, _ := os.ReadDir(logDirectory)
		var text strings.Builder
		for _, entry := range entries {
			content, _ := os.ReadFile(filepath.Join(logDirectory, entry.Name()))
			text.Write(content)
		}
		return text.String()
	}
	return manager, root, read
}

func writeSized(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Safety rule 8: the log never holds the name of a redacted item, an account name or a
// path outside what can be rendered, even through error texts and link targets.
func TestLogsHoldNoRedactedNames(t *testing.T) {
	manager, root, read := loggedSession(t, t.TempDir(), true)
	view, err := manager.Export(false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Paste(PasteRequest{Batch: 1, Text: replyFor(view.Code,
		view.Batches[0].Text)}); err != nil {
		t.Fatal(err)
	}
	manager.lock.Lock()
	scrubbed := manager.safeText("open " + filepath.Join(root, "clients", "northwind",
		"contract.bin") + ": access denied for zorblax (S-1-5-21-1-2-3-1001)")
	outsideText := manager.safeText("cannot read " + filepath.Join(os.TempDir(),
		"northwind-elsewhere", "file"))
	manager.lock.Unlock()
	manager.Log("test", scrubbed)
	manager.Log("test", outsideText)

	log := read()
	for _, want := range []string{"scan      started", "scan      finished", "export",
		"paste", "<R", "access denied for <account> (<sid>)", pathWithheld} {
		if !strings.Contains(log, want) {
			t.Errorf("log lacks %q:\n%s", want, log)
		}
	}
	for _, secret := range []string{"northwind", "zorblax", "S-1-5-21"} {
		if strings.Contains(strings.ToLower(log), strings.ToLower(secret)) {
			t.Fatalf("log contains %q:\n%s", secret, log)
		}
	}
	if runtime.GOOS != "windows" && !strings.Contains(log, "link not followed") {
		t.Errorf("link event missing:\n%s", log)
	}
}

// With unreadable rules nothing can be rendered safely, so every path is withheld.
func TestLogsWithholdPathsWhileRulesAreUnreadable(t *testing.T) {
	configDirectory := t.TempDir()
	if err := os.WriteFile(redact.StorePath(configDirectory),
		[]byte("ULLAGE-RULES\x00 garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager, root, read := loggedSession(t, configDirectory, false)
	if _, err := manager.Export(false); !errors.Is(err, ErrRulesUnavailable) {
		t.Fatalf("export: %v", err)
	}
	log := read()
	if strings.Contains(log, filepath.Base(root)) || strings.Contains(log, "photos") ||
		!strings.Contains(log, pathWithheld) {
		t.Fatalf("paths logged while rules are unreadable:\n%s", log)
	}
}
