package session

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/toldry/ullage/internal/deletion"
	"github.com/toldry/ullage/internal/overview"
	"github.com/toldry/ullage/internal/results"
)

// planLifetime bounds how long a confirmation screen may sit open before its plan must be
// prepared again; everything is re-verified at deletion time regardless.
const planLifetime = 15 * time.Minute

// StatusRejected marks a selection key that does not name a deletable item of the
// current results: unknown, from an older scan, or of a category that may not be deleted.
const StatusRejected deletion.Status = "rejected"

// deletionState is guarded by Manager.lock.
type deletionState struct {
	plan *deletionPlan
	run  *deletion.Run
	// runItems are the plan items being deleted, in run order.
	runItems []DeletionItem
	// deleted holds results.DeletedKey values; reset by a new scan.
	deleted map[string]bool
	// baselines are re-scanned states the items must still match, by results.DeletedKey
	// (decision D-072); reset by a new scan.
	baselines map[string]deletion.Fingerprint
	// recorded: the finished run's outcomes are reflected in deleted and the history.
	// The run is reported finished only then, so a refresh never shows stale results.
	recorded bool
}

type deletionPlan struct {
	id string
	// created is when verification finished; the plan's lifetime counts from there.
	created time.Time
	started time.Time
	items   []DeletionItem
	targets map[string]deletion.Target
	// rescan accepts the current state of items whose contents changed (D-072).
	rescan bool

	verifying    bool
	current      string
	checked      int
	checkedBytes int64
	filesRead    int64
	foldersRead  int64
}

// DeletionItem is one row of the confirmation and progress screens.
type DeletionItem struct {
	Key          string          `json:"key"`
	Category     string          `json:"category"`
	CategoryName string          `json:"categoryName"`
	Path         string          `json:"path"`
	Kind         string          `json:"kind"`
	Bytes        int64           `json:"bytes"`
	Status       deletion.Status `json:"status"`
	Reason       string          `json:"reason,omitempty"`
	// NowBytes is the size found on disk when it differs from the scan.
	NowBytes int64 `json:"nowBytes,omitempty"`
	// CloudSync: deleting it also deletes the cloud copy (decision D-052).
	CloudSync bool `json:"cloudSync,omitempty"`
	// Warning is the recipe's brute-force warning, repeated where the user confirms.
	Warning string `json:"warning,omitempty"`
	// Rescanned: the item was read again and is checked against that state, not the
	// scan's; Bytes is then its size at the re-scan.
	Rescanned bool `json:"rescanned,omitempty"`
	// KeepOverridden: the AI suggested keeping it; the user allowed deleting it anyway.
	KeepOverridden bool `json:"keepOverridden,omitempty"`
}

// Tally counts items and bytes.
type Tally struct {
	Count int   `json:"count"`
	Bytes int64 `json:"bytes"`
}

// DeletionPlan is the confirmation screen's data.
type DeletionPlan struct {
	ID      string         `json:"id"`
	Items   []DeletionItem `json:"items"`
	Ready   Tally          `json:"ready"`
	Changed Tally          `json:"changed"`
	Missing Tally          `json:"missing"`
	Refused Tally          `json:"refused"`
	// Confirmation is what the user types to delete permanently.
	Confirmation string `json:"confirmation"`

	// Verification progress: the items are checked one after another in the background.
	Verifying      bool    `json:"verifying"`
	ToCheck        int     `json:"toCheck"`
	Checked        int     `json:"checked"`
	ToCheckBytes   int64   `json:"toCheckBytes"`
	CheckedBytes   int64   `json:"checkedBytes"`
	FilesRead      int64   `json:"filesRead"`
	FoldersRead    int64   `json:"foldersRead"`
	Current        string  `json:"current,omitempty"`
	ElapsedSeconds float64 `json:"elapsedSeconds"`
}

// Errors of the deletion API.
var (
	ErrDeletionRunning = errors.New("a deletion is in progress")
	ErrStillVerifying  = errors.New("the items are still being checked")
	ErrPlanExpired     = errors.New("the confirmation is out of date; review the items again")
	ErrConfirmation    = errors.New("type the confirmation exactly to delete permanently")
	ErrNothingReady    = errors.New("no item is ready to delete")
)

func (state *deletionState) running() bool {
	return state.run != nil && !state.run.State().Finished
}

