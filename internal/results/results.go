// Package results merges what the built-in recipes found with the categories the
// assistant returned into the one list the Results screen shows. Everything here is local:
// real names are restored for display, but nothing in this package is ever exported.
package results

import (
	"sort"
	"strings"
	"time"

	"github.com/toldry/ullage/internal/overview"
	"github.com/toldry/ullage/internal/platform"
	"github.com/toldry/ullage/internal/prompt"
	"github.com/toldry/ullage/internal/recipes"
	"github.com/toldry/ullage/internal/redact"
	"github.com/toldry/ullage/internal/response"
	"github.com/toldry/ullage/internal/scanner"
)

// Source says who identified a category.
type Source string

const (
	SourceRecipe Source = "recipe"
	SourceAI     Source = "ai"
)

// ExplanationState says whether a category can be explained yet.
type ExplanationState string

const (
	// ExplanationRecipe: the recipe carries the explanation.
	ExplanationRecipe ExplanationState = "recipe"
	// ExplanationNone: an AI category nobody asked to explain yet.
	ExplanationNone     ExplanationState = "none"
	ExplanationQueued   ExplanationState = "queued"
	ExplanationReceived ExplanationState = "received"
)

// ConfidenceRecipe marks recipe categories, which are matched by path rather than judged.
const ConfidenceRecipe = "recipe"

// maximumListedItems bounds the item list sent per category; the rest is summarized.
const maximumListedItems = 200

// Step is one managed-cleanup step. Only recipe steps carry a command: a command written
// by the assistant would be an action it authored, so AI steps are text only.
type Step struct {
	Text    string `json:"text"`
	Command string `json:"command,omitempty"`
}

// Explanation is what the user reads before deciding.
type Explanation struct {
	What       string    `json:"what"`
	Lost       string    `json:"lost"`
	Steps      []Step    `json:"steps"`
	Note       string    `json:"note,omitempty"`
	Round      int       `json:"round,omitempty"`
	ReceivedAt time.Time `json:"receivedAt,omitempty"`
}

// BruteForce says whether Ullage itself may move the items to the Recycle Bin.
type BruteForce struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason,omitempty"`
}

// Item is one thing a category covers, with its real local path.
type Item struct {
	Key  string `json:"key"`
	Kind string `json:"kind"`
	Path string `json:"path"`
	// ExportedAs is the path as the prompt showed it, when redaction changed it.
	ExportedAs string `json:"exportedAs,omitempty"`
	Bytes      int64  `json:"bytes"`
	Files      int64  `json:"files"`
	Modified   int64  `json:"modified"`

	// What deletion resolves the item to; never sent to the browser.
	Node    scanner.NodeIndex   `json:"-"`
	Listed  []scanner.NodeIndex `json:"-"`
	Tier    scanner.Tier        `json:"-"`
	ClaimID int32               `json:"-"`
}

// Category is one row of the Results screen.
type Category struct {
	Key    string `json:"key"`
	Number int    `json:"number,omitempty"`
	Name   string `json:"name"`
	// RestoredFrom is the assistant's name when real names were put back into it.
	RestoredFrom  string    `json:"restoredFrom,omitempty"`
	Source        Source    `json:"source"`
	RecipeID      string    `json:"recipeId,omitempty"`
	RecipeVersion int       `json:"recipeVersion,omitempty"`
	Batch         int       `json:"batch,omitempty"`
	ImportedAt    time.Time `json:"importedAt,omitempty"`
	Confidence    string    `json:"confidence"`
	Regenerable   string    `json:"regenerable"`
	Bytes         int64     `json:"bytes"`
	Files         int64     `json:"files"`
	ItemCount     int       `json:"itemCount"`
	Root          string    `json:"root"`
	Items         []Item    `json:"items"`
	// Remaining are the items beyond the listed ones, for resolving a "select all".
	Remaining        []Item           `json:"-"`
	MoreItems        int              `json:"moreItems"`
	MoreBytes        int64            `json:"moreBytes"`
	Explanation      *Explanation     `json:"explanation,omitempty"`
	ExplanationState ExplanationState `json:"explanationState"`
	BruteForce       BruteForce       `json:"bruteForce"`
	// NeedsExpand: a low-confidence category whose row the user has not opened yet.
	NeedsExpand bool `json:"needsExpand"`
	// NeedsExplanation: an AI category whose round B explanation has not arrived. The user
	// reads what is lost before anything the AI judged can be selected (decision D-050).
	NeedsExplanation bool `json:"needsExplanation"`
	Selectable       bool `json:"selectable"`
}

