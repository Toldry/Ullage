package session

import (
	"errors"
	"sort"
	"time"

	"github.com/toldry/ullage/internal/prompt"
	"github.com/toldry/ullage/internal/recipes"
	"github.com/toldry/ullage/internal/response"
	"github.com/toldry/ullage/internal/results"
)

// explanationState is round B bookkeeping, guarded by Manager.lock and reset with the
// export.
type explanationState struct {
	nextNumber int
	received   map[int]results.ReceivedExplanation
	// queued keeps the order categories were requested in, which the prompt follows.
	queued          []int
	expanded        map[string]bool
	completedRounds int
}

func newExplanationState() explanationState {
	return explanationState{nextNumber: 1, received: map[int]results.ReceivedExplanation{},
		expanded: map[string]bool{}}
}

func (state *explanationState) allocateNumber() int {
	if state.nextNumber == 0 {
		*state = newExplanationState()
	}
	number := state.nextNumber
	state.nextNumber++
	return number
}

func (state *explanationState) isQueued(number int) bool {
	for _, queued := range state.queued {
		if queued == number {
			return true
		}
	}
	return false
}

func (state *explanationState) unqueue(number int) {
	kept := state.queued[:0]
	for _, queued := range state.queued {
		if queued != number {
			kept = append(kept, queued)
		}
	}
	state.queued = kept
}

func (state *explanationState) forget(numbers []int) {
	for _, number := range numbers {
		delete(state.received, number)
		delete(state.expanded, results.AIKey(number))
		state.unqueue(number)
	}
}

// ExplanationView is the round B prompt on the Export and Paste screens.
type ExplanationView struct {
	Round         int    `json:"round"`
	Text          string `json:"text"`
	Categories    int    `json:"categories"`
	Characters    int    `json:"characters"`
	TokenEstimate int    `json:"tokenEstimate"`
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

// explanationRound is the stepper position of the round B prompt, or 0 when nothing is
// queued. Callers hold manager.lock.
func (manager *Manager) explanationRound() int {
	state := &manager.exportState
	if state.export == nil || len(state.explanations.queued) == 0 {
		return 0
	}
	return len(state.export.Batches) + state.explanations.completedRounds + 1
}

func (manager *Manager) explanationRequests() []prompt.ExplanationRequest {
	var requests []prompt.ExplanationRequest
	for _, number := range manager.exportState.explanations.queued {
		if imported, found := manager.importedCategory(number); found {
			requests = append(requests, prompt.ExplanationRequest{Number: number,
				Name: imported.Category.Name, IDs: imported.Category.IDs})
		}
	}
	return requests
}

// explanationView builds the round B prompt. Callers hold manager.lock.
func (manager *Manager) explanationView() *ExplanationView {
	round := manager.explanationRound()
	if round == 0 {
		return nil
	}
	requests := manager.explanationRequests()
	text := prompt.ExplanationPrompt(manager.exportState.export.Code, round, requests,
		response.MaximumExplanationSteps, response.ExplanationWordTarget)
	return &ExplanationView{Round: round, Text: text, Categories: len(requests),
		Characters: len(text), TokenEstimate: approximateTokens(text)}
}

// pasteExplanations validates a round B reply and imports it only when it has no
// problems. Callers hold manager.lock.
func (manager *Manager) pasteExplanations(text string) PasteResult {
	state := &manager.exportState
	expectation := response.ExplanationExpectation{Code: state.export.Code,
		Round: manager.explanationRound(), Numbers: append([]int{},
			state.explanations.queued...)}
	parsed := response.ParseExplanations(text, expectation)
	result := PasteResult{Explanations: &parsed, Valid: parsed.Valid(),
		Result: response.Result{Categories: []response.Category{},
			Problems: []response.Problem{}}}
	if result.Valid {
		now := time.Now()
		for _, explanation := range parsed.Explanations {
			state.explanations.received[explanation.Number] = results.ReceivedExplanation{
				Explanation: explanation, Round: expectation.Round, ReceivedAt: now}
			state.explanations.unqueue(explanation.Number)
		}
		state.explanations.completedRounds++
		result.Imported = true
	} else {
		result.Repair = response.RepairExplanationPrompt(parsed, expectation)
	}
	stale := state.redactionVersion != manager.redaction.version
	result.Export = manager.exportView(state.export, stale)
	return result
}

// ResultsView is the Results screen's data.
type ResultsView struct {
	results.Model
	Code             string `json:"code"`
	BatchCount       int    `json:"batchCount"`
	BatchesImported  int    `json:"batchesImported"`
	QueuedCount      int    `json:"queuedCount"`
	ExplanationRound int    `json:"explanationRound"`
	MinimumItemBytes int64  `json:"minimumItemBytes"`
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
		Redactor:     state.redactor,
		Export:       state.export,
		Explanations: state.explanations.received,
		Queued:       map[int]bool{},
		Expanded:     state.explanations.expanded,
		Deleted:      manager.deletion.deleted,
		MinimumBytes: manager.promptOptions().MinimumItemBytes,
		Profile:      manager.CurrentProfile(),
	}
	for _, number := range state.explanations.queued {
		input.Queued[number] = true
	}
	view := ResultsView{MinimumItemBytes: input.MinimumBytes,
		QueuedCount:      len(state.explanations.queued),
		ExplanationRound: manager.explanationRound()}
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

// ExplanationRequest queues or unqueues an AI category for round B.
type ExplanationRequest struct {
	Number int  `json:"number"`
	Queued bool `json:"queued"`
}

// RequestExplanation changes the round B queue. Only AI categories without an
// explanation can be queued.
func (manager *Manager) RequestExplanation(request ExplanationRequest) (ResultsView, error) {
	manager.lock.Lock()
	defer manager.lock.Unlock()
	state := &manager.exportState.explanations
	if _, found := manager.importedCategory(request.Number); !found {
		return ResultsView{}, ErrNoSuchCategory
	}
	if _, explained := state.received[request.Number]; explained && request.Queued {
		return ResultsView{}, errors.New("this category is already explained")
	}
	if request.Queued && !state.isQueued(request.Number) {
		state.queued = append(state.queued, request.Number)
	} else if !request.Queued {
		state.unqueue(request.Number)
	}
	return manager.resultsView()
}

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
			if manager.exportState.explanations.expanded == nil {
				manager.exportState.explanations = newExplanationState()
			}
			manager.exportState.explanations.expanded[key] = true
			return manager.resultsView()
		}
	}
	return ResultsView{}, ErrNoSuchCategory
}
