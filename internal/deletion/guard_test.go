package deletion

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/toldry/ullage/internal/platform"
	"github.com/toldry/ullage/internal/scanner"
	"github.com/toldry/ullage/internal/scanner/scannertest"
)

const megabyte = 1 << 20

// tiersByName gives a folder with a listed name, and everything below it, that tier and
// claim.
type tiersByName map[string]scanner.Classification

func (tiers tiersByName) Root(string) (scanner.Classification, scanner.ClassifierState) {
	return scanner.Classification{Tier: scanner.TierUnknown, ClaimID: scanner.NoClaim}, true
}

func (tiers tiersByName) Child(parent scanner.Classification, _ scanner.ClassifierState,
	name string, _ platform.EntryKind) (scanner.Classification, scanner.ClassifierState) {
	if classification, found := tiers[name]; found {
		return classification, true
	}
	return parent, true
}

func (tiers tiersByName) ClaimLabel(int32) string { return "claim" }

func native(slashPath string) string { return filepath.FromSlash(slashPath) }

type fixture struct {
	files  *scannertest.FileSystem
	result *scanner.Result
	guard  *Guard
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	files := scannertest.New()
	add := func(path string, megabytes int64) {
		files.AddFile("/c/"+path, megabytes*megabyte, 1000)
	}
	add("Windows/System32/kernel.bin", 50)
	add("Program Files/App/app.bin", 50)
	add("Users/mara/projects/web/node_modules/a.bin", 30)
	add("Users/mara/projects/web/node_modules/b.bin", 40)
	add("Users/mara/projects/web/src/main.bin", 2)
	add("Users/mara/Downloads/setup.iso", 900)
	add("Users/mara/Downloads/notes.txt", 0)
	add("Users/mara/AppData/Local/npm-cache/blob.bin", 300)
	add("Users/mara/AppData/Local/npm-cache/nested-other/x.bin", 20)
	add("Users/mara/AppData/Local/Mixed/sys/s.bin", 5)
	add("Users/mara/AppData/Local/Mixed/data.bin", 5)
	// Loose contents: small files directly in Clutter, an unlisted folder, a listed one.
	for _, name := range []string{"a.tmp", "b.tmp", "c.tmp"} {
		files.AddFile("/c/Users/mara/Clutter/"+name, 4096, 1000)
	}
	add("Users/mara/Clutter/old/x.bin", 3)
	add("Users/mara/Clutter/keep-listed/y.bin", 200)
	files.AddLink("/c/Users/mara/Clutter/shortcut", "/c/Windows")
	// A recipe folder split into entries (decision D-070): small files and two entries.
	files.AddFile("/c/Users/mara/AppData/Local/Temp/note.tmp", 4096, 1000)
	add("Users/mara/AppData/Local/Temp/setup/x.bin", 50)
	add("Users/mara/AppData/Local/Temp/busy/y.bin", 50)

	tiers := tiersByName{
		"Windows":       {Tier: scanner.TierSystem, ClaimID: scanner.NoClaim},
		"Program Files": {Tier: scanner.TierSystem, ClaimID: scanner.NoClaim},
		"npm-cache":     {Tier: scanner.TierKnown, ClaimID: 0},
		"nested-other":  {Tier: scanner.TierKnown, ClaimID: 1},
		"sys":           {Tier: scanner.TierSystem, ClaimID: scanner.NoClaim},
		"Temp":          {Tier: scanner.TierKnown, ClaimID: 2},
	}
	result := files.Scan(t, "/c", tiers)
	return &fixture{files: files, result: result, guard: &Guard{Tree: result.Tree,
		Reader: files, Locations: Locations{
			Roots:          []string{native("/c")},
			ProtectedTrees: []string{native("/c/Windows"), native("/c/Program Files")},
			KeptPaths:      []string{native("/c/Users/mara"), native("/c/Users")},
		}}}
}

func (fixture *fixture) node(t *testing.T, slashPath string) scanner.NodeIndex {
	t.Helper()
	return scannertest.Find(t, fixture.result, "/c/"+slashPath)
}

func (fixture *fixture) unknownFolder(t *testing.T, slashPath string) Target {
	return Target{Key: slashPath, Node: fixture.node(t, slashPath), Kind: KindFolder,
		Tier: scanner.TierUnknown, ClaimID: scanner.NoClaim}
}

