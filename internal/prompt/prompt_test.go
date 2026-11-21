package prompt

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/toldry/ullage/internal/platform"
	"github.com/toldry/ullage/internal/redact"
	"github.com/toldry/ullage/internal/response"
	"github.com/toldry/ullage/internal/scanner"
	"github.com/toldry/ullage/internal/scanner/scannertest"
)

const megabyte = 1 << 20

var month = func(year int, monthNumber time.Month) int64 {
	return time.Date(year, monthNumber, 15, 0, 0, 0, 0, time.UTC).UnixNano()
}

type fixture struct {
	result   *scanner.Result
	redactor *redact.Redactor
}

func sampleFileSystem() *scannertest.FileSystem {
	fileSystem := scannertest.New()
	add := func(path string, megabytes int64, modified int64) {
		fileSystem.AddFile(path, megabytes*megabyte, modified)
	}
	add("/c/Users/mara/AppData/Local/Vendor/blob.vhdx", 41*1024, month(2026, 9))
	for index := 0; index < 900; index++ {
		add("/c/Users/mara/AppData/Local/Temp/t"+itoa(index)+".tmp", 7, month(2025, 3))
	}
	for _, project := range []string{"alpha", "beta", "gamma", "delta"} {
		for index := 0; index < 3; index++ {
			add("/c/Users/mara/projects/"+project+"/node_modules/pkg"+itoa(index)+"/big.bin",
				300, month(2024, 1))
		}
		add("/c/Users/mara/projects/"+project+"/src/main.bin", 2, month(2026, 8))
	}
	add("/c/Users/mara/projects/kestrel/build/out.bin", 9*1024, month(2026, 9))
	add("/c/Users/mara/projects/kestrel/src/core.bin", 150, month(2026, 9))
	for index := 0; index < 25; index++ {
		add("/c/Users/mara/Videos/archive/clip"+itoa(index)+".mp4", 900, month(2019, 6))
	}
	add("/c/Users/mara/Documents/notes.txt", 1, month(2026, 1))
	return fileSystem
}

func itoa(value int) string { return string(rune('a'+value/26)) + string(rune('a'+value%26)) }

func newFixture(t *testing.T, rules []redact.Rule) fixture {
	t.Helper()
	result := sampleFileSystem().Scan(t, "/c", nil)
	redactor := redact.New(redact.Options{Tree: result.Tree, Rules: rules,
		Registry: redact.NewRegistry(nil, 1, true),
		Context: redact.Context{CurrentProfile: filepath.FromSlash("/c/Users/mara"),
			SensitiveWords: []string{"mara"}, CaseInsensitive: true}})
	return fixture{result: result, redactor: redactor}
}

var dataLine = regexp.MustCompile(`^ \d+(-\d+)? \d+(\.\d)?[BKMGTP] \S`)

func dataLines(text string) []string {
	var lines []string
	inData := false
	for _, line := range strings.Split(text, "\n") {
		switch {
		case strings.HasPrefix(line, "@SESSION "):
			inData = true
		case strings.HasPrefix(line, "(end of data"):
			inData = false
		case inData && strings.HasPrefix(line, " "):
			lines = append(lines, line)
		}
	}
	return lines
}

func TestSampleExport(t *testing.T) {
	fixture := newFixture(t, []redact.Rule{{ID: "1", Type: redact.RuleExact,
		Pattern: "~/projects/kestrel", Treatment: redact.TreatmentSubtree,
		KeepWellKnown: true}})
	export := Build(fixture.result.Tree, fixture.redactor, "7KQX2M", DefaultOptions)
	text := export.Batches[0].Text
	for _, expected := range []string{
		"@SESSION 7KQX2M\n/c/Users/~/AppData/Local\n",
		" 1 6.2G Temp [900 files] m2025-03 {tmp}\n",
		" 2 41G Vendor/blob.vhdx m2026-09\n",
		"/c/Users/~/projects\n",
		" 5-8 3.5G node_modules x4 [12 files] m2024-01 {bin}\n",
		"/c/Users/~/Videos\n 9 22G archive [25 files] m2019-06 {mp4}\n",
		"(end of data: IDs 1-9, batch 1 of 1)",
		"@END 7KQX2M",
	} {
		if !strings.Contains(text, expected) {
			t.Errorf("prompt lacks %q:\n%s", expected, text)
		}
	}
	if !regexp.MustCompile(` 3 150M <R\d+>/\*\n`).MatchString(text) ||
		!regexp.MustCompile(` 4 9G <R\d+>/build m2026-09 \{bin\}\n`).
			MatchString(text) {
		t.Errorf("redacted folder lines wrong:\n%s", text)
	}
	for _, line := range dataLines(text) {
		if !dataLine.MatchString(line) {
			t.Errorf("malformed data line %q", line)
		}
	}
	if export.Items[4].Kind != ItemFolder || export.BatchOf(9) != 1 || export.BatchOf(10) != 0 {
		t.Errorf("item bookkeeping wrong")
	}
	again := Build(fixture.result.Tree, fixture.redactor, "7KQX2M", DefaultOptions)
	if again.Batches[0].Text != text {
		t.Errorf("export is not deterministic")
	}
}

