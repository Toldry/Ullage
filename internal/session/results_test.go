package session

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/toldry/ullage/internal/redact"
	"github.com/toldry/ullage/internal/results"
)

// roundAFixture exports the fixture scan with "northwind" redacted and imports one reply:
// a low-confidence deletable category, a keep category and a medium one.
func roundAFixture(t *testing.T) (*Manager, ExportView, map[string]string) {
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
		"Client " + lines["clientsPlaceholder"] + " files|high|no|0|" + lines["clients"] +
		"\n" + "Old photos|med|partially|1|" + lines["photos"] + "\n@END " + view.Code
	if pasted, err := manager.Paste(PasteRequest{Batch: 1, Text: reply}); err != nil ||
		!pasted.Imported {
		t.Fatalf("round A import failed: %+v %v", pasted, err)
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

func TestResultsAfterRoundA(t *testing.T) {
	manager, view, _ := roundAFixture(t)
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
	notes := findCategory(resultsView, "ai:1")
	if !notes.NeedsExpand || notes.Selectable || !strings.Contains(notes.Name, "zorblax") {
		t.Fatalf("low confidence category = %+v", notes)
	}
	expanded, err := manager.MarkExpanded("ai:1")
	if err != nil || !findCategory(expanded, "ai:1").Selectable {
		t.Fatalf("expanding did not unlock: %v", err)
	}
	if _, err := manager.MarkExpanded("ai:99"); !errors.Is(err, ErrNoSuchCategory) {
		t.Fatalf("expanding an unknown category: %v", err)
	}
}

func TestRoundBExplanations(t *testing.T) {
	manager, view, _ := roundAFixture(t)
	for _, number := range []int{3, 1} {
		if _, err := manager.RequestExplanation(ExplanationRequest{Number: number,
			Queued: true}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := manager.RequestExplanation(ExplanationRequest{Number: 42,
		Queued: true}); !errors.Is(err, ErrNoSuchCategory) {
		t.Fatalf("queueing an unknown category: %v", err)
	}
	exported, _ := manager.Export(false)
	explanation := exported.Explanation
	if explanation == nil || explanation.Round != 2 || explanation.Categories != 2 {
		t.Fatalf("explanation view = %+v", explanation)
	}
	text := explanation.Text
	// Round B sends the assistant's own names back, never the restored real ones.
	for _, secret := range []string{"zorblax", "northwind"} {
		if strings.Contains(strings.ToLower(text), secret) {
			t.Fatalf("round B prompt contains %q:\n%s", secret, text)
		}
	}
	if !strings.Contains(text, "under 25 words") || !strings.Contains(text, "@SESSION "+
		view.Code) || strings.Index(text, "#3 Old photos") > strings.Index(text, "#1 Notes") {
		t.Fatalf("round B prompt:\n%s", text)
	}
	categories := text[strings.Index(text, "Categories:\n")+len("Categories:\n"):]
	for _, line := range strings.Split(strings.TrimSpace(categories), "\n") {
		if !strings.HasPrefix(line, "#") && line != "(end of categories)" {
			t.Fatalf("unexpected data line %q", line)
		}
	}

	bad, _ := manager.Paste(PasteRequest{Batch: 2, Text: "@SESSION " + view.Code +
		"\n#7\nwhat: x\nlost: y\nstep: z\n@END " + view.Code})
	if bad.Imported || bad.Explanations == nil || !strings.Contains(bad.Repair, "#7") {
		t.Fatalf("invalid round B reply: %+v", bad)
	}
	good, err := manager.Paste(PasteRequest{Batch: 2, Text: "@SESSION " + view.Code +
		"\n#1\nwhat: Notes.\nlost: The notes.\nstep: Open them first.\n@END " + view.Code})
	if err != nil || !good.Imported || good.Export.Explanation == nil ||
		good.Export.Explanation.Round != 3 || good.Export.Explanation.Categories != 1 {
		t.Fatalf("round B import: %+v %v", good, err)
	}
	resultsView, _ := manager.Results()
	notes := findCategory(resultsView, "ai:1")
	if notes.ExplanationState != results.ExplanationReceived ||
		notes.Explanation.What != "Notes." || notes.Explanation.Round != 2 ||
		findCategory(resultsView, "ai:3").ExplanationState != results.ExplanationQueued ||
		resultsView.QueuedCount != 1 || resultsView.ExplanationRound != 3 {
		t.Fatalf("results after round B = %+v", resultsView)
	}
	if _, err := manager.RequestExplanation(ExplanationRequest{Number: 1,
		Queued: true}); err == nil {
		t.Fatal("an explained category was queued again")
	}

	// Importing batch 1 again replaces its categories; their explanations and queue
	// entries go with them.
	if again, _ := manager.Paste(PasteRequest{Batch: 1,
		Text: replyFor(view.Code, view.Batches[0].Text)}); !again.Imported {
		t.Fatalf("re-import failed: %+v", again)
	}
	resultsView, _ = manager.Results()
	if resultsView.QueuedCount != 0 || resultsView.ExplanationRound != 0 ||
		findCategory(resultsView, "ai:1") != nil || findCategory(resultsView, "ai:4") == nil {
		t.Fatalf("results after re-import = %+v", resultsView)
	}
	if exported, _ := manager.Export(false); exported.Explanation != nil {
		t.Fatalf("round B prompt left over: %+v", exported.Explanation)
	}
}
