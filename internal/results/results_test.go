package results

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/toldry/ullage/internal/platform"
	"github.com/toldry/ullage/internal/prompt"
	"github.com/toldry/ullage/internal/recipes"
	"github.com/toldry/ullage/internal/redact"
	"github.com/toldry/ullage/internal/response"
	"github.com/toldry/ullage/internal/scanner"
	"github.com/toldry/ullage/internal/scanner/scannertest"
)

const megabyte = 1 << 20

// claimsByName gives a folder with a listed name, and its subtree, that claim. Nested
// listed folders start their own claim, as nested recipe paths do in the real classifier.
type claimsByName map[string]int32

func (claims claimsByName) Root(string) (scanner.Classification, scanner.ClassifierState) {
	return scanner.Classification{Tier: scanner.TierUnknown, ClaimID: scanner.NoClaim}, true
}

func (claims claimsByName) Child(parent scanner.Classification, _ scanner.ClassifierState,
	name string, _ platform.EntryKind) (scanner.Classification, scanner.ClassifierState) {
	if claimID, found := claims[name]; found {
		return scanner.Classification{Tier: scanner.TierKnown, ClaimID: claimID}, true
	}
	return parent, true
}

func (claims claimsByName) ClaimLabel(int32) string { return "claim" }

func recipe(id string, bruteForce bool) *recipes.Recipe {
	return &recipes.Recipe{ID: id, Version: 2, Name: id + " name", What: "what " + id,
		Lost: "lost " + id, Regenerable: recipes.RegenerableYes,
		ManagedSteps: []recipes.ManagedStep{{Text: "step", Command: id + " clean"}},
		BruteForce:   recipes.BruteForcePolicy{Allowed: bruteForce, Reason: "use the tool"}}
}

type fixture struct {
	input  Input
	result *scanner.Result
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	fileSystem := scannertest.New()
	add := func(path string, megabytes int64) {
		fileSystem.AddFile("/c/Users/mara/"+path, megabytes*megabyte, 1)
	}
	add("AppData/Local/npm-cache/blob.bin", 300)
	add("AppData/Local/npm-cache/packages/nested.bin", 60)
	add("a/packages/one.bin", 150)
	add("b/packages/two.bin", 100)
	add("docker/disk.vhdx", 2000)
	add("Programs/App/app.bin", 5000)
	add("tinycache/t.bin", 10)
	add("projects/kestrel/build/out.bin", 900)
	add("Videos/clip.mp4", 700)
	add("AppData/Local/Strata/x.bin", 400)

	classifier := claimsByName{"npm-cache": 0, "packages": 1, "docker": 2, "App": 3,
		"tinycache": 4}
	byClaim := []*recipes.Recipe{recipe("npm-cache", true), recipe("nuget", true),
		recipe("docker", false), recipe("apps", false), recipe("tiny", true)}
	byClaim[3].Dynamic = recipes.DynamicInstalledApplications
	byClaim[1].BruteForce.Warning = "The tool loses track of deleted packages."
	result := fileSystem.Scan(t, "/c", classifier)

	redactor := redact.New(redact.Options{Tree: result.Tree,
		Rules: []redact.Rule{{ID: "r1", Type: redact.RuleName, Pattern: "kestrel",
			Treatment: redact.TreatmentSubtree}},
		Registry: redact.NewRegistry(nil, 1, true),
		Context: redact.Context{CurrentProfile: filepath.FromSlash("/c/Users/mara"),
			CaseInsensitive: true}})
	export := prompt.Build(result.Tree, redactor, "ABCDEF", prompt.DefaultOptions)
	return fixture{result: result, input: Input{Tree: result.Tree,
		ClaimRoots: result.ClaimRoots,
		RecipeForClaim: func(claimID int32) *recipes.Recipe {
			return byClaim[claimID]
		},
		Redactor: redactor, Export: &export, Expanded: map[string]bool{},
		MinimumBytes: prompt.DefaultOptions.MinimumItemBytes,
		Profile:      filepath.FromSlash("/c/Users/mara")}}
}

// idOf returns the export ID of the item whose real path contains fragment.
func (fixture fixture) idOf(t *testing.T, fragment string) int {
	t.Helper()
	for _, item := range fixture.input.Export.Items {
		path := filepath.ToSlash(fixture.result.Tree.Path(item.Node))
		if strings.Contains(path, fragment) {
			return item.ID
		}
	}
	t.Fatalf("no item for %s", fragment)
	return 0
}

// kestrelPlaceholder is the placeholder the export used for the redacted folder.
func (fixture fixture) kestrelPlaceholder(t *testing.T) string {
	t.Helper()
	for _, item := range fixture.input.Export.Items {
		if strings.Contains(fixture.result.Tree.Path(item.Node), "kestrel") {
			return fixture.input.Redactor.RenderName(item.Node)
		}
	}
	t.Fatal("no kestrel item")
	return ""
}