// resolveSelection turns selection keys from the browser into targets. Only items of
// categories the Results screen allows to select are accepted; everything else is
// reported as rejected, never silently dropped. Callers hold manager.lock.
func (manager *Manager) resolveSelection(keys []string) ([]DeletionItem,
	map[string]deletion.Target, error) {
	view, err := manager.resultsView()
	if err != nil {
		return nil, nil, err
	}
	categories := map[string]results.Category{}
	for _, category := range append(append([]results.Category{}, view.Categories...),
		view.Kept...) {
		categories[category.Key] = category
	}
	var items []DeletionItem
	targets := map[string]deletion.Target{}
	seen := map[string]bool{}
	reject := func(key, reason string) {
		items = append(items, DeletionItem{Key: key, Status: StatusRejected, Reason: reason})
	}
	for _, key := range keys {
		categoryKey, itemKey, rest := splitSelectionKey(key)
		category, found := categories[categoryKey]
		switch {
		case seen[key]:
			continue
		case !found:
			reject(key, "not part of the current results")
			continue
		case !category.Selectable:
			reject(key, refusalReason(category))
			continue
		}
		seen[key] = true
		selected := category.Remaining
		if !rest {
			selected = nil
			for _, item := range append(append([]results.Item{}, category.Items...),
				category.Remaining...) {
				if item.Key == itemKey {
					selected = append(selected, item)
				}
			}
		}
		if len(selected) == 0 {
			reject(key, "not part of the current results")
			continue
		}
		for _, item := range selected {
			full := category.Key + "|" + item.Key
			if _, duplicate := targets[full]; duplicate {
				continue
			}
			target := targetFor(full, category, item)
			entry := DeletionItem{Key: full, Category: category.Key,
				CategoryName: category.Name, Path: item.Path, Kind: item.Kind,
				Bytes: item.Bytes, CloudSync: item.CloudSync,
				Warning: category.BruteForce.Warning, KeepOverridden: category.KeepOverridden}
			if baseline, found := manager.deletion.baselines[baselineKey(target)]; found {
				target.Baseline = &baseline
				entry.Rescanned, entry.Bytes = true, baseline.LogicalBytes
			}
			targets[full] = target
			items = append(items, entry)
		}
	}
	return items, targets, nil
}

func splitSelectionKey(key string) (string, string, bool) {
	if category, found := strings.CutSuffix(key, "/rest"); found {
		return category, "", true
	}
	category, item, _ := strings.Cut(key, "|")
	return category, item, false
}

func refusalReason(category results.Category) string {
	switch {
	case !category.BruteForce.Allowed && category.Source == results.SourceRecipe:
		return "this recipe offers managed cleanup only: " + category.BruteForce.Reason
	case !category.BruteForce.Allowed:
		return "the AI suggests keeping these; turn on \"Allow deleting against AI recommendation\" first"
	case category.NeedsExplanation:
		return "the AI reply did not explain this category"
	case category.NeedsExpand:
		return "low confidence: open the category's details first"
	default:
		return "this category cannot be deleted"
	}
}

func targetFor(key string, category results.Category, item results.Item) deletion.Target {
	kind := deletion.KindFolder
	switch item.Kind {
	case "file":
		kind = deletion.KindFile
	case "loose":
		kind = deletion.KindLoose
	}
	return deletion.Target{Key: key, Category: category.Key, Node: item.Node, Kind: kind,
		Listed: item.Listed, Tier: item.Tier, ClaimID: item.ClaimID, Bytes: item.Bytes}
}

func baselineKey(target deletion.Target) string {
	return results.DeletedKey(target.Node, target.Kind == deletion.KindLoose)
}

// guard builds the deletion guard for the finished scan. Callers hold manager.lock.
func (manager *Manager) guard() *deletion.Guard {
	tree := manager.completed.Result.Tree
	locations := deletion.Locations{
		ProtectedTrees:  manager.environment.ProtectedTrees,
		CaseInsensitive: manager.environment.Locations.CaseInsensitive,
	}
	for _, root := range tree.Roots {
		locations.Roots = append(locations.Roots, tree.Path(root))
	}
	variables := manager.environment.Locations.Variables
	for _, name := range []string{"UserProfile", "CurrentUserProfile", "ProfilesDirectory"} {
		locations.KeptPaths = append(locations.KeptPaths, variables[name]...)
	}
	locations.KeptPaths = append(locations.KeptPaths, manager.environment.ConfigDirectory)
	return &deletion.Guard{Tree: tree, Reader: manager.environment.Reader,
		Locations: locations, Canonical: manager.environment.Canonical}
}

