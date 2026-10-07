package session

import (
	"errors"
	"time"

	"github.com/toldry/ullage/internal/prompt"
	"github.com/toldry/ullage/internal/redact"
	"github.com/toldry/ullage/internal/response"
)

// exportState is guarded by Manager.lock.
type exportState struct {
	export *prompt.Export
	// redactor is the one the export was rendered with, so names are restored exactly as
	// the assistant saw them even after the rules change.
	redactor         *redact.Redactor
	redactionVersion int
	imports          map[int]BatchImport
	explanations     explanationState
}

// BatchImport is a validated reply for one batch.
type BatchImport struct {
	Categories []response.Category `json:"categories"`
	// Numbers gives each category a number that stays unique for this export, so round B
	// can refer to it even after other batches are imported again.
	Numbers    []int     `json:"numbers"`
	ImportedAt time.Time `json:"importedAt"`
}

// BatchView describes one batch on the Export and Paste screens.
type BatchView struct {
	prompt.Batch
	Characters      int  `json:"characters"`
	TokenEstimate   int  `json:"tokenEstimate"`
	Imported        bool `json:"imported"`
	CategoryCount   int  `json:"categoryCount"`
	ClassifiedItems int  `json:"classifiedItems"`
}

// ExportView is the Export screen's data.
type ExportView struct {
	Code             string      `json:"code"`
	Batches          []BatchView `json:"batches"`
	Items            int         `json:"items"`
	RepresentedBytes int64       `json:"representedBytes"`
	UnknownBytes     int64       `json:"unknownBytes"`
	MinimumItemBytes int64       `json:"minimumItemBytes"`
	// Stale means redactions changed after a reply was imported; the export keeps the
	// imported results until the user rebuilds it.
	Stale bool `json:"stale"`
	// Explanation is the round B prompt, present while categories are queued.
	Explanation *ExplanationView `json:"explanation,omitempty"`
}

func (manager *Manager) promptOptions() prompt.Options {
	options := prompt.DefaultOptions
	if manager.config.PromptMinimumMegabytes > 0 {
		options.MinimumItemBytes = int64(manager.config.PromptMinimumMegabytes) << 20
	}
	if manager.config.PromptBatchLines > 0 {
		options.BatchLines = manager.config.PromptBatchLines
	}
	if manager.config.PromptMaximumItems > 0 {
		options.MaximumItems = manager.config.PromptMaximumItems
	}
	return options
}

// currentExport returns the export, building or rebuilding it when needed. Callers hold
// manager.lock. A rebuild gets a new session code, so a reply to an older prompt is
// rejected as a session mismatch rather than mapped onto different items.
func (manager *Manager) currentExport(forceRebuild bool) (*prompt.Export, bool, error) {
	redactor, err := manager.currentRedactor()
	if err != nil {
		return nil, false, err
	}
	state := &manager.exportState
	stale := state.export != nil && state.redactionVersion != manager.redaction.version
	if state.export != nil && !forceRebuild && (!stale || len(state.imports) > 0) {
		return state.export, stale, nil
	}
	code := manager.completed.Code
	if state.export != nil {
		if code, err = newSessionCode(); err != nil {
			return nil, false, err
		}
	}
	built := prompt.Build(manager.completed.Result.Tree, redactor, code,
		manager.promptOptions())
	state.export = &built
	state.redactor = redactor
	state.redactionVersion = manager.redaction.version
	state.imports = map[int]BatchImport{}
	state.explanations = newExplanationState()
	// Keep the placeholder numbers this prompt used for later sessions (D-034). A failed
	// save only affects numbering next time, never what this prompt contains.
	_ = manager.saveRules()
	return state.export, false, nil
}

// approximateTokens is a rough count for the UI (about four characters per token).
func approximateTokens(text string) int {
	return (len(text) + 3) / 4
}