func expectStatus(t *testing.T, check Check, status Status, reason string) {
	t.Helper()
	if check.Status != status || !strings.Contains(check.Reason, reason) {
		t.Fatalf("status = %s (%q), want %s containing %q", check.Status, check.Reason,
			status, reason)
	}
}

func TestReadyTargetIsVerified(t *testing.T) {
	fixture := newFixture(t)
	check := fixture.guard.Verify(fixture.unknownFolder(t, "Users/mara/projects/web/node_modules"))
	expectStatus(t, check, StatusReady, "")
	if check.Now.Files != 2 || check.Now.LogicalBytes != 70*megabyte ||
		len(check.Paths) != 1 || check.Now != check.Scan {
		t.Fatalf("check = %+v", check)
	}
}

// Safety rule 4: the System tier, the Windows folder and Program Files are never deleted,
// whatever category a target came from.
func TestSystemAndProtectedLocationsAreRefused(t *testing.T) {
	fixture := newFixture(t)
	cases := []struct {
		name   string
		target Target
		reason string
	}{
		{"system tier claimed as unknown", fixture.unknownFolder(t, "Windows/System32"),
			"inside " + native("/c/Windows")},
		{"program files", fixture.unknownFolder(t, "Program Files/App"),
			"inside " + native("/c/Program Files")},
		{"folder holding a system subfolder", fixture.unknownFolder(t,
			"Users/mara/AppData/Local/Mixed"), "System files"},
		{"profile", fixture.unknownFolder(t, "Users/mara"), "contains " +
			native("/c/Users/mara")},
		{"profiles folder", fixture.unknownFolder(t, "Users"), "contains"},
		{"scanned root", Target{Node: fixture.result.Tree.Roots[0], Kind: KindFolder,
			Tier: scanner.TierUnknown}, ""},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			expectStatus(t, fixture.guard.Verify(testCase.target), StatusProtected,
				testCase.reason)
		})
	}
}

func TestSystemTierIsRefusedEvenOutsideProtectedTrees(t *testing.T) {
	fixture := newFixture(t)
	fixture.guard.Locations.ProtectedTrees = nil
	expectStatus(t, fixture.guard.Verify(fixture.unknownFolder(t, "Windows/System32")),
		StatusProtected, "System files")
}

func TestOutsideScannedRootsIsRefused(t *testing.T) {
	fixture := newFixture(t)
	fixture.guard.Locations.Roots = []string{native("/c/Users/mara/projects")}
	expectStatus(t, fixture.guard.Verify(fixture.unknownFolder(t, "Users/mara/Downloads")),
		StatusProtected, "not inside a scanned folder")
}

func TestRecipeTargetMustNotContainOtherCategories(t *testing.T) {
	fixture := newFixture(t)
	target := Target{Node: fixture.node(t, "Users/mara/AppData/Local/npm-cache"),
		Kind: KindFolder, Tier: scanner.TierKnown, ClaimID: 0}
	expectStatus(t, fixture.guard.Verify(target), StatusProtected, "another category")
	// The tier must match too: an AI category cannot delete a recipe's folder.
	expectStatus(t, fixture.guard.Verify(fixture.unknownFolder(t,
		"Users/mara/AppData/Local/npm-cache")), StatusProtected, "another category")
}

func TestUnverifiableScanStateIsRefused(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*scanner.Node)
		reason string
	}{
		{"excluded", func(node *scanner.Node) { node.Flags |= scanner.FlagExcluded },
			"excluded"},
		{"denied", func(node *scanner.Node) { node.Flags |= scanner.FlagAccessDenied },
			"could not read"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newFixture(t)
			src := fixture.result.Tree.Node(fixture.node(t, "Users/mara/projects/web/src"))
			testCase.mutate(src)
			expectStatus(t, fixture.guard.Verify(fixture.unknownFolder(t,
				"Users/mara/projects/web")), StatusProtected, testCase.reason)
		})
	}
}