// Summary accounts for every scanned byte, so an empty result still explains itself.
type Summary struct {
	SystemBytes       int64 `json:"systemBytes"`
	KnownKeptBytes    int64 `json:"knownKeptBytes"`
	KeptByAIBytes     int64 `json:"keptByAiBytes"`
	UnclassifiedBytes int64 `json:"unclassifiedBytes"`
	ReclaimableBytes  int64 `json:"reclaimableBytes"`
}

// Model is the Results screen's data.
type Model struct {
	Categories []Category `json:"categories"`
	// Kept are AI categories marked del 0: shown on request, never selectable.
	Kept    []Category `json:"kept"`
	Summary Summary    `json:"summary"`
}

// ImportedCategory is one validated round A row with the number it was given on import.
type ImportedCategory struct {
	Number     int
	Batch      int
	ImportedAt time.Time
	Category   response.Category
}

// ReceivedExplanation is a validated round B entry.
type ReceivedExplanation struct {
	Explanation response.Explanation
	Round       int
	ReceivedAt  time.Time
}

// Input is everything Build needs. Nothing in it is modified.
type Input struct {
	Tree           *scanner.Tree
	ClaimRoots     map[int32][]scanner.NodeIndex
	RecipeForClaim func(claimID int32) *recipes.Recipe
	Redactor       *redact.Redactor
	Export         *prompt.Export
	Imported       []ImportedCategory
	Explanations   map[int]ReceivedExplanation
	Queued         map[int]bool
	Expanded       map[string]bool
	// Deleted holds DeletedKey values of items deleted in this session; they are not
	// listed again. Keys name scan nodes, so they stay valid when an export is rebuilt.
	Deleted      map[string]bool
	MinimumBytes int64
	Profile      string
}

// Build produces the merged, sorted list.
func Build(input Input) Model {
	model := Model{Categories: []Category{}, Kept: []Category{}}
	restorer := newRestorer(input)
	recipeCategories, knownListed := buildRecipeCategories(input)
	model.Categories = append(model.Categories, recipeCategories...)
	var aiClassified int64
	for _, imported := range input.Imported {
		category := buildAICategory(input, imported, restorer)
		if category.ItemCount == 0 && wasDeleted(input, imported) {
			continue
		}
		aiClassified += category.Bytes
		if imported.Category.Deletable {
			model.Categories = append(model.Categories, category)
		} else {
			category.Selectable = false
			model.Kept = append(model.Kept, category)
			model.Summary.KeptByAIBytes += category.Bytes
		}
	}
	sortCategories(model.Categories)
	sortCategories(model.Kept)
	for _, category := range model.Categories {
		model.Summary.ReclaimableBytes += category.Bytes
	}
	for _, root := range input.Tree.Roots {
		node := input.Tree.Node(root)
		model.Summary.SystemBytes += node.TierBytes(scanner.TierSystem)
		model.Summary.KnownKeptBytes += node.TierBytes(scanner.TierKnown)
		model.Summary.UnclassifiedBytes += node.TierBytes(scanner.TierUnknown)
	}
	model.Summary.KnownKeptBytes -= knownListed
	model.Summary.UnclassifiedBytes -= aiClassified
	return model
}

func sortCategories(categories []Category) {
	sort.SliceStable(categories, func(first, second int) bool {
		if categories[first].Bytes != categories[second].Bytes {
			return categories[first].Bytes > categories[second].Bytes
		}
		return categories[first].Key < categories[second].Key
	})
}