func TestItemsNeverCoverKnownOrSystemBytes(t *testing.T) {
	fileSystem := sampleFileSystem()
	fileSystem.AddFile("/c/Users/mara/AppData/Local/npm-cache/blob", 500*megabyte, 0)
	fileSystem.AddFile("/c/Windows/System32/big.dll", 800*megabyte, 0)
	result := fileSystem.Scan(t, "/c", claimByName{"npm-cache": scanner.TierKnown,
		"Windows": scanner.TierSystem})
	redactor := redact.New(redact.Options{Tree: result.Tree,
		Registry: redact.NewRegistry(nil, 1, true)})
	export := Build(result.Tree, redactor, "7KQX2M", DefaultOptions)
	for _, item := range export.Items {
		node := result.Tree.Node(item.Node)
		switch item.Kind {
		case ItemFile, ItemFolder:
			if node.TierBytes(scanner.TierUnknown) != node.TotalBytes() {
				t.Errorf("item %d covers non-Unknown bytes", item.ID)
			}
		case ItemLooseContents:
			for _, child := range node.Children {
				childNode := result.Tree.Node(child)
				listed := false
				for _, excluded := range item.Listed {
					listed = listed || excluded == child
				}
				if !listed && childNode.TotalBytes() != childNode.TierBytes(scanner.TierUnknown) &&
					childNode.TierBytes(scanner.TierUnknown) >= DefaultOptions.MinimumItemBytes {
					t.Errorf("loose item %d hides a mixed child", item.ID)
				}
			}
		}
	}
	if export.RepresentedBytes > export.UnknownBytes {
		t.Errorf("represented %d > unknown %d", export.RepresentedBytes, export.UnknownBytes)
	}
}

// claimByName is a minimal classifier: a folder with a listed name and its subtree get
// that tier.
type claimByName map[string]scanner.Tier

func (claims claimByName) Root(string) (scanner.Classification, scanner.ClassifierState) {
	return scanner.Classification{Tier: scanner.TierUnknown, ClaimID: scanner.NoClaim}, true
}

func (claims claimByName) Child(parent scanner.Classification, _ scanner.ClassifierState,
	name string, _ platform.EntryKind) (scanner.Classification, scanner.ClassifierState) {
	if parent.Tier != scanner.TierUnknown {
		return parent, nil
	}
	if tier, found := claims[name]; found {
		return scanner.Classification{Tier: tier, ClaimID: 0}, nil
	}
	return parent, true
}

func (claims claimByName) ClaimLabel(int32) string { return "claim" }

func TestBudgetLimitsRefinement(t *testing.T) {
	fixture := newFixture(t, nil)
	options := DefaultOptions
	options.MaximumItems = 4
	export := Build(fixture.result.Tree, fixture.redactor, "7KQX2M", options)
	if len(export.Items) > 4 {
		t.Fatalf("%d items exceed the budget of 4", len(export.Items))
	}
	if export.RepresentedBytes < export.UnknownBytes*9/10 {
		t.Fatalf("coarse export lost too much: %d of %d", export.RepresentedBytes,
			export.UnknownBytes)
	}
}