// Safety rule 6: any change since the scan blocks the item.
func TestChangesSinceTheScanBlock(t *testing.T) {
	folder := "Users/mara/projects/web/node_modules"
	cases := []struct {
		name   string
		change func(*scannertest.FileSystem)
		status Status
		reason string
	}{
		{"modified", func(files *scannertest.FileSystem) {
			files.Touch("/c/"+folder+"/a.bin", 5000)
		}, StatusChanged, "after the scan"},
		{"file added", func(files *scannertest.FileSystem) {
			files.AddFile("/c/"+folder+"/new.bin", 10, 1000)
		}, StatusChanged, "1 file added"},
		{"file removed", func(files *scannertest.FileSystem) {
			files.Remove("/c/" + folder + "/b.bin")
		}, StatusChanged, "1 file removed"},
		{"link added", func(files *scannertest.FileSystem) {
			files.AddLink("/c/"+folder+"/escape", "/c/Windows")
		}, StatusChanged, "1 link added"},
		{"missing", func(files *scannertest.FileSystem) {
			files.Remove("/c/" + folder)
		}, StatusMissing, "no longer exists"},
		{"replaced", func(files *scannertest.FileSystem) {
			files.Remove("/c/" + folder)
			files.AddFile("/c/"+folder+"/a.bin", 30*megabyte, 1000)
		}, StatusChanged, "replaced"},
		{"ancestor swapped for a link", func(files *scannertest.FileSystem) {
			files.Remove("/c/Users/mara/projects")
			files.AddLink("/c/Users/mara/projects", "/c/Windows")
		}, StatusChanged, "no longer a plain folder"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newFixture(t)
			testCase.change(fixture.files)
			expectStatus(t, fixture.guard.Verify(fixture.unknownFolder(t, folder)),
				testCase.status, testCase.reason)
		})
	}
}

// Cloud-synced and cloud-only items may be deleted (the Results screen warns about them).
func TestCloudSyncedItemsAreAllowed(t *testing.T) {
	fixture := newFixture(t)
	src := fixture.result.Tree.Node(fixture.node(t, "Users/mara/projects/web/src"))
	src.Attributes |= platform.AttributeManagedReparsePoint
	src.NotLocalBytes = 10
	expectStatus(t, fixture.guard.Verify(fixture.unknownFolder(t, "Users/mara/projects/web")),
		StatusReady, "")
}

func TestChangedFileBlocks(t *testing.T) {
	fixture := newFixture(t)
	target := Target{Node: fixture.node(t, "Users/mara/Downloads/setup.iso"), Kind: KindFile,
		Tier: scanner.TierUnknown}
	expectStatus(t, fixture.guard.Verify(target), StatusReady, "")
	fixture.files.Touch("/c/Users/mara/Downloads/setup.iso", 99999)
	expectStatus(t, fixture.guard.Verify(target), StatusChanged, "after the scan")
	// A target of the wrong kind is refused before the disk is read.
	target.Kind = KindFolder
	expectStatus(t, fixture.guard.Verify(target), StatusProtected, "not a folder")
}

// fakeRemover deletes from the fake file system, optionally failing for some paths.
type fakeRemover struct {
	files    *scannertest.FileSystem
	failFor  map[string]error
	recycled []string
}

func (remover *fakeRemover) remove(path string) error {
	if err, fails := remover.failFor[path]; fails {
		return err
	}
	remover.files.Remove(filepath.ToSlash(path))
	return nil
}

func (remover *fakeRemover) Recycle(paths []string) error {
	var failures []error
	for _, path := range paths {
		if err := remover.remove(path); err != nil {
			failures = append(failures, err)
			continue
		}
		remover.recycled = append(remover.recycled, path)
	}
	return errors.Join(failures...)
}

func (remover *fakeRemover) RemovePermanently(path string) error { return remover.remove(path) }

func TestDeleteReportsWhatHappened(t *testing.T) {
	fixture := newFixture(t)
	remover := &fakeRemover{files: fixture.files}
	folder := "Users/mara/projects/web/node_modules"
	outcome := fixture.guard.Delete(fixture.unknownFolder(t, folder), remover, false)
	if outcome.Status != StatusRecycled || fixture.files.Exists("/c/"+folder) ||
		len(remover.recycled) != 1 {
		t.Fatalf("outcome = %+v", outcome)
	}
	// Deleting it again finds it missing instead of touching anything.
	again := fixture.guard.Delete(fixture.unknownFolder(t, folder), remover, true)
	if again.Status != StatusMissing || len(remover.recycled) != 1 {
		t.Fatalf("second delete = %+v", again)
	}
}