// buildRecipeCategories groups claims by recipe. Installed applications are left out:
// their recipe says keep, and uninstalling is not a disk-space cleanup Ullage suggests.
func buildRecipeCategories(input Input) ([]Category, int64) {
	byRecipe := map[string]*Category{}
	var order []string
	claimIDs := make([]int32, 0, len(input.ClaimRoots))
	for claimID := range input.ClaimRoots {
		claimIDs = append(claimIDs, claimID)
	}
	sort.Slice(claimIDs, func(first, second int) bool { return claimIDs[first] < claimIDs[second] })
	for _, claimID := range claimIDs {
		recipe := input.RecipeForClaim(claimID)
		if recipe == nil || recipe.Dynamic == recipes.DynamicInstalledApplications {
			continue
		}
		category := byRecipe[recipe.ID]
		if category == nil {
			category = newRecipeCategory(recipe)
			byRecipe[recipe.ID] = category
			order = append(order, recipe.ID)
		}
		for _, root := range input.ClaimRoots[claimID] {
			bytes, files := claimedBytes(input.Tree, root, claimID)
			if bytes == 0 {
				continue
			}
			node := input.Tree.Node(root)
			if input.Deleted[DeletedKey(root, false)] {
				continue
			}
			category.Items = append(category.Items, Item{Key: nodeKey(root),
				Kind: itemKind(node.Kind, false), Bytes: bytes, Files: files,
				Path: displayPath(input, root, false), Modified: modifiedOf(node),
				Node: root, Tier: scanner.TierKnown, ClaimID: claimID})
		}
	}
	var categories []Category
	var listedBytes int64
	for _, recipeID := range order {
		category := byRecipe[recipeID]
		finishItems(category)
		if category.Bytes < input.MinimumBytes || category.Bytes == 0 {
			continue
		}
		listedBytes += category.Bytes
		categories = append(categories, *category)
	}
	return categories, listedBytes
}

func newRecipeCategory(recipe *recipes.Recipe) *Category {
	steps := make([]Step, len(recipe.ManagedSteps))
	for index, step := range recipe.ManagedSteps {
		steps[index] = Step{Text: step.Text, Command: step.Command}
	}
	return &Category{Key: "recipe:" + recipe.ID, Name: recipe.Name, Source: SourceRecipe,
		RecipeID: recipe.ID, RecipeVersion: recipe.Version, Confidence: ConfidenceRecipe,
		Regenerable: string(recipe.Regenerable),
		Explanation: &Explanation{What: recipe.What, Lost: recipe.Lost, Steps: steps,
			Note: recipe.ManagedNote},
		ExplanationState: ExplanationRecipe,
		BruteForce: BruteForce{Allowed: recipe.BruteForce.Allowed,
			Reason: recipe.BruteForce.Reason},
		Selectable: recipe.BruteForce.Allowed}
}

// claimedBytes counts the Known bytes of one claim under a root, leaving out anything a
// nested claim or another tier owns, so no byte is listed twice.
func claimedBytes(tree *scanner.Tree, root scanner.NodeIndex, claimID int32) (int64, int64) {
	var bytes, files int64
	stack := []scanner.NodeIndex{root}
	for len(stack) > 0 {
		index := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		node := tree.Node(index)
		if node.ClaimID != claimID || node.Tier != scanner.TierKnown {
			continue
		}
		// Same rule as the scanner's roll-up, so the totals agree with the Overview.
		bytes += node.OwnBytes
		if node.Kind == platform.EntryFile {
			files++
		} else {
			files += int64(node.SmallFileCount)
		}
		stack = append(stack, node.Children...)
	}
	return bytes, files
}

func buildAICategory(input Input, imported ImportedCategory, restorer *restorer) Category {
	row := imported.Category
	name := restorer.restore(row.Name)
	category := Category{Key: aiKey(imported.Number), Number: imported.Number, Name: name,
		Source: SourceAI, Batch: imported.Batch, ImportedAt: imported.ImportedAt,
		Confidence: string(row.Confidence), Regenerable: string(row.Regenerable),
		ExplanationState: ExplanationNone,
		BruteForce:       BruteForce{Allowed: row.Deletable}}
	if name != row.Name {
		category.RestoredFrom = row.Name
	}
	if !row.Deletable {
		category.BruteForce.Reason = "The AI marked these as files to keep."
	}
	for _, id := range row.IDs {
		item := input.Export.ItemByID(id)
		if item == nil || input.Deleted[DeletedKey(item.Node, item.Kind ==
			prompt.ItemLooseContents)] {
			continue
		}
		loose := item.Kind == prompt.ItemLooseContents
		node := input.Tree.Node(item.Node)
		entry := Item{Key: "id:" + itoa(id), Kind: itemKind(node.Kind, loose),
			Bytes: item.Bytes, Files: item.Files, Path: displayPath(input, item.Node, loose),
			Modified: modifiedOf(node), Node: item.Node, Listed: item.Listed,
			Tier: scanner.TierUnknown, ClaimID: scanner.NoClaim}
		if exported := renderedPath(input, item.Node, loose); strings.Contains(exported, "<R") {
			entry.ExportedAs = exported
		}
		category.Items = append(category.Items, entry)
	}
	finishItems(&category)
	if received, found := input.Explanations[imported.Number]; found {
		explanation := received.Explanation
		steps := make([]Step, len(explanation.Steps))
		for index, step := range explanation.Steps {
			steps[index] = Step{Text: restorer.restore(step)}
		}
		category.Explanation = &Explanation{What: restorer.restore(explanation.What),
			Lost: restorer.restore(explanation.Lost), Steps: steps, Round: received.Round,
			ReceivedAt: received.ReceivedAt}
		category.ExplanationState = ExplanationReceived
	} else if input.Queued[imported.Number] {
		category.ExplanationState = ExplanationQueued
	}
	category.NeedsExpand = row.Confidence == response.ConfidenceLow &&
		!input.Expanded[category.Key]
	category.NeedsExplanation = category.ExplanationState != ExplanationReceived
	category.Selectable = row.Deletable && !category.NeedsExpand &&
		!category.NeedsExplanation && category.Bytes > 0
	return category
}