func (fixture *fixture) importRows(t *testing.T) {
	t.Helper()
	rows := []response.Category{
		{Name: "Build output of " + fixture.kestrelPlaceholder(t), Confidence: response.ConfidenceHigh,
			Regenerable: response.RegenerableYes, Deletable: true,
			IDs: []int{fixture.idOf(t, "kestrel")}},
		{Name: "Home videos", Confidence: response.ConfidenceHigh,
			Regenerable: response.RegenerableNo, IDs: []int{fixture.idOf(t, "Videos")}},
		{Name: "Unidentified app data", Confidence: response.ConfidenceLow,
			Regenerable: response.RegenerableNo, Deletable: true,
			IDs: []int{fixture.idOf(t, "Strata")}},
	}
	for index, row := range rows {
		fixture.input.Imported = append(fixture.input.Imported, ImportedCategory{
			Number: index + 1, Batch: 1, Category: row})
	}
}

func byKey(model Model, key string) *Category {
	for _, list := range [][]Category{model.Categories, model.Kept} {
		for index := range list {
			if list[index].Key == key {
				return &list[index]
			}
		}
	}
	return nil
}

func TestMergeAndSortByReclaimableSize(t *testing.T) {
	fixture := newFixture(t)
	fixture.importRows(t)
	model := Build(fixture.input)

	var keys []string
	for _, category := range model.Categories {
		keys = append(keys, category.Key)
	}
	// docker 2000, kestrel 900, Strata 400, packages 310, npm-cache 300.
	want := "recipe:docker,ai:1,ai:3,recipe:nuget,recipe:npm-cache"
	if strings.Join(keys, ",") != want {
		t.Fatalf("order = %v, want %s", keys, want)
	}
	for index := 1; index < len(model.Categories); index++ {
		if model.Categories[index].Bytes > model.Categories[index-1].Bytes {
			t.Fatalf("not sorted by size: %v", keys)
		}
	}

	// Nested claims are not counted twice: the nested packages folder belongs to nuget.
	if npm := byKey(model, "recipe:npm-cache"); npm.Bytes != 300*megabyte {
		t.Errorf("npm-cache bytes = %d", npm.Bytes)
	}
	nuget := byKey(model, "recipe:nuget")
	if nuget.Bytes != 310*megabyte || nuget.ItemCount != 3 ||
		nuget.Items[0].Bytes != 150*megabyte || nuget.RecipeVersion != 2 ||
		!nuget.Selectable || nuget.BruteForce.Warning == "" {
		t.Errorf("nuget = %+v", nuget)
	}
	// Installed applications and categories under the threshold are not listed.
	if byKey(model, "recipe:apps") != nil || byKey(model, "recipe:tiny") != nil {
		t.Errorf("installed apps or a tiny recipe listed: %v", keys)
	}
	docker := byKey(model, "recipe:docker")
	if docker.Selectable || docker.BruteForce.Allowed || docker.BruteForce.Reason == "" ||
		docker.Explanation.Steps[0].Command != "docker clean" {
		t.Errorf("docker = %+v", docker)
	}

	kept := byKey(model, "ai:2")
	if len(model.Kept) != 1 || kept == nil || kept.Selectable || kept.BruteForce.Allowed {
		t.Errorf("del 0 category should be kept and not selectable: %+v", model.Kept)
	}

	summary := model.Summary
	if summary.KeptByAIBytes != 700*megabyte ||
		summary.KnownKeptBytes != 5010*megabyte ||
		summary.ReclaimableBytes != (2000+900+400+310+300)*megabyte ||
		summary.UnclassifiedBytes != 0 {
		t.Errorf("summary = %+v", summary)
	}
}

func TestRealNamesRestoredForDisplayOnly(t *testing.T) {
	fixture := newFixture(t)
	fixture.importRows(t)
	if text := fixture.input.Export.Batches[0].Text; strings.Contains(text, "kestrel") {
		t.Fatalf("fixture prompt leaks the redacted name:\n%s", text)
	}
	placeholder := fixture.kestrelPlaceholder(t)
	if !strings.HasPrefix(placeholder, "<R") {
		t.Fatalf("kestrel not redacted: %q", placeholder)
	}
	row := &fixture.input.Imported[0].Category
	row.What, row.Lost = "CMake output for "+placeholder+".", "Nothing in "+placeholder+"/src."
	row.Steps = []string{"Rebuild " + placeholder + " later."}
	model := Build(fixture.input)

	category := byKey(model, "ai:1")
	if category.Name != "Build output of kestrel" ||
		category.RestoredFrom != "Build output of "+placeholder {
		t.Errorf("name = %q from %q", category.Name, category.RestoredFrom)
	}
	item := category.Items[0]
	if !strings.Contains(item.Path, "kestrel") || !strings.HasPrefix(item.Path, "~/") ||
		!strings.Contains(item.ExportedAs, placeholder) {
		t.Errorf("item = %+v", item)
	}
	explanation := category.Explanation
	if category.ExplanationState != ExplanationReceived ||
		explanation.What != "CMake output for kestrel." ||
		explanation.Steps[0].Text != "Rebuild kestrel later." ||
		explanation.Steps[0].Command != "" {
		t.Errorf("explanation = %+v", explanation)
	}
	// A placeholder this export never used stays as it is.
	restored := newRestorer(fixture.input).restore("<R99> and " + placeholder + ".txt")
	if restored != "<R99> and kestrel.txt" {
		t.Errorf("restore = %q", restored)
	}
}