func TestDeleteNeverTouchesABlockedTarget(t *testing.T) {
	fixture := newFixture(t)
	remover := &fakeRemover{files: fixture.files}
	fixture.files.Touch("/c/Users/mara/Downloads/setup.iso", 5000)
	outcome := fixture.guard.Delete(fixture.unknownFolder(t, "Users/mara/Downloads"), remover,
		true)
	if outcome.Status != StatusChanged || !fixture.files.Exists("/c/Users/mara/Downloads") {
		t.Fatalf("outcome = %+v", outcome)
	}
}

func TestFailedAndPartialDeletions(t *testing.T) {
	fixture := newFixture(t)
	folder := "Users/mara/projects/web/node_modules"
	inUse := &fakeRemover{files: fixture.files, failFor: map[string]error{
		native("/c/" + folder): platform.ErrInUse}}
	failed := fixture.guard.Delete(fixture.unknownFolder(t, folder), inUse, false)
	if failed.Status != StatusFailed || !strings.Contains(failed.Reason, "nothing changed") ||
		!strings.Contains(failed.Reason, "in use") || failed.Remaining != 1 {
		t.Fatalf("failed = %+v", failed)
	}

	loose := fixture.looseTarget(t)
	partial := &fakeRemover{files: fixture.files, failFor: map[string]error{
		native("/c/Users/mara/Clutter/b.tmp"): platform.ErrInUse}}
	outcome := fixture.guard.Delete(loose, partial, true)
	if outcome.Status != StatusPartial || outcome.Remaining != 1 ||
		!strings.Contains(outcome.Reason, "1 of 4 entries remain") {
		t.Fatalf("partial = %+v", outcome)
	}
}

func (fixture *fixture) looseTarget(t *testing.T) Target {
	return Target{Node: fixture.node(t, "Users/mara/Clutter"), Kind: KindLoose,
		Listed: []scanner.NodeIndex{fixture.node(t, "Users/mara/Clutter/keep-listed")},
		Tier:   scanner.TierUnknown, ClaimID: scanner.NoClaim}
}

// Loose contents: small files and unlisted folders go; listed items and links stay.
func TestLooseContents(t *testing.T) {
	fixture := newFixture(t)
	target := fixture.looseTarget(t)
	check := fixture.guard.Verify(target)
	expectStatus(t, check, StatusReady, "")
	if len(check.Paths) != 4 || check.Now.Files != 4 {
		t.Fatalf("check = %+v paths %v", check, check.Paths)
	}
	outcome := fixture.guard.Delete(target, &fakeRemover{files: fixture.files}, true)
	if outcome.Status != StatusDeleted {
		t.Fatalf("outcome = %+v", outcome)
	}
	for _, kept := range []string{"keep-listed", "shortcut"} {
		if !fixture.files.Exists("/c/Users/mara/Clutter/" + kept) {
			t.Errorf("%s was deleted", kept)
		}
	}
	for _, gone := range []string{"a.tmp", "old"} {
		if fixture.files.Exists("/c/Users/mara/Clutter/" + gone) {
			t.Errorf("%s was not deleted", gone)
		}
	}
}

func TestLooseContentsChangeBlocks(t *testing.T) {
	cases := map[string]func(*scannertest.FileSystem){
		"small file modified": func(files *scannertest.FileSystem) {
			files.Touch("/c/Users/mara/Clutter/a.tmp", 7777)
		},
		"small file added": func(files *scannertest.FileSystem) {
			files.AddFile("/c/Users/mara/Clutter/d.tmp", 4096, 1000)
		},
		"folder appeared": func(files *scannertest.FileSystem) {
			files.AddDirectory("/c/Users/mara/Clutter/new-folder")
		},
		"unlisted folder changed": func(files *scannertest.FileSystem) {
			files.AddFile("/c/Users/mara/Clutter/old/y.bin", 1, 1000)
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			fixture := newFixture(t)
			change(fixture.files)
			if check := fixture.guard.Verify(fixture.looseTarget(t)); check.Status ==
				StatusReady {
				t.Fatalf("change not detected: %+v", check)
			}
		})
	}
}