// StatusPending marks an item that has not been checked yet.
const StatusPending deletion.Status = "pending"

// PrepareDeletion resolves a selection and starts re-verifying every item against the
// disk in the background; DeletionPlanProgress reports how far it got. The disk work runs
// without the session lock; the scan tree it reads never changes.
func (manager *Manager) PrepareDeletion(keys []string) (DeletionPlan, error) {
	return manager.prepareDeletion(keys, false)
}

// PrepareRescan is PrepareDeletion that also re-scans: an item whose contents changed
// since the scan, and that passes every other check, is read again and from then on
// checked against what was read. It is shown with its new size for the user to confirm
// (decision D-072).
func (manager *Manager) PrepareRescan(keys []string) (DeletionPlan, error) {
	return manager.prepareDeletion(keys, true)
}

func (manager *Manager) prepareDeletion(keys []string, rescan bool) (DeletionPlan, error) {
	manager.lock.Lock()
	defer manager.lock.Unlock()
	if manager.completed == nil {
		return DeletionPlan{}, errors.New("no finished scan yet")
	}
	if manager.deletion.running() {
		return DeletionPlan{}, ErrDeletionRunning
	}
	items, targets, err := manager.resolveSelection(keys)
	if err != nil {
		return DeletionPlan{}, err
	}
	id, err := randomID()
	if err != nil {
		return DeletionPlan{}, err
	}
	tree := manager.completed.Result.Tree
	profile := manager.CurrentProfile()
	for index := range items {
		target, found := targets[items[index].Key]
		if !found {
			continue
		}
		items[index].Status = StatusPending
		items[index].Path = overview.DisplayPath(tree.Path(target.Node), profile)
		if target.Kind == deletion.KindLoose {
			items[index].Path += "/*"
		}
	}
	plan := &deletionPlan{id: id, started: time.Now(), items: items, targets: targets,
		rescan: rescan, verifying: true}
	manager.deletion.plan = plan
	go manager.verifyPlan(plan, manager.guard())
	return planView(plan), nil
}

// beforeVerifying runs at the start of each verification; tests replace it to hold the
// check, so what happens while it runs is tested without depending on timing.
var beforeVerifying = func() {}

// verifyPlan checks the plan's items one by one, publishing each result as it comes.
// A newer plan supersedes this one; its results are then simply dropped.
func (manager *Manager) verifyPlan(plan *deletionPlan, guard *deletion.Guard) {
	beforeVerifying()
	for index := range plan.items {
		manager.lock.Lock()
		if manager.deletion.plan != plan {
			manager.lock.Unlock()
			return
		}
		item := plan.items[index]
		target, found := plan.targets[item.Key]
		if found {
			plan.current = item.Path
		}
		manager.lock.Unlock()
		if !found {
			continue
		}

		check := guard.Verify(target)

		manager.lock.Lock()
		if manager.deletion.plan != plan {
			manager.lock.Unlock()
			return
		}
		plan.items[index].Status, plan.items[index].Reason = check.Status, check.Reason
		if plan.rescan && check.OnlyContentChanged {
			manager.acceptRescan(plan, index, target, check.Now)
		} else if check.Status == deletion.StatusChanged && check.Now.LogicalBytes > 0 {
			plan.items[index].NowBytes = check.Now.LogicalBytes
		}
		plan.checked++
		plan.checkedBytes += item.Bytes
		plan.filesRead += check.Now.Files
		plan.foldersRead += check.Now.Directories
		manager.lock.Unlock()
	}
	manager.lock.Lock()
	defer manager.lock.Unlock()
	if manager.deletion.plan != plan {
		return
	}
	plan.verifying, plan.current, plan.created = false, "", time.Now()
	view := planView(plan)
	manager.logf("delete", "verified %d: %d ready, %d changed, %d missing, %d refused",
		len(plan.items), view.Ready.Count, view.Changed.Count, view.Missing.Count,
		view.Refused.Count)
}

// acceptRescan records what a re-scan read as the item's new baseline and marks the item
// ready at its new size. Callers hold manager.lock.
func (manager *Manager) acceptRescan(plan *deletionPlan, index int, target deletion.Target,
	now deletion.Fingerprint) {
	if manager.deletion.baselines == nil {
		manager.deletion.baselines = map[string]deletion.Fingerprint{}
	}
	manager.deletion.baselines[baselineKey(target)] = now
	target.Baseline = &now
	plan.targets[plan.items[index].Key] = target
	item := &plan.items[index]
	item.Status, item.Reason, item.NowBytes = deletion.StatusReady, "", 0
	item.Rescanned, item.Bytes = true, now.LogicalBytes
}