func TestBatchesSplitWithoutBreakingGroups(t *testing.T) {
	fixture := newFixture(t, nil)
	options := DefaultOptions
	options.BatchLines = 2
	export := Build(fixture.result.Tree, fixture.redactor, "7KQX2M", options)
	if len(export.Batches) < 3 {
		t.Fatalf("batches = %d", len(export.Batches))
	}
	next := 1
	for _, batch := range export.Batches {
		if batch.FirstID != next || batch.LastID < batch.FirstID {
			t.Fatalf("batch %d IDs %d-%d, expected to start at %d", batch.Number,
				batch.FirstID, batch.LastID, next)
		}
		next = batch.LastID + 1
		if batch.Number > 1 && (strings.Contains(batch.Text, "How to judge") ||
			!strings.HasPrefix(batch.Text, "Batch "+strconv.Itoa(batch.Number)+" of")) {
			t.Errorf("continuation prompt %d repeats instructions or lacks its header",
				batch.Number)
		}
		if len(dataLines(batch.Text)) > 2 {
			t.Errorf("batch %d has more than 2 data lines", batch.Number)
		}
	}
	if next != len(export.Items)+1 {
		t.Fatalf("IDs not continuous: ended at %d of %d", next-1, len(export.Items))
	}
	if !strings.Contains(export.Batches[0].Text, "A complete example reply") {
		t.Errorf("first batch lacks the worked example")
	}
}

func TestRedactedItemsCarryOnlyTheirSize(t *testing.T) {
	fixture := newFixture(t, []redact.Rule{{ID: "1", Type: redact.RuleExact,
		Pattern: "~/Videos/archive", Treatment: redact.TreatmentSubtree}})
	export := Build(fixture.result.Tree, fixture.redactor, "7KQX2M", DefaultOptions)
	matched := false
	for _, line := range dataLines(export.Batches[0].Text) {
		if strings.Contains(line, "<R") {
			matched = true
			if !regexp.MustCompile(`^ \d+ 22G (Videos/)?<R\d+>$`).MatchString(line) {
				t.Errorf("redacted item carries metadata: %q", line)
			}
		}
	}
	if !matched {
		t.Fatalf("redacted archive missing from:\n%s", export.Batches[0].Text)
	}
}

func TestFormatting(t *testing.T) {
	sizes := map[int64]string{41 << 30: "41G", 6549825126: "6.1G", 820 << 20: "820M",
		999 << 20: "999M", 1000 << 20: "1G", 512: "512B", 100 << 20: "100M"}
	for bytes, want := range sizes {
		if got := formatSize(bytes); got != want {
			t.Errorf("formatSize(%d) = %s, want %s", bytes, got, want)
		}
	}
	counts := map[int64]string{9: "9", 999: "999", 9412: "9.4k", 9000: "9k", 21095: "21k",
		2_400_000: "2.4M"}
	for count, want := range counts {
		if got := formatCount(count); got != want {
			t.Errorf("formatCount(%d) = %s, want %s", count, got, want)
		}
	}
}

var placeholderLike = regexp.MustCompile(`<R\d+>`)

// The worked example teaches the reply format, so it must itself be a valid reply.
func TestWorkedExampleParses(t *testing.T) {
	fixture := newFixture(t, nil)
	text := Build(fixture.result.Tree, fixture.redactor, "7KQX2M", DefaultOptions).
		Batches[0].Text
	start := strings.Index(text, "@SESSION "+exampleSessionCode)
	end := strings.Index(text, "@END "+exampleSessionCode)
	example := text[start : end+len("@END "+exampleSessionCode)]
	result := response.Parse(example, response.Expectation{Code: exampleSessionCode,
		Batch: 1, BatchCount: 1, FirstID: 1, LastID: 10})
	if !result.Valid() || len(result.Categories) != 4 {
		t.Fatalf("worked example does not parse: %+v", result)
	}
	if result.Categories[0].What == "" || len(result.Categories[2].Steps) != 2 {
		t.Fatalf("worked example lacks explanations: %+v", result.Categories)
	}
	if !strings.Contains(text, "under 25 words") || !strings.Contains(text, "1 to 5 step:") {
		t.Fatalf("instructions do not state the explanation limits:\n%s", text)
	}
}

// The instructions promise the limits the parser enforces.
func TestExplanationLimitsMatchParser(t *testing.T) {
	if ExplanationWordTarget != response.ExplanationWordTarget ||
		ExplanationMaximumSteps != response.MaximumExplanationSteps {
		t.Fatal("prompt and response packages disagree on explanation limits")
	}
}