func TestRunCompletesAndStops(t *testing.T) {
	fixture := newFixture(t)
	targets := []Target{
		fixture.unknownFolder(t, "Users/mara/projects/web/node_modules"),
		{Node: fixture.node(t, "Users/mara/Downloads/setup.iso"), Kind: KindFile,
			Tier: scanner.TierUnknown},
	}
	finished := make(chan *Run, 1)
	run := Start(fixture.guard, &fakeRemover{files: fixture.files}, targets, false,
		func(run *Run) { finished <- run })
	<-finished
	state := run.State()
	if !state.Finished || state.Done != 2 || state.Outcomes[0].Status != StatusRecycled ||
		state.Outcomes[1].Status != StatusRecycled {
		t.Fatalf("state = %+v", state)
	}

	fixture = newFixture(t)
	gate := make(chan struct{})
	blocking := &blockingRemover{fakeRemover: fakeRemover{files: fixture.files}, gate: gate}
	finished = make(chan *Run, 1)
	run = Start(fixture.guard, blocking, targets, false, func(run *Run) { finished <- run })
	run.Stop()
	close(gate)
	state = (<-finished).State()
	skipped := 0
	for _, outcome := range state.Outcomes {
		if outcome.Status == StatusSkipped {
			skipped++
		}
	}
	if !state.Stopping || skipped == 0 || !fixture.files.Exists("/c/Users/mara/Downloads/setup.iso") {
		t.Fatalf("stop not honored: %+v", state)
	}
}

type blockingRemover struct {
	fakeRemover
	gate chan struct{}
}

func (remover *blockingRemover) Recycle(paths []string) error {
	<-remover.gate
	return remover.fakeRemover.Recycle(paths)
}

// Entries of a split recipe folder are checked one by one: a change inside one entry
// blocks only that entry, and the folder's small files go without touching any entry.
func TestSplitEntriesAreIndependent(t *testing.T) {
	fixture := newFixture(t)
	temp := "Users/mara/AppData/Local/Temp"
	entry := func(name string) Target {
		return Target{Node: fixture.node(t, temp+"/"+name), Kind: KindFolder,
			Tier: scanner.TierKnown, ClaimID: 2}
	}
	small := Target{Node: fixture.node(t, temp), Kind: KindLoose, Tier: scanner.TierKnown,
		ClaimID: 2, Listed: []scanner.NodeIndex{fixture.node(t, temp+"/setup"),
			fixture.node(t, temp+"/busy")}}
	fixture.files.AddFile("/c/"+temp+"/busy/new.bin", 4096, 2000)
	expectStatus(t, fixture.guard.Verify(entry("busy")), StatusChanged, "")
	expectStatus(t, fixture.guard.Verify(entry("setup")), StatusReady, "")
	remover := &fakeRemover{files: fixture.files}
	if outcome := fixture.guard.Delete(small, remover, true); outcome.Status != StatusDeleted {
		t.Fatalf("small files = %+v", outcome)
	}
	if outcome := fixture.guard.Delete(entry("setup"), remover, true); outcome.Status !=
		StatusDeleted {
		t.Fatalf("unchanged entry = %+v", outcome)
	}
	if fixture.files.Exists("/c/"+temp+"/note.tmp") ||
		fixture.files.Exists("/c/"+temp+"/setup") || !fixture.files.Exists("/c/"+temp+"/busy") {
		t.Fatal("wrong entries removed")
	}
}

// Decision D-072: a re-scan's state, once accepted, is what the item must match; any
// change after it blocks again. Location checks are not relaxed by it.
func TestRescanBaseline(t *testing.T) {
	fixture := newFixture(t)
	target := fixture.unknownFolder(t, "Users/mara/Downloads")
	fixture.files.AddFile("/c/Users/mara/Downloads/new.bin", 4096, 2000)
	changed := fixture.guard.Verify(target)
	if changed.Status != StatusChanged || !changed.OnlyContentChanged {
		t.Fatalf("changed = %+v", changed)
	}
	target.Baseline = &changed.Now
	expectStatus(t, fixture.guard.Verify(target), StatusReady, "")
	fixture.files.AddFile("/c/Users/mara/Downloads/later.bin", 4096, 3000)
	expectStatus(t, fixture.guard.Verify(target), StatusChanged, "after the re-scan")

	// A refusal is never just a content change, so a re-scan cannot accept it.
	protected := fixture.unknownFolder(t, "Windows/System32")
	if check := fixture.guard.Verify(protected); check.OnlyContentChanged {
		t.Fatalf("protected item offered for re-scan: %+v", check)
	}
}
