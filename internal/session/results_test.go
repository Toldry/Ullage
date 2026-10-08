package session

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/toldry/ullage/internal/redact"
	"github.com/toldry/ullage/internal/results"
)

// importedFixture exports the fixture scan with "northwind" redacted and imports one
// reply: a low-confidence deletable category, a keep category and a medium one. The
// deletable ones carry explanation lines, which mention the redacted names by placeholder.
func importedFixture(t *testing.T) (*Manager, ExportView, map[string]string) {
	t.Helper()
	manager := exportingManager(t, t.TempDir())
	if _, err := manager.SaveRule(RuleRequest{Type: redact.RuleName, Pattern: "northwind",
		Treatment: redact.TreatmentSubtree}); err != nil {
		t.Fatal(err)
	}
	view, err := manager.Export(false)
	if err != nil {
		t.Fatal(err)
	}
	lines := map[string]string{}
	for _, match := range regexp.MustCompile(`(?m)^ (\d+) \S+ (\S+)`).FindAllStringSubmatch(
		view.Batches[0].Text, -1) {
		switch {
		case strings.HasSuffix(match[2], "/x.bin"):
			lines["notes"] = match[1]
			lines["notesPlaceholder"] = strings.TrimSuffix(match[2], "/x.bin")
		case strings.HasPrefix(match[2], "clients/"):
			lines["clients"] = match[1]
			lines["clientsPlaceholder"] = strings.TrimPrefix(match[2], "clients/")
		case strings.HasPrefix(match[2], "photos"):
			lines["photos"] = match[1]
		}
	}
	if len(lines) != 5 {
		t.Fatalf("unexpected prompt layout %v:\n%s", lines, view.Batches[0].Text)
	}
	reply := "@SESSION " + view.Code + "\ncat|conf|regen|del|ids\n" +
		"Notes in " + lines["notesPlaceholder"] + "|low|no|1|" + lines["notes"] + "\n" +
		"what: Notes kept in " + lines["notesPlaceholder"] + ".\nlost: The notes.\n" +
		"step: Open them first.\n" +
		"Client " + lines["clientsPlaceholder"] + " files|high|no|0|" + lines["clients"] +
		"\n" + "Old photos|med|partially|1|" + lines["photos"] + "\nwhat: Photos.\n" +
		"lost: Photos without another copy.\nstep: Check for a backup.\n@END " + view.Code
	if pasted, err := manager.Paste(PasteRequest{Batch: 1, Text: reply}); err != nil ||
		!pasted.Imported {
		t.Fatalf("import failed: %+v %v", pasted, err)
	}
	return manager, view, lines
}

func findCategory(view ResultsView, key string) *results.Category {
	for _, list := range [][]results.Category{view.Categories, view.Kept} {
		for index := range list {
			if list[index].Key == key {
				return &list[index]
			}
		}
	}
	return nil
}

func TestResultsAfterImport(t *testing.T) {
	manager, view, _ := importedFixture(t)
	resultsView, err := manager.Results()
	if err != nil {
		t.Fatal(err)
	}
	if resultsView.Code != view.Code || resultsView.BatchesImported != 1 ||
		resultsView.BatchCount != 1 || len(resultsView.Categories) != 2 ||
		len(resultsView.Kept) != 1 {
		t.Fatalf("results = %+v", resultsView)
	}
	clients := findCategory(resultsView, "ai:2")
	if clients.Name != "Client northwind files" || clients.Selectable {
		t.Fatalf("kept category = %+v", clients)
	}
	photos := findCategory(resultsView, "ai:3")
	if !photos.Selectable || photos.ExplanationState != results.ExplanationReceived ||
		photos.Explanation.Lost != "Photos without another copy." {
		t.Fatalf("explained category = %+v", photos)
	}
	notes := findCategory(resultsView, "ai:1")
	if !notes.NeedsExpand || notes.Selectable || !strings.Contains(notes.Name, "zorblax") ||
		!strings.Contains(notes.Explanation.What, "zorblax") {
		t.Fatalf("low confidence category = %+v", notes)
	}
	// Expanding clears the low-confidence lock.
	expanded, err := manager.MarkExpanded("ai:1")
	if unlocked := findCategory(expanded, "ai:1"); err != nil || unlocked.NeedsExpand ||
		!unlocked.Selectable {
		t.Fatalf("after expanding: %+v %v", unlocked, err)
	}
	if _, err := manager.MarkExpanded("ai:99"); !errors.Is(err, ErrNoSuchCategory) {
		t.Fatalf("expanding an unknown category: %v", err)
	}

	// Importing batch 1 again replaces its categories and forgets what was opened.
	if again, _ := manager.Paste(PasteRequest{Batch: 1,
		Text: replyFor(view.Code, view.Batches[0].Text)}); !again.Imported {
		t.Fatalf("re-import failed: %+v", again)
	}
	resultsView, _ = manager.Results()
	if findCategory(resultsView, "ai:1") != nil || findCategory(resultsView, "ai:4") == nil {
		t.Fatalf("results after re-import = %+v", resultsView)
	}
}

// A deletable row without explanation lines is a problem with a repair prompt, not an
// import (decision D-066).
func TestUnexplainedDeletableRowIsNotImported(t *testing.T) {
	manager, view, lines := importedFixture(t)
	reply := "@SESSION " + view.Code + "\ncat|conf|regen|del|ids\nOld photos|med|no|1|" +
		lines["photos"] + "\n@END " + view.Code
	pasted, err := manager.Paste(PasteRequest{Batch: 1, Text: reply})
	if err != nil || pasted.Imported || !strings.Contains(pasted.Repair, "what:") {
		t.Fatalf("unexplained row: %+v %v", pasted, err)
	}
}

// Imported replies stay available for review in step 5.
func TestImportedRepliesStayReviewable(t *testing.T) {
	manager, view, _ := importedFixture(t)
	exported, _ := manager.Export(false)
	if !strings.Contains(exported.Batches[0].Reply, "@SESSION "+view.Code) {
		t.Fatalf("batch reply not kept: %q", exported.Batches[0].Reply)
	}
}