func (manager *Manager) exportView(export *prompt.Export, stale bool) ExportView {
	view := ExportView{Code: export.Code, Items: len(export.Items),
		RepresentedBytes: export.RepresentedBytes, UnknownBytes: export.UnknownBytes,
		MinimumItemBytes: manager.promptOptions().MinimumItemBytes, Stale: stale}
	for _, batch := range export.Batches {
		batchView := BatchView{Batch: batch, Characters: len(batch.Text),
			TokenEstimate: approximateTokens(batch.Text)}
		if imported, found := manager.exportState.imports[batch.Number]; found {
			batchView.Imported = true
			batchView.CategoryCount = len(imported.Categories)
			for _, category := range imported.Categories {
				batchView.ClassifiedItems += len(category.IDs)
			}
		}
		view.Batches = append(view.Batches, batchView)
	}
	view.Explanation = manager.explanationView()
	return view
}

// Export returns the prompts for the finished scan. It fails closed while the redaction
// rules cannot be read (safety rule 7).
func (manager *Manager) Export(rebuild bool) (ExportView, error) {
	manager.lock.Lock()
	defer manager.lock.Unlock()
	export, stale, err := manager.currentExport(rebuild)
	if err != nil {
		return ExportView{}, err
	}
	return manager.exportView(export, stale), nil
}

// PasteRequest carries a reply for one batch.
type PasteRequest struct {
	Batch int    `json:"batch"`
	Text  string `json:"text"`
}

// PasteResult is what the Paste screen shows. Round B replies fill Explanations instead
// of Result.
type PasteResult struct {
	Result       response.Result             `json:"result"`
	Explanations *response.ExplanationResult `json:"explanations,omitempty"`
	Valid        bool                        `json:"valid"`
	Repair       string                      `json:"repair,omitempty"`
	Imported     bool                        `json:"imported"`
	Export       ExportView                  `json:"export"`
}

// ErrNoExport means a reply was pasted before any prompt existed.
var ErrNoExport = errors.New("export a prompt before pasting a reply")

// Paste validates a reply and imports it only when it has no problems at all.
func (manager *Manager) Paste(request PasteRequest) (PasteResult, error) {
	manager.lock.Lock()
	defer manager.lock.Unlock()
	if manager.redaction.loadError != nil {
		return PasteResult{}, ErrRulesUnavailable
	}
	export := manager.exportState.export
	if export == nil {
		return PasteResult{}, ErrNoExport
	}
	if round := manager.explanationRound(); round != 0 && request.Batch == round {
		return manager.pasteExplanations(request.Text), nil
	}
	if request.Batch < 1 || request.Batch > len(export.Batches) {
		return PasteResult{}, errors.New("no such batch")
	}
	batch := export.Batches[request.Batch-1]
	expectation := response.Expectation{Code: export.Code, Batch: batch.Number,
		BatchCount: len(export.Batches), FirstID: batch.FirstID, LastID: batch.LastID}
	parsed := response.Parse(request.Text, expectation)
	result := PasteResult{Result: parsed, Valid: parsed.Valid()}
	if result.Valid {
		manager.importBatch(batch.Number, parsed.Categories)
		result.Imported = true
	} else {
		result.Repair = response.RepairPrompt(parsed, expectation)
	}
	stale := manager.exportState.redactionVersion != manager.redaction.version
	result.Export = manager.exportView(export, stale)
	return result, nil
}

// importBatch stores a batch's categories. Importing a batch again replaces its
// categories, and whatever was explained or queued for the old ones is dropped with them.
func (manager *Manager) importBatch(batch int, categories []response.Category) {
	state := &manager.exportState
	if previous, found := state.imports[batch]; found {
		state.explanations.forget(previous.Numbers)
	}
	numbers := make([]int, len(categories))
	for index := range categories {
		numbers[index] = state.explanations.allocateNumber()
	}
	state.imports[batch] = BatchImport{Categories: categories, Numbers: numbers,
		ImportedAt: time.Now()}
	manager.recordImports()
}

// Imports returns the validated categories per batch.
func (manager *Manager) Imports() (*prompt.Export, map[int]BatchImport) {
	manager.lock.Lock()
	defer manager.lock.Unlock()
	copied := map[int]BatchImport{}
	for number, imported := range manager.exportState.imports {
		copied[number] = imported
	}
	return manager.exportState.export, copied
}