// explain gives imported rows the explanation lines a reply carries below them.
func explain(fixture *fixture, numbers ...int) {
	for _, number := range numbers {
		row := &fixture.input.Imported[number-1].Category
		row.What, row.Lost, row.Steps = "w", "l", []string{"s"}
	}
}

func TestLowConfidenceNeedsExpandBeforeDelete(t *testing.T) {
	fixture := newFixture(t)
	fixture.importRows(t)
	explain(&fixture, 1, 3)
	locked := byKey(Build(fixture.input), "ai:3")
	if !locked.NeedsExpand || locked.Selectable {
		t.Fatalf("low confidence category is selectable before expanding: %+v", locked)
	}
	fixture.input.Expanded["ai:3"] = true
	unlocked := byKey(Build(fixture.input), "ai:3")
	if unlocked.NeedsExpand || !unlocked.Selectable {
		t.Fatalf("expanding did not unlock: %+v", unlocked)
	}
	if high := byKey(Build(fixture.input), "ai:1"); high.NeedsExpand || !high.Selectable {
		t.Fatalf("high confidence category locked: %+v", high)
	}
}

// Decision D-050: nothing the AI judged is selectable without an explanation. The parser
// already rejects such rows; this is the second line of defense.
func TestAICategoriesNeedAnExplanationBeforeDelete(t *testing.T) {
	fixture := newFixture(t)
	fixture.importRows(t)
	fixture.input.Expanded["ai:3"] = true
	model := Build(fixture.input)
	for _, key := range []string{"ai:1", "ai:3"} {
		if category := byKey(model, key); !category.NeedsExplanation || category.Selectable {
			t.Fatalf("%s selectable without an explanation: %+v", key, category)
		}
	}
	if nuget := byKey(model, "recipe:nuget"); nuget.NeedsExplanation || !nuget.Selectable {
		t.Fatalf("recipe category locked: %+v", nuget)
	}
	explain(&fixture, 1)
	if category := byKey(Build(fixture.input), "ai:1"); category.NeedsExplanation ||
		!category.Selectable {
		t.Fatalf("explained category still locked: %+v", category)
	}
}

func TestExplanationStates(t *testing.T) {
	fixture := newFixture(t)
	fixture.importRows(t)
	explain(&fixture, 3)
	model := Build(fixture.input)
	if byKey(model, "ai:3").ExplanationState != ExplanationReceived ||
		byKey(model, "ai:1").ExplanationState != ExplanationNone ||
		byKey(model, "recipe:nuget").ExplanationState != ExplanationRecipe {
		t.Fatalf("states wrong: %+v", model.Categories)
	}
}

func TestIDsMissingFromTheExportAreIgnored(t *testing.T) {
	fixture := newFixture(t)
	fixture.input.Imported = []ImportedCategory{{Number: 1, Category: response.Category{
		Name: "Ghost", Confidence: response.ConfidenceHigh, Deletable: true,
		Regenerable: response.RegenerableYes, IDs: []int{0, 9999}}}}
	ghost := byKey(Build(fixture.input), "ai:1")
	if ghost == nil || ghost.ItemCount != 0 || ghost.Bytes != 0 || ghost.Selectable {
		t.Fatalf("category with unknown IDs = %+v", ghost)
	}
}

// Cloud-synced items stay deletable, but the category says how many there are.
func TestCloudSyncedItemsAreFlagged(t *testing.T) {
	fixture := newFixture(t)
	fixture.importRows(t)
	strata := scannertest.Find(t, fixture.result, "/c/Users/mara/AppData/Local/Strata")
	fixture.result.Tree.Node(strata).Attributes |= platform.AttributeManagedReparsePoint
	nested := scannertest.Find(t, fixture.result, "/c/Users/mara/AppData/Local/npm-cache")
	fixture.result.Tree.Node(nested).NotLocalBytes = 5 * megabyte
	model := Build(fixture.input)
	if category := byKey(model, "ai:3"); category.CloudSyncItems != 1 ||
		!category.Items[0].CloudSync {
		t.Fatalf("cloud-synced AI item not flagged: %+v", category)
	}
	if npm := byKey(model, "recipe:npm-cache"); npm.CloudSyncItems != 1 ||
		npm.NotLocalBytes != 5*megabyte {
		t.Fatalf("cloud-only recipe item not flagged: %+v", npm)
	}
	if kestrel := byKey(model, "ai:1"); kestrel.CloudSyncItems != 0 {
		t.Fatalf("unrelated category flagged: %+v", kestrel)
	}
}
