package session

import (
	"errors"
	"sort"
	"time"

	"github.com/toldry/ullage/internal/recipes"
	"github.com/toldry/ullage/internal/results"
)

// categoryState is per-export bookkeeping for the categories imported from replies,
// guarded by Manager.lock and reset with the export.
type categoryState struct {
	// nextNumber numbers imported categories uniquely within the export, so a category
	// keeps its key when other batches are imported again.
	nextNumber int
	expanded   map[string]bool
	// keepOverridden: AI "keep" categories the user chose to delete anyway (D-078).
	keepOverridden map[string]bool
}

func newCategoryState() categoryState {
	return categoryState{nextNumber: 1, expanded: map[string]bool{},
		keepOverridden: map[string]bool{}}
}

func (state *categoryState) allocateNumber() int {
	if state.nextNumber == 0 {
		*state = newCategoryState()
	}
	number := state.nextNumber
	state.nextNumber++
	return number
}

func (state *categoryState) forget(numbers []int) {
	for _, number := range numbers {
		delete(state.expanded, results.AIKey(number))
		delete(state.keepOverridden, results.AIKey(number))
	}
}

// importedCategory finds an imported category by number. Callers hold manager.lock.
func (manager *Manager) importedCategory(number int) (results.ImportedCategory, bool) {
	for batch, imported := range manager.exportState.imports {
		for index, candidate := range imported.Numbers {
			if candidate == number {
				return results.ImportedCategory{Number: number, Batch: batch,
					ImportedAt: imported.ImportedAt,
					Category:   imported.Categories[index]}, true
			}
		}
	}
	return results.ImportedCategory{}, false
}

// ResultsView is the category list of the Delete step.
type ResultsView struct {
	results.Model
	Code             string `json:"code"`
	BatchCount       int    `json:"batchCount"`
	BatchesImported  int    `json:"batchesImported"`
	MinimumItemBytes int64  `json:"minimumItemBytes"`
	// UnchangedDays is the age filter in effect; 0 when off.
	UnchangedDays int `json:"unchangedDays"`
}

// Results merges recipe categories with the imported AI categories.
func (manager *Manager) Results() (ResultsView, error) {
	manager.lock.Lock()
	defer manager.lock.Unlock()
	return manager.resultsView()
}

func (manager *Manager) resultsView() (ResultsView, error) {
	if manager.completed == nil {
		return ResultsView{}, errors.New("no finished scan yet")
	}
	state := &manager.exportState
	classifier := manager.completed.Classifier
	input := results.Input{
		Tree:       manager.completed.Result.Tree,
		ClaimRoots: manager.completed.Result.ClaimRoots,
		RecipeForClaim: func(claimID int32) *recipes.Recipe {
			if claim := classifier.ClaimByID(claimID); claim != nil {
				return claim.Recipe
			}
			return nil
		},
		Redactor:       state.redactor,
		Export:         state.export,
		Expanded:       state.categories.expanded,
		KeepOverridden: state.categories.keepOverridden,
		Deleted:        manager.deletion.deleted,
		MinimumBytes:   manager.promptOptions().MinimumItemBytes,
		Profile:        manager.CurrentProfile(),
	}
	if manager.unchangedDays > 0 {
		input.UnchangedBefore = time.Now().AddDate(0, 0, -manager.unchangedDays).Unix()
	}
	view := ResultsView{MinimumItemBytes: input.MinimumBytes,
		UnchangedDays: manager.unchangedDays}
	if state.export != nil {
		view.Code = state.export.Code
		view.BatchCount = len(state.export.Batches)
		batches := make([]int, 0, len(state.imports))
		for batch := range state.imports {
			batches = append(batches, batch)
		}
		sort.Ints(batches)
		for _, batch := range batches {
			imported := state.imports[batch]
			for index, category := range imported.Categories {
				input.Imported = append(input.Imported, results.ImportedCategory{
					Number: imported.Numbers[index], Batch: batch,
					ImportedAt: imported.ImportedAt, Category: category})
			}
		}
		view.BatchesImported = len(batches)
	}
	view.Model = results.Build(input)
	return view, nil
}

// ErrNoSuchCategory means a request named a category that is not in the results.
var ErrNoSuchCategory = errors.New("no such category")

// MarkExpanded records that the user opened a category's row, which is what unlocks
// deleting a low-confidence category.
func (manager *Manager) MarkExpanded(key string) (ResultsView, error) {
	manager.lock.Lock()
	defer manager.lock.Unlock()
	view, err := manager.resultsView()
	if err != nil {
		return view, err
	}
	for _, category := range append(append([]results.Category{}, view.Categories...),
		view.Kept...) {
		if category.Key == key {
			if manager.exportState.categories.expanded == nil {
				manager.exportState.categories = newCategoryState()
			}
			manager.exportState.categories.expanded[key] = true
			return manager.resultsView()
		}
	}
	return ResultsView{}, ErrNoSuchCategory
}

// KeepOverrideConfirmation is what the user types to delete a category the AI suggests
// keeping (decision D-078).
const KeepOverrideConfirmation = "delete anyway"

// ErrKeepOverrideConfirmation means the typed confirmation did not match.
var ErrKeepOverrideConfirmation = errors.New(
	`type "` + KeepOverrideConfirmation + `" to delete what the AI suggests keeping`)

// ErrNotKept means an override named a category that is not an AI "keep" category.
var ErrNotKept = errors.New("only categories the AI suggests keeping can be overridden")

// OverrideKeep lets the user select an AI "keep" category for deletion after typing
// KeepOverrideConfirmation, or, with override false, puts the guard back. Nothing else
// is relaxed: managed-only recipes, low-confidence rows that were not opened, the age
// filter and every check at deletion time still apply.
func (manager *Manager) OverrideKeep(key string, override bool,
	confirmation string) (ResultsView, error) {
	if override && confirmation != KeepOverrideConfirmation {
		return ResultsView{}, ErrKeepOverrideConfirmation
	}
	manager.lock.Lock()
	defer manager.lock.Unlock()
	view, err := manager.resultsView()
	if err != nil {
		return view, err
	}
	if !isKept(view, key) {
		for _, category := range view.Categories {
			if category.Key == key {
				return ResultsView{}, ErrNotKept
			}
		}
		return ResultsView{}, ErrNoSuchCategory
	}
	state := &manager.exportState.categories
	if state.keepOverridden == nil {
		state.keepOverridden = map[string]bool{}
	}
	if override {
		state.keepOverridden[key] = true
	} else {
		delete(state.keepOverridden, key)
	}
	return manager.resultsView()
}

func isKept(view ResultsView, key string) bool {
	for _, category := range view.Kept {
		if category.Key == key && category.Source == results.SourceAI {
			return true
		}
	}
	return false
}

// maximumUnchangedDays bounds the age filter to something a person could mean.
const maximumUnchangedDays = 100 * 365

// SetUnchangedDays sets the age filter: only items whose newest change is at least this
// many days old are listed and can be selected; 0 turns it off. Deletion resolves the
// selection against the same filtered list.
func (manager *Manager) SetUnchangedDays(days int) (ResultsView, error) {
	if days < 0 || days > maximumUnchangedDays {
		return ResultsView{}, errors.New("days must be between 0 and 36500")
	}
	manager.lock.Lock()
	defer manager.lock.Unlock()
	manager.unchangedDays = days
	return manager.resultsView()
}
