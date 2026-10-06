package session

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/toldry/ullage/internal/platform"
	"github.com/toldry/ullage/internal/redact"
	"github.com/toldry/ullage/internal/scanner"
)

func scannedManager(t *testing.T, configDirectory string) (*Manager, string) {
	t.Helper()
	environment := NativeEnvironment(configDirectory)
	environment.InstalledApplications = func() ([]platform.InstalledApplication, error) {
		return nil, nil
	}
	environment.SensitiveWords = []string{"zorblax"}
	manager, err := NewManager(environment)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	for _, file := range []string{"clients/northwind/contract.bin",
		"clients/northwind/build/out.bin", "zorblax-notes/x.bin", "photos/2019/trip.bin"} {
		path := filepath.Join(root, filepath.FromSlash(file))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, make([]byte, 2<<20), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := manager.StartScan(ScanRequest{Roots: []string{root}}); err != nil {
		t.Fatal(err)
	}
	waitForState(t, manager)
	return manager, root
}

func findItem(t *testing.T, manager *Manager, code string, names ...string) RedactionItem {
	t.Helper()
	parent := scanner.NoNode
	var item RedactionItem
	for _, name := range names {
		children, err := manager.Children(code, parent)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, child := range children.Items {
			if child.Name == name || (parent == scanner.NoNode && len(names) > 0) {
				item, found = child, true
				break
			}
		}
		if !found {
			t.Fatalf("item %s not found", name)
		}
		parent = item.ID
	}
	return item
}

func TestRedactionReviewAndRules(t *testing.T) {
	manager, _ := scannedManager(t, t.TempDir())
	summary := manager.RedactionSummary()
	if !summary.Available || summary.AlwaysRedacted != 1 {
		t.Fatalf("summary = %+v", summary)
	}
	code := summary.SessionCode
	northwind := findItem(t, manager, code, "root", "clients", "northwind")

	err := manager.SetOverride(OverrideRequest{SessionCode: code, ID: northwind.ID,
		Treatment: "subtree", KeepWellKnown: true})
	if err != nil {
		t.Fatal(err)
	}
	build := findItem(t, manager, code, "root", "clients", "northwind", "build")
	if strings.Contains(build.Rendered, "northwind") || !strings.HasSuffix(build.Rendered,
		"/build") {
		t.Fatalf("rendered = %s", build.Rendered)
	}

	fromNode := northwind.ID
	rule, err := manager.SaveRule(RuleRequest{Type: redact.RuleExact,
		Treatment: redact.TreatmentSubtree, SessionCode: code, FromNode: &fromNode})
	if err != nil {
		t.Fatal(err)
	}
	if rule.Anchor == nil || !strings.HasSuffix(rule.Pattern, "/clients/northwind") {
		t.Fatalf("rule = %+v", rule)
	}
	views, err := manager.Rules()
	if err != nil || len(views) != 1 || views[0].Matches != 1 || views[0].MatchBytes < 4<<20 {
		t.Fatalf("views = %+v %v", views, err)
	}
	if item := findItem(t, manager, code, "root", "clients", "northwind"); item.Source !=
		"rule" {
		t.Fatalf("remembered rule did not replace the session choice: %+v", item)
	}
	if err := manager.SetOverride(OverrideRequest{SessionCode: "OLD123", ID: northwind.ID,
		Treatment: "none"}); !errors.Is(err, ErrStaleScan) {
		t.Fatalf("stale session code accepted: %v", err)
	}
	if err := manager.DeleteRule(rule.ID); err != nil {
		t.Fatal(err)
	}
	if views, _ := manager.Rules(); len(views) != 0 {
		t.Fatalf("rule not deleted")
	}
}

func TestRulesPersistAcrossLaunches(t *testing.T) {
	configDirectory := t.TempDir()
	manager, _ := scannedManager(t, configDirectory)
	if _, err := manager.SaveRule(RuleRequest{Type: redact.RuleName, Pattern: "*trip*",
		Treatment: redact.TreatmentKeepExtension}); err != nil {
		t.Fatal(err)
	}
	relaunched, _ := scannedManager(t, configDirectory)
	views, err := relaunched.Rules()
	if err != nil || len(views) != 1 || views[0].Pattern != "*trip*" || views[0].Matches != 1 {
		t.Fatalf("views after relaunch = %+v %v", views, err)
	}
}

func TestUnreadableRulesFailClosedAndAreNeverOverwritten(t *testing.T) {
	configDirectory := t.TempDir()
	storePath := redact.StorePath(configDirectory)
	garbage := []byte("ULLAGE-RULES\x00 this is not decryptable")
	if err := os.WriteFile(storePath, garbage, 0o600); err != nil {
		t.Fatal(err)
	}
	manager, _ := scannedManager(t, configDirectory)
	summary := manager.RedactionSummary()
	if summary.Available || summary.Error == "" {
		t.Fatalf("summary = %+v", summary)
	}
	if _, err := manager.Children(summary.SessionCode, scanner.NoNode); !errors.Is(err,
		ErrRulesUnavailable) {
		t.Fatalf("children: %v", err)
	}
	if _, err := manager.SaveRule(RuleRequest{Type: redact.RuleName, Pattern: "x",
		Treatment: redact.TreatmentSubtree}); !errors.Is(err, ErrRulesUnavailable) {
		t.Fatalf("save rule: %v", err)
	}
	if _, err := manager.Rules(); !errors.Is(err, ErrRulesUnavailable) {
		t.Fatalf("rules: %v", err)
	}
	if err := manager.PersistPlaceholders(); err != nil && !errors.Is(err,
		ErrRulesUnavailable) {
		t.Fatalf("persist: %v", err)
	}
	if content, _ := os.ReadFile(storePath); string(content) != string(garbage) {
		t.Fatalf("unreadable rules file was overwritten")
	}

	aside, err := manager.ResetUnreadableRules()
	if err != nil {
		t.Fatal(err)
	}
	if content, _ := os.ReadFile(aside); string(content) != string(garbage) {
		t.Fatalf("old file not kept aside")
	}
	if !manager.RedactionSummary().Available {
		t.Fatalf("redaction still unavailable after reset")
	}
}