// DeletionPlanProgress returns the current plan, while or after it is verified.
func (manager *Manager) DeletionPlanProgress() (DeletionPlan, error) {
	manager.lock.Lock()
	defer manager.lock.Unlock()
	if manager.deletion.plan == nil {
		return DeletionPlan{}, ErrPlanExpired
	}
	return planView(manager.deletion.plan), nil
}

func planView(plan *deletionPlan) DeletionPlan {
	view := DeletionPlan{ID: plan.id, Items: append([]DeletionItem{}, plan.items...),
		Verifying: plan.verifying, Checked: plan.checked, CheckedBytes: plan.checkedBytes,
		FilesRead: plan.filesRead, FoldersRead: plan.foldersRead, Current: plan.current}
	ended := plan.created
	if plan.verifying {
		ended = time.Now()
	}
	view.ElapsedSeconds = ended.Sub(plan.started).Seconds()
	for _, item := range plan.items {
		if item.Status != StatusRejected {
			view.ToCheck++
			view.ToCheckBytes += item.Bytes
		}
		switch item.Status {
		case StatusPending:
		case deletion.StatusReady:
			view.Ready.add(item.Bytes)
		case deletion.StatusChanged:
			view.Changed.add(item.Bytes)
		case deletion.StatusMissing:
			view.Missing.add(item.Bytes)
		default:
			view.Refused.add(item.Bytes)
		}
	}
	view.Confirmation = fmt.Sprintf("delete %d", view.Ready.Count)
	return view
}

func (tally *Tally) add(bytes int64) {
	tally.Count++
	tally.Bytes += bytes
}

