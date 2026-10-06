package session

import (
	"errors"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/toldry/ullage/internal/redact"
	"github.com/toldry/ullage/internal/storage"
)

func exportingManager(t *testing.T, configDirectory string) *Manager {
	t.Helper()
	manager, _ := scannedManager(t, configDirectory)
	// The fixture files are 2 MB, so lower the line threshold to list them.
	if _, err := manager.UpdateConfig(func(config *storage.Config) {
		config.PromptMinimumMegabytes = 1
	}); err != nil {
		t.Fatal(err)
	}
	return manager
}

var itemLine = regexp.MustCompile(`(?m)^ (\d+)(-\d+)? `)

func replyFor(code string, text string) string {
	ids := itemLine.FindAllStringSubmatch(text, -1)
	return "@SESSION " + code + "\ncat|conf|regen|del|ids\nEverything|low|no|0|" + ids[0][1] +
		"\n@END " + code
}

func TestExportAndPaste(t *testing.T) {
	manager := exportingManager(t, t.TempDir())
	view, err := manager.Export(false)
	if err != nil {
		t.Fatal(err)
	}
	if view.Code != manager.Completed().Code || len(view.Batches) != 1 || view.Items == 0 {
		t.Fatalf("view = %+v", view)
	}
	text := view.Batches[0].Text
	if strings.Contains(strings.ToLower(text), "zorblax") {
		t.Fatalf("always-redacted word in the prompt:\n%s", text)
	}

	bad, err := manager.Paste(PasteRequest{Batch: 1, Text: "@SESSION " + view.Code +
		"\nX|sure|yes|1|1\n@END " + view.Code})
	if err != nil || bad.Valid || bad.Imported || !strings.Contains(bad.Repair, "conf must be") {
		t.Fatalf("invalid reply: %+v %v", bad, err)
	}
	good, err := manager.Paste(PasteRequest{Batch: 1, Text: "Here you go:\n```\n" +
		replyFor(view.Code, text) + "\n```"})
	if err != nil || !good.Valid || !good.Imported || !good.Export.Batches[0].Imported {
		t.Fatalf("valid reply: %+v %v", good, err)
	}
	if _, imports := manager.Imports(); len(imports[1].Categories) != 1 {
		t.Fatalf("imports = %+v", imports)
	}
}

func TestRedactionChangeRebuildsWithANewCode(t *testing.T) {
	manager := exportingManager(t, t.TempDir())
	first, _ := manager.Export(false)
	if _, err := manager.SaveRule(RuleRequest{Type: redact.RuleName, Pattern: "*trip*",
		Treatment: redact.TreatmentKeepExtension}); err != nil {
		t.Fatal(err)
	}
	second, _ := manager.Export(false)
	if second.Code == first.Code || second.Stale {
		t.Fatalf("export not rebuilt with a new code: %s -> %s", first.Code, second.Code)
	}
	old, _ := manager.Paste(PasteRequest{Batch: 1,
		Text: replyFor(first.Code, first.Batches[0].Text)})
	if old.Valid || !old.Result.Failed("session") {
		t.Fatalf("reply to the superseded prompt accepted: %+v", old)
	}

	// Once a reply is imported, a later redaction change marks the export stale instead
	// of silently discarding the import.
	if result, _ := manager.Paste(PasteRequest{Batch: 1,
		Text: replyFor(second.Code, second.Batches[0].Text)}); !result.Imported {
		t.Fatalf("import failed: %+v", result)
	}
	if _, err := manager.SaveRule(RuleRequest{Type: redact.RuleName, Pattern: "*photo*",
		Treatment: redact.TreatmentSubtree}); err != nil {
		t.Fatal(err)
	}
	third, _ := manager.Export(false)
	if third.Code != second.Code || !third.Stale || !third.Batches[0].Imported {
		t.Fatalf("stale export not reported: %+v", third)
	}
	rebuilt, _ := manager.Export(true)
	if rebuilt.Code == second.Code || rebuilt.Stale || rebuilt.Batches[0].Imported {
		t.Fatalf("rebuild did not start over: %+v", rebuilt)
	}
}

// Safety rule 7: no prompt while the rules file cannot be read.
func TestExportFailsClosedWithUnreadableRules(t *testing.T) {
	configDirectory := t.TempDir()
	if err := os.WriteFile(redact.StorePath(configDirectory),
		[]byte("ULLAGE-RULES\x00 garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := exportingManager(t, configDirectory)
	if _, err := manager.Export(false); !errors.Is(err, ErrRulesUnavailable) {
		t.Fatalf("export with unreadable rules: %v", err)
	}
	if _, err := manager.Paste(PasteRequest{Batch: 1, Text: "x"}); !errors.Is(err,
		ErrRulesUnavailable) {
		t.Fatalf("paste with unreadable rules: %v", err)
	}
}