func TestContentAwareRollup(t *testing.T) {
	fileSystem := scannertest.New()
	for year := 2015; year <= 2020; year++ {
		for monthNumber := 1; monthNumber <= 12; monthNumber++ {
			fileSystem.AddFile("/c/Users/mara/Pictures/"+strconv.Itoa(year)+"/"+
				fmt.Sprintf("%02d", monthNumber)+"/IMG.JPG", 150*megabyte, 0)
		}
	}
	mixed := []string{"setup.exe", "ubuntu.iso", "lecture.mp4", "scan.pdf", "game.zip"}
	for index, name := range mixed {
		fileSystem.AddFile("/c/Users/mara/Downloads/"+name, int64(200+index)*megabyte, 0)
	}
	for index := 0; index < 30; index++ {
		fileSystem.AddFile("/c/Users/mara/Archive/part"+strconv.Itoa(index)+".rar",
			150*megabyte, 0)
	}
	fileSystem.AddFile("/c/Users/mara/Music/loose.flac", 300*megabyte, 0)
	fileSystem.AddFile("/c/Users/mara/Music/Band/album.flac", 400*megabyte, 0)
	result := fileSystem.Scan(t, "/c", nil)
	redactor := redact.New(redact.Options{Tree: result.Tree,
		Registry: redact.NewRegistry(nil, 1, true),
		Context:  redact.Context{CurrentProfile: filepath.FromSlash("/c/Users/mara")}})
	text := Build(result.Tree, redactor, "7KQX2M", DefaultOptions).Batches[0].Text
	lines := strings.Join(dataLines(text), "\n")

	if !strings.Contains(lines, "Pictures [72 files] {jpg}") || strings.Contains(lines, "x6") {
		t.Errorf("photo library by year and month should be one line:\n%s", lines)
	}
	for _, name := range mixed {
		if !strings.Contains(lines, name) {
			t.Errorf("mixed downloads should be listed per file; %s missing:\n%s", name, lines)
		}
	}
	if !strings.Contains(lines, "Archive [30 files] {rar}") {
		t.Errorf("a homogeneous archive of many files should be one line:\n%s", lines)
	}
	if !strings.Contains(lines, "Music/* ") && !strings.Contains(lines, "loose.flac") {
		t.Errorf("loose contents line missing:\n%s", lines)
	}
}

// A home folder hidden as a whole tree used to become a single line with only a size, so
// the assistant could only suggest keeping it. With well-known names kept, the profile
// layout splits it into lines the assistant can act on, and nothing that is not
// well-known is named (decision D-075).
func TestHiddenHomeKeepsProfileLayout(t *testing.T) {
	fixture := newFixture(t, []redact.Rule{{ID: "1", Type: redact.RuleSubtree,
		Pattern: "~", Treatment: redact.TreatmentSubtree, KeepWellKnown: true}})
	export := Build(fixture.result.Tree, fixture.redactor, "7KQX2M", DefaultOptions)
	lines := strings.Join(dataLines(export.Batches[0].Text), "\n")
	for _, expected := range []string{
		"~/AppData/Local/Temp [900 files] m2025-03 {tmp}",
		"~/Videos [25 files] m2019-06 {mp4}",
	} {
		if !strings.Contains(lines, expected) {
			t.Errorf("hidden home lacks %q:\n%s", expected, lines)
		}
	}
	// The rest of the home is listed by size only: its own name is redacted.
	if !regexp.MustCompile(`(?m)^ \d+ \S+ ~/\*$`).MatchString(lines) {
		t.Errorf("the rest of a hidden home should be a bare ~/* line:\n%s", lines)
	}
	for _, leaked := range []string{"projects", "kestrel", "alpha", "Vendor", "archive",
		"blob", "<R"} {
		if strings.Contains(lines, leaked) {
			t.Errorf("hidden home leaks %q:\n%s", leaked, lines)
		}
	}

	whole := newFixture(t, []redact.Rule{{ID: "1", Type: redact.RuleSubtree,
		Pattern: "~", Treatment: redact.TreatmentSubtree}})
	text := Build(whole.result.Tree, whole.redactor, "7KQX2M", DefaultOptions).Batches[0].Text
	if got := dataLines(text); len(got) != 1 || !strings.HasSuffix(got[0], " ~") {
		t.Errorf("without well-known names the home is one line reading ~, got %q", got)
	}
}