func randomID() (string, error) {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

// ExecuteRequest starts deleting the ready items of a plan.
type ExecuteRequest struct {
	PlanID       string `json:"planId"`
	Permanent    bool   `json:"permanent"`
	Confirmation string `json:"confirmation"`
}

// ExecuteDeletion starts the run. Permanent deletion requires the typed confirmation
// (safety rule 5). The selection is resolved again, so a category that became
// unselectable since the plan was made is not deleted, and every item is re-verified
// right before it is deleted.
func (manager *Manager) ExecuteDeletion(request ExecuteRequest) (DeletionRun, error) {
	manager.lock.Lock()
	defer manager.lock.Unlock()
	plan := manager.deletion.plan
	switch {
	case manager.deletion.running():
		return DeletionRun{}, ErrDeletionRunning
	case plan != nil && plan.id == request.PlanID && plan.verifying:
		return DeletionRun{}, ErrStillVerifying
	case plan == nil || plan.id != request.PlanID ||
		time.Since(plan.created) > planLifetime:
		return DeletionRun{}, ErrPlanExpired
	}
	view := planView(plan)
	if request.Permanent && request.Confirmation != view.Confirmation {
		return DeletionRun{}, ErrConfirmation
	}
	var readyKeys []string
	for _, item := range plan.items {
		if item.Status == deletion.StatusReady {
			readyKeys = append(readyKeys, item.Key)
		}
	}
	if len(readyKeys) == 0 {
		return DeletionRun{}, ErrNothingReady
	}
	items, targets, err := manager.resolveSelection(readyKeys)
	if err != nil {
		return DeletionRun{}, err
	}
	var ordered []deletion.Target
	var runItems []DeletionItem
	for _, item := range items {
		if target, found := targets[item.Key]; found {
			ordered = append(ordered, target)
			runItems = append(runItems, planItem(plan, item.Key))
		}
	}
	if len(ordered) == 0 {
		return DeletionRun{}, ErrNothingReady
	}
	// The plan is used up: a second execute must go through a new confirmation.
	manager.deletion.plan = nil
	manager.deletion.runItems = runItems
	manager.deletion.recorded = false
	manager.deletion.run = deletion.Start(manager.guard(), manager.environment.Remover,
		ordered, request.Permanent, manager.recordDeleted)
	return manager.runView(), nil
}

func planItem(plan *deletionPlan, key string) DeletionItem {
	for _, item := range plan.items {
		if item.Key == key {
			return item
		}
	}
	return DeletionItem{Key: key}
}

// recordDeleted remembers fully deleted items, so Results stops listing them.
func (manager *Manager) recordDeleted(run *deletion.Run) {
	manager.lock.Lock()
	defer manager.lock.Unlock()
	if manager.deletion.run != run {
		return
	}
	if manager.deletion.deleted == nil {
		manager.deletion.deleted = map[string]bool{}
	}
	manager.deletion.recorded = true
	view := manager.runView()
	manager.recordDeletion(view)
	manager.deletion.recorded = false
	state := run.State()
	for index, outcome := range state.Outcomes {
		line := string(outcome.Status) + " " + manager.safeNode(run.Targets()[index].Node)
		if run.Targets()[index].Kind == deletion.KindLoose {
			line += "/*"
		}
		if outcome.Reason != "" {
			line += ": " + manager.safeText(outcome.Reason)
		}
		manager.environment.Logger.Write("delete", line)
	}
	manager.logf("delete", "%s finished: %d removed (%d bytes), %d failed, %d skipped",
		map[bool]string{true: "permanent deletion", false: "recycling"}[view.Permanent],
		view.Removed.Count, view.Removed.Bytes, view.Failed.Count, view.Skipped.Count)
	for index, outcome := range state.Outcomes {
		if outcome.Status == deletion.StatusRecycled || outcome.Status == deletion.StatusDeleted {
			target := run.Targets()[index]
			manager.deletion.deleted[results.DeletedKey(target.Node,
				target.Kind == deletion.KindLoose)] = true
		}
	}
	manager.deletion.recorded = true
}

// DeletionRun is the progress and final report.
type DeletionRun struct {
	Permanent bool           `json:"permanent"`
	Total     int            `json:"total"`
	Done      int            `json:"done"`
	Stopping  bool           `json:"stopping"`
	Finished  bool           `json:"finished"`
	Items     []DeletionItem `json:"items"`
	Removed   Tally          `json:"removed"`
	Failed    Tally          `json:"failed"`
	Skipped   Tally          `json:"skipped"`
	Pending   Tally          `json:"pending"`
	// Current is the item being verified and deleted right now.
	Current        string  `json:"current,omitempty"`
	ElapsedSeconds float64 `json:"elapsedSeconds"`
}

// runView describes the current or last run. Callers hold manager.lock.
func (manager *Manager) runView() DeletionRun {
	run := manager.deletion.run
	if run == nil {
		return DeletionRun{Items: []DeletionItem{}, Finished: true}
	}
	state := run.State()
	view := DeletionRun{Permanent: state.Permanent, Total: state.Total, Done: state.Done,
		Stopping: state.Stopping, Finished: state.Finished && manager.deletion.recorded}
	ended := state.Ended
	if ended.IsZero() {
		ended = time.Now()
	}
	view.ElapsedSeconds = ended.Sub(state.Started).Seconds()
	if !state.Finished && state.Done < len(manager.deletion.runItems) {
		view.Current = manager.deletion.runItems[state.Done].Path
	}
	for index, item := range manager.deletion.runItems {
		if index >= len(state.Outcomes) {
			item.Status, item.Reason = deletion.StatusReady, ""
			view.Pending.add(item.Bytes)
			view.Items = append(view.Items, item)
			continue
		}
		outcome := state.Outcomes[index]
		item.Status, item.Reason = outcome.Status, outcome.Reason
		switch outcome.Status {
		case deletion.StatusRecycled, deletion.StatusDeleted:
			view.Removed.add(item.Bytes)
		case deletion.StatusFailed, deletion.StatusPartial:
			view.Failed.add(item.Bytes)
		default:
			view.Skipped.add(item.Bytes)
		}
		view.Items = append(view.Items, item)
	}
	return view
}

// DeletionProgress returns the current or last run.
func (manager *Manager) DeletionProgress() DeletionRun {
	manager.lock.Lock()
	defer manager.lock.Unlock()
	return manager.runView()
}

// StopDeletion ends the run after the current item.
func (manager *Manager) StopDeletion() DeletionRun {
	manager.lock.Lock()
	defer manager.lock.Unlock()
	if manager.deletion.run != nil {
		manager.deletion.run.Stop()
	}
	return manager.runView()
}

// DeletionRunning reports whether a deletion is in progress, which keeps the app alive.
func (manager *Manager) DeletionRunning() bool {
	manager.lock.Lock()
	defer manager.lock.Unlock()
	return manager.deletion.running()
}
