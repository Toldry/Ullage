package session

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/toldry/ullage/internal/deletion"
)

// explainedFixture imports round A, then explains and opens categories 1 (low confidence)
// and 3, so both are selectable.
func explainedFixture(t *testing.T) (*Manager, map[string]string, string) {
	t.Helper()
	manager, view, lines := roundAFixture(t)
	for _, number := range []int{1, 3} {
		if _, err := manager.RequestExplanation(ExplanationRequest{Number: number,
			Queued: true}); err != nil {
			t.Fatal(err)
		}
	}
	reply := "@SESSION " + view.Code + "\n#1\nwhat: a\nlost: b\nstep: c\n#3\nwhat: a\n" +
		"lost: b\nstep: c\n@END " + view.Code
	if pasted, err := manager.Paste(PasteRequest{Batch: 2, Text: reply}); err != nil ||
		!pasted.Imported {
		t.Fatalf("round B import failed: %+v %v", pasted, err)
	}
	if _, err := manager.MarkExpanded("ai:1"); err != nil {
		t.Fatal(err)
	}
	tree := manager.Completed().Result.Tree
	return manager, lines, tree.Path(tree.Roots[0])
}

func statuses(plan DeletionPlan) map[string]deletion.Status {
	result := map[string]deletion.Status{}
	for _, item := range plan.Items {
		result[item.Key] = item.Status
	}
	return result
}

func waitForDeletion(t *testing.T, manager *Manager) DeletionRun {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if run := manager.DeletionProgress(); run.Finished {
			return run
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("deletion did not finish")
	return DeletionRun{}
}

// Safety rule 3: only items of the current results, in categories that may be deleted,
// are accepted; everything else is refused by name rather than dropped.
func TestDeletionRefusesKeysOutsideTheResults(t *testing.T) {
	manager, _, lines := roundAFixture(t)
	keys := []string{
		"ai:99|id:1",                  // no such category
		"ai:3|id:9999",                // no such item
		"ai:3|id:" + lines["photos"],  // no explanation yet (D-050)
		"ai:2|id:" + lines["clients"], // the AI suggests keeping it
		"recipe:npm-cache|node:1",     // no such recipe category in this scan
		"ai:3|id:" + lines["notes"],   // an item of another category
		`ai:3|..\..\Windows`,          // text is never a path
	}
	plan, err := manager.PrepareDeletion(keys)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Items) != len(keys) || plan.Ready.Count != 0 {
		t.Fatalf("plan = %+v", plan)
	}
	for _, item := range plan.Items {
		if item.Status != StatusRejected || item.Reason == "" {
			t.Errorf("%s: %s (%s)", item.Key, item.Status, item.Reason)
		}
	}
	if _, err := manager.ExecuteDeletion(ExecuteRequest{PlanID: plan.ID}); !errors.Is(err,
		ErrNothingReady) {
		t.Fatalf("execute with nothing ready: %v", err)
	}
}

func TestPermanentDeletionNeedsTypedConfirmation(t *testing.T) {
	manager, lines, root := explainedFixture(t)
	keys := []string{"ai:3|id:" + lines["photos"], "ai:1|id:" + lines["notes"]}
	plan, err := manager.PrepareDeletion(keys)
	if err != nil || plan.Ready.Count != 2 || plan.Confirmation != "delete 2" {
		t.Fatalf("plan = %+v %v", plan, err)
	}
	for _, wrong := range []string{"", "delete", "delete 3", "DELETE 2"} {
		if _, err := manager.ExecuteDeletion(ExecuteRequest{PlanID: plan.ID, Permanent: true,
			Confirmation: wrong}); !errors.Is(err, ErrConfirmation) {
			t.Fatalf("confirmation %q accepted: %v", wrong, err)
		}
	}
	if _, err := manager.ExecuteDeletion(ExecuteRequest{PlanID: "forged", Permanent: true,
		Confirmation: "delete 2"}); !errors.Is(err, ErrPlanExpired) {
		t.Fatalf("forged plan id: %v", err)
	}
	if _, err := manager.ExecuteDeletion(ExecuteRequest{PlanID: plan.ID, Permanent: true,
		Confirmation: "delete 2"}); err != nil {
		t.Fatal(err)
	}
	run := waitForDeletion(t, manager)
	if run.Removed.Count != 2 || run.Failed.Count != 0 || !run.Permanent {
		t.Fatalf("run = %+v", run)
	}
	for _, gone := range []string{"photos", "zorblax-notes/x.bin"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(gone))); !os.IsNotExist(err) {
			t.Errorf("%s still exists: %v", gone, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "clients")); err != nil {
		t.Errorf("an unselected folder was touched: %v", err)
	}
	// The plan is used up, and deleted items leave the results.
	if _, err := manager.ExecuteDeletion(ExecuteRequest{PlanID: plan.ID}); !errors.Is(err,
		ErrPlanExpired) {
		t.Fatalf("plan reused: %v", err)
	}
	view, _ := manager.Results()
	if findCategory(view, "ai:1") != nil || findCategory(view, "ai:3") != nil {
		t.Fatalf("deleted categories still listed: %+v", view.Categories)
	}
}

// Safety rule 6 at the session level: a change after the plan blocks the item at
// deletion time, even though the plan said it was ready.
func TestChangeAfterConfirmationBlocks(t *testing.T) {
	manager, lines, root := explainedFixture(t)
	plan, err := manager.PrepareDeletion([]string{"ai:3|id:" + lines["photos"]})
	if err != nil || plan.Ready.Count != 1 {
		t.Fatalf("plan = %+v %v", plan, err)
	}
	if err := os.WriteFile(filepath.Join(root, "photos", "2019", "new.jpg"),
		[]byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ExecuteDeletion(ExecuteRequest{PlanID: plan.ID}); err != nil {
		t.Fatal(err)
	}
	run := waitForDeletion(t, manager)
	if run.Removed.Count != 0 || run.Items[0].Status != deletion.StatusChanged ||
		!strings.Contains(run.Items[0].Reason, "1 file added") {
		t.Fatalf("run = %+v", run)
	}
	if _, err := os.Stat(filepath.Join(root, "photos", "2019", "trip.bin")); err != nil {
		t.Fatalf("a changed item was deleted: %v", err)
	}
	// Preparing again shows it as changed.
	again, _ := manager.PrepareDeletion([]string{"ai:3|id:" + lines["photos"]})
	if again.Changed.Count != 1 || statuses(again)["ai:3|id:"+lines["photos"]] !=
		deletion.StatusChanged {
		t.Fatalf("plan after change = %+v", again)
	}
}

func TestRecycleIsTheDefault(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "data"))
	manager, lines, root := explainedFixture(t)
	if filepath.VolumeName(root) != filepath.VolumeName(os.Getenv("XDG_DATA_HOME")) {
		t.Skip("trash and fixture on different volumes")
	}
	plan, _ := manager.PrepareDeletion([]string{"ai:3|id:" + lines["photos"]})
	if _, err := manager.ExecuteDeletion(ExecuteRequest{PlanID: plan.ID}); err != nil {
		t.Fatal(err)
	}
	run := waitForDeletion(t, manager)
	if run.Permanent || run.Removed.Count != 1 || run.Items[0].Status != deletion.StatusRecycled {
		t.Fatalf("run = %+v", run)
	}
}