func aiKey(number int) string { return "ai:" + itoa(number) }

// AIKey is the key of an AI category, for callers that track expansion.
func AIKey(number int) string { return aiKey(number) }

// finishItems totals the items, sorts them by size and trims the list.
func finishItems(category *Category) {
	sort.SliceStable(category.Items, func(first, second int) bool {
		return category.Items[first].Bytes > category.Items[second].Bytes
	})
	category.Bytes, category.Files = 0, 0
	for _, item := range category.Items {
		category.Bytes += item.Bytes
		category.Files += item.Files
	}
	category.ItemCount = len(category.Items)
	category.Root = commonDirectory(category.Items)
	if len(category.Items) > maximumListedItems {
		for _, item := range category.Items[maximumListedItems:] {
			category.MoreBytes += item.Bytes
		}
		category.MoreItems = len(category.Items) - maximumListedItems
		category.Remaining = category.Items[maximumListedItems:]
		category.Items = category.Items[:maximumListedItems]
	}
	if category.Items == nil {
		category.Items = []Item{}
	}
}

// commonDirectory is the deepest folder containing every item, ending in "/".
func commonDirectory(items []Item) string {
	if len(items) == 0 {
		return ""
	}
	common := parentSegments(items[0].Path)
	for _, item := range items[1:] {
		segments := parentSegments(item.Path)
		length := 0
		for length < len(common) && length < len(segments) &&
			strings.EqualFold(common[length], segments[length]) {
			length++
		}
		common = common[:length]
	}
	if len(common) == 0 {
		return ""
	}
	return strings.Join(common, "/") + "/"
}

func parentSegments(path string) []string {
	segments := strings.Split(strings.TrimSuffix(strings.TrimSuffix(path, "/*"), "/"), "/")
	return segments[:len(segments)-1]
}

func itemKind(kind platform.EntryKind, loose bool) string {
	switch {
	case loose:
		return "loose"
	case kind == platform.EntryFile:
		return "file"
	default:
		return "folder"
	}
}

func modifiedOf(node *scanner.Node) int64 {
	nanos := node.LatestModified
	if node.Kind == platform.EntryFile || nanos == 0 {
		nanos = node.ModifiedUnixNanos
	}
	return nanos / int64(time.Second)
}

func displayPath(input Input, index scanner.NodeIndex, loose bool) string {
	path := overview.DisplayPath(input.Tree.Path(index), input.Profile)
	if loose {
		path = strings.TrimSuffix(path, "/") + "/*"
	}
	return path
}

func renderedPath(input Input, index scanner.NodeIndex, loose bool) string {
	path := input.Redactor.Render(index)
	if loose {
		path += "/*"
	}
	return path
}

func nodeKey(index scanner.NodeIndex) string { return "node:" + itoa(int(index)) }

// wasDeleted reports whether any of the category's items was deleted in this session, so
// a category emptied by deleting is no longer listed.
func wasDeleted(input Input, imported ImportedCategory) bool {
	for _, id := range imported.Category.IDs {
		if item := input.Export.ItemByID(id); item != nil && input.Deleted[DeletedKey(
			item.Node, item.Kind == prompt.ItemLooseContents)] {
			return true
		}
	}
	return false
}

// DeletedKey names a deleted item by its scan node: the node itself, or a folder's loose
// contents.
func DeletedKey(node scanner.NodeIndex, loose bool) string {
	if loose {
		return "loose:" + itoa(int(node))
	}
	return "node:" + itoa(int(node))
}
