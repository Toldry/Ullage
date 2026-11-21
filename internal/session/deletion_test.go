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

// explainedFixture imports the reply and opens category 1 (low confidence), so both
// deletable categories, 1 and 3, are selectable.
func explainedFixture(t *testing.T) (*Manager, map[string]string, string) {
	t.Helper()
	manager, _, lines := importedFixture(t)
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

// prepareAndWait starts verification and waits until every item has been checked.
func prepareAndWait(t *testing.T, manager *Manager, keys []string) (DeletionPlan, error) {
	t.Helper()
	plan, err := manager.PrepareDeletion(keys)
	if err != nil {
		return plan, err
	}
	for deadline := time.Now().Add(10 * time.Second); plan.Verifying; {
		if time.Now().After(deadline) {
			t.Fatal("verification did not finish")
		}
		time.Sleep(5 * time.Millisecond)
		if plan, err = manager.DeletionPlanProgress(); err != nil {
			return plan, err
		}
	}
	return plan, nil
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
	manager, _, lines := importedFixture(t)
	keys := []string{
		"ai:99|id:1",                  // no such category
		"ai:3|id:9999",                // no such item
		"ai:1|id:" + lines["notes"],   // low confidence, not opened yet
		"ai:2|id:" + lines["clients"], // the AI suggests keeping it
		"recipe:npm-cache|node:1",     // no such recipe category in this scan
		"ai:3|id:" + lines["notes"],   // an item of another category
		`ai:3|..\..\Windows`,          // text is never a path
	}
	plan, err := prepareAndWait(t, manager, keys)
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
	plan, err := prepareAndWait(t, manager, keys)
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
	recorded := manager.History()
	if len(recorded.Entries) != 1 || recorded.Entries[0].Items != 2 ||
		recorded.Entries[0].Deleted == 0 || recorded.Entries[0].Batches != 1 ||
		recorded.Current != recorded.Entries[0].Code || recorded.Reclaimed == 0 {
		t.Fatalf("history = %+v", recorded)
	}
	// A folder scan records its drive, never the folder's path.
	for _, drive := range recorded.Entries[0].Drives {
		if strings.Contains(drive, filepath.Base(root)) {
			t.Fatalf("history names the scanned folder: %v", recorded.Entries[0].Drives)
		}
	}
}

// Safety rule 6 at the session level: a change after the plan blocks the item at
// deletion time, even though the plan said it was ready.
func TestChangeAfterConfirmationBlocks(t *testing.T) {
	manager, lines, root := explainedFixture(t)
	plan, err := prepareAndWait(t, manager, []string{"ai:3|id:" + lines["photos"]})
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
	again, _ := prepareAndWait(t, manager, []string{"ai:3|id:" + lines["photos"]})
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
	plan, _ := prepareAndWait(t, manager, []string{"ai:3|id:" + lines["photos"]})
	if _, err := manager.ExecuteDeletion(ExecuteRequest{PlanID: plan.ID}); err != nil {
		t.Fatal(err)
	}
	run := waitForDeletion(t, manager)
	if run.Permanent || run.Removed.Count != 1 || run.Items[0].Status != deletion.StatusRecycled {
		t.Fatalf("run = %+v", run)
	}
}

func TestVerificationReportsProgress(t *testing.T) {
	manager, lines, _ := explainedFixture(t)
	release := make(chan struct{})
	beforeVerifying = func() { <-release }
	t.Cleanup(func() { beforeVerifying = func() {} })
	keys := []string{"ai:3|id:" + lines["photos"], "ai:1|id:" + lines["notes"], "ai:9|id:1"}
	started, err := manager.PrepareDeletion(keys)
	if err != nil {
		t.Fatal(err)
	}
	if started.ToCheck != 2 || started.Refused.Count != 1 || !started.Verifying {
		t.Fatalf("plan at start = %+v", started)
	}
	// Executing before the check finishes is refused.
	if _, err := manager.ExecuteDeletion(ExecuteRequest{PlanID: started.ID}); !errors.Is(err,
		ErrStillVerifying) {
		t.Fatalf("execute while verifying: %v", err)
	}
	close(release)
	plan, err := prepareAndWait(t, manager, keys)
	if err != nil || plan.Verifying || plan.Checked != 2 || plan.Ready.Count != 2 ||
		plan.FilesRead == 0 || plan.Current != "" {
		t.Fatalf("plan after = %+v %v", plan, err)
	}
	for _, item := range plan.Items {
		if item.Status == StatusPending {
			t.Fatalf("item left pending: %+v", item)
		}
	}
}

// Decision D-072: preparing with a re-scan accepts a changed item's new state, shows its
// new size, and a later change blocks it again.
func TestRescanAcceptsChangedItems(t *testing.T) {
	manager, lines, root := explainedFixture(t)
	key := "ai:3|id:" + lines["photos"]
	photos := filepath.Join(root, "photos")
	if err := os.WriteFile(filepath.Join(photos, "added.bin"), make([]byte, 5000),
		0o600); err != nil {
		t.Fatal(err)
	}
	plan, err := prepareAndWait(t, manager, []string{key})
	if err != nil || statuses(plan)[key] != deletion.StatusChanged {
		t.Fatalf("plain prepare = %+v %v", plan, err)
	}
	rescanned, err := manager.PrepareRescan([]string{key})
	for err == nil && rescanned.Verifying {
		time.Sleep(5 * time.Millisecond)
		rescanned, err = manager.DeletionPlanProgress()
	}
	if err != nil || rescanned.Ready.Count != 1 || !rescanned.Items[0].Rescanned ||
		rescanned.Items[0].Bytes == plan.Items[0].Bytes {
		t.Fatalf("re-scan = %+v %v", rescanned, err)
	}
	// The accepted state is kept for later plans; a new change blocks again.
	if again, _ := prepareAndWait(t, manager, []string{key}); statuses(again)[key] !=
		deletion.StatusReady || !again.Items[0].Rescanned {
		t.Fatalf("after re-scan = %+v", again)
	}
	if err := os.WriteFile(filepath.Join(photos, "later.bin"), []byte("x"),
		0o600); err != nil {
		t.Fatal(err)
	}
	if blocked, _ := prepareAndWait(t, manager, []string{key}); statuses(blocked)[key] !=
		deletion.StatusChanged || !strings.Contains(blocked.Items[0].Reason, "after the re-scan") {
		t.Fatalf("change after re-scan = %+v", blocked)
	}
}

// Decision D-078: a category the AI suggests keeping is refused until the user types
// "delete anyway" for it, and is refused again once the override is taken back, even for
// a plan prepared while it was on.
func TestDeleteAnywayOverridesTheAIKeep(t *testing.T) {
	manager, lines, root := explainedFixture(t)
	key := "ai:2|id:" + lines["clients"]
	plan, err := prepareAndWait(t, manager, []string{key})
	if err != nil || plan.Ready.Count != 0 || plan.Items[0].Status != StatusRejected ||
		!strings.Contains(plan.Items[0].Reason, "Delete anyway") {
		t.Fatalf("plan before the override = %+v %v", plan, err)
	}
	for _, wrong := range []string{"", "delete", "Delete anyway", "delete anyway "} {
		if _, err := manager.OverrideKeep("ai:2", true, wrong); !errors.Is(err,
			ErrKeepOverrideConfirmation) {
			t.Fatalf("confirmation %q accepted: %v", wrong, err)
		}
	}
	view, err := manager.OverrideKeep("ai:2", true, KeepOverrideConfirmation)
	if clients := findCategory(view, "ai:2"); err != nil || !clients.KeepOverridden ||
		!clients.Selectable || len(view.Kept) != 1 {
		t.Fatalf("after the override: %+v %v", clients, err)
	}
	plan, err = prepareAndWait(t, manager, []string{key})
	if err != nil || plan.Ready.Count != 1 || !plan.Items[0].KeepOverridden {
		t.Fatalf("plan after the override = %+v %v", plan, err)
	}

	// Taking the override back makes the prepared plan delete nothing.
	view, err = manager.OverrideKeep("ai:2", false, "")
	if clients := findCategory(view, "ai:2"); err != nil || clients.KeepOverridden ||
		clients.Selectable {
		t.Fatalf("after taking it back: %+v %v", clients, err)
	}
	if _, err := manager.ExecuteDeletion(ExecuteRequest{PlanID: plan.ID}); !errors.Is(err,
		ErrNothingReady) {
		t.Fatalf("execute after taking the override back: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "clients")); err != nil {
		t.Fatalf("a kept folder was touched: %v", err)
	}

	// With the override on again, the deletion goes through.
	if _, err := manager.OverrideKeep("ai:2", true, KeepOverrideConfirmation); err != nil {
		t.Fatal(err)
	}
	plan, _ = prepareAndWait(t, manager, []string{key})
	if _, err := manager.ExecuteDeletion(ExecuteRequest{PlanID: plan.ID, Permanent: true,
		Confirmation: plan.Confirmation}); err != nil {
		t.Fatal(err)
	}
	if run := waitForDeletion(t, manager); run.Removed.Count != 1 ||
		!run.Items[0].KeepOverridden {
		t.Fatalf("run = %+v", run)
	}
}

// The override is only for the AI's keep categories, and it lapses when the batch is
// imported again.
func TestDeleteAnywayIsOnlyForKeepCategories(t *testing.T) {
	manager, view, _ := importedFixture(t)
	if _, err := manager.OverrideKeep("ai:3", true, KeepOverrideConfirmation); !errors.Is(
		err, ErrNotKept) {
		t.Fatalf("override of a deletable category: %v", err)
	}
	if _, err := manager.OverrideKeep("ai:99", true, KeepOverrideConfirmation); !errors.Is(
		err, ErrNoSuchCategory) {
		t.Fatalf("override of an unknown category: %v", err)
	}
	if _, err := manager.OverrideKeep("ai:2", true, KeepOverrideConfirmation); err != nil {
		t.Fatal(err)
	}
	if again, _ := manager.Paste(PasteRequest{Batch: 1,
		Text: replyFor(view.Code, view.Batches[0].Text)}); !again.Imported {
		t.Fatalf("re-import failed: %+v", again)
	}
	results, _ := manager.Results()
	for _, category := range results.Kept {
		if category.KeepOverridden || category.Selectable {
			t.Fatalf("override survived a re-import: %+v", category)
		}
	}
}
