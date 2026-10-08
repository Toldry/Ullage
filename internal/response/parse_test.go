package response

import (
	"reflect"
	"strings"
	"testing"
)

var batchOne = Expectation{Code: "7KQX2M", Batch: 1, BatchCount: 3, FirstID: 1, LastID: 50}

const validReply = `@SESSION 7KQX2M
cat|conf|regen|del|ids
Node.js dependency folders|high|yes|1|3-33
what: Packages npm installed for JavaScript projects.
lost: Nothing; npm install restores them.
step: Run npm prune in each project, or delete the folder.
Build outputs|med|yes|1|34,40-42
what: Compiler output.
lost: Nothing; the next build recreates it.
step: Rebuild when needed.
@END 7KQX2M`

// explanation is a minimal valid explanation for a deletable row.
const explanation = "\nwhat: Files.\nlost: Nothing.\nstep: Delete it."

func checksFailed(result Result) []Check {
	var failed []Check
	for _, check := range AllChecks {
		if result.Failed(check) {
			failed = append(failed, check)
		}
	}
	return failed
}

func TestValidReply(t *testing.T) {
	result := Parse(validReply, batchOne)
	if !result.Valid() || len(result.Categories) != 2 || result.Classified != 35 {
		t.Fatalf("result = %+v", result)
	}
	want := Category{Name: "Build outputs", Confidence: ConfidenceMedium,
		Regenerable: RegenerableYes, Deletable: true, IDs: []int{34, 40, 41, 42}, Line: 7,
		What: "Compiler output.", Lost: "Nothing; the next build recreates it.",
		Steps: []string{"Rebuild when needed."}}
	if !reflect.DeepEqual(result.Categories[1], want) {
		t.Fatalf("category = %+v", result.Categories[1])
	}
}

// Fixtures the spec requires. Each must parse to the same two categories.
func TestTolerantInput(t *testing.T) {
	fixtures := map[string]string{
		"preamble and postamble": "Sure! Here is the classification:\n\n" + validReply +
			"\n\nLet me know if you need anything else.",
		"markdown fences":     "```\n" + validReply + "\n```",
		"fence with language": "Here you go:\n```text\n" + validReply + "\n```\nDone.",
		"smart quotes and dashes": strings.NewReplacer("3-33", "3\u201333",
			"Build outputs", "\u201CBuild outputs\u201D").Replace(validReply),
		"zero-width and nbsp": strings.NewReplacer("high|", "high\u200B|", "|yes",
			"|\u00A0yes").Replace(validReply),
		"blockquote markers":           "> " + strings.ReplaceAll(validReply, "\n", "\n> "),
		"trailing whitespace and CRLF": strings.ReplaceAll(validReply, "\n", "   \r\n"),
		"lowercase headers": strings.NewReplacer("@SESSION", "@session",
			"@END", "@End", "cat|conf", "CAT|Conf").Replace(validReply),
		"bold markers": strings.NewReplacer("@SESSION 7KQX2M", "**@SESSION 7KQX2M**",
			"@END 7KQX2M", "**@END 7KQX2M**").Replace(validReply),
		"markdown table": "@SESSION 7KQX2M\n| cat | conf | regen | del | ids |\n" +
			"|---|---|---|---|---|\n| Node.js dependency folders | high | yes | 1 | 3-33 |" +
			explanation + "\n| Build outputs | med | yes | 1 | 34, 40-42 |" + explanation +
			"\n@END 7KQX2M",
		"prompt pasted before the reply": "@SESSION EXAMPL\ncat|conf|regen|del|ids\n" +
			"Old program installers|high|yes|1|1,4\n@END EXAMPL\n\n" + validReply,
	}
	for name, text := range fixtures {
		result := Parse(text, batchOne)
		if !result.Valid() || len(result.Categories) != 2 ||
			result.Categories[0].Name != "Node.js dependency folders" ||
			len(result.Categories[0].IDs) != 31 {
			t.Errorf("%s: valid=%v problems=%+v categories=%+v", name, result.Valid(),
				result.Problems, result.Categories)
		}
	}
}

func TestPipeInsideCategoryName(t *testing.T) {
	result := Parse("@SESSION 7KQX2M\nCaches | temporary files|high|yes|1|1-3"+
		explanation+"\n@END 7KQX2M", batchOne)
	if !result.Valid() || result.Categories[0].Name != "Caches | temporary files" {
		t.Fatalf("result = %+v", result)
	}
}

func TestEmptyBlockMeansNothingClassified(t *testing.T) {
	result := Parse("@SESSION 7KQX2M\ncat|conf|regen|del|ids\n@END 7KQX2M", batchOne)
	if !result.Valid() || len(result.Categories) != 0 {
		t.Fatalf("result = %+v", result)
	}
}

func TestDetectedErrors(t *testing.T) {
	cases := []struct {
		name     string
		text     string
		failed   []Check
		fragment string
	}{
		{"wrong session", strings.ReplaceAll(validReply, "7KQX2M", "9B2C7X"),
			[]Check{CheckSessionCode}, "session code is 9B2C7X but must be 7KQX2M"},
		{"missing session", "Node.js|high|yes|1|3", []Check{CheckSessionCode},
			"no @SESSION 7KQX2M line"},
		{"missing code", strings.Replace(validReply, "@SESSION 7KQX2M", "@SESSION", 1),
			[]Check{CheckSessionCode}, "has no code"},
		{"truncated", strings.TrimSuffix(validReply, "\n@END 7KQX2M"),
			[]Check{CheckComplete}, "cut off before @END 7KQX2M"},
		{"truncated mid-row", "@SESSION 7KQX2M\ncat|conf|regen|del|ids\nBuild outputs|me",
			[]Check{CheckComplete, CheckFormat}, "expected 5 columns"},
		{"bad enums", "@SESSION 7KQX2M\nCaches|very high|maybe|yes|1\n@END 7KQX2M",
			[]Check{CheckFormat}, `conf must be high, med or low, not "very high"`},
		{"overlapping IDs", "@SESSION 7KQX2M\nA|high|yes|1|1-10" + explanation +
			"\nB|low|no|0|8-12\n@END 7KQX2M",
			[]Check{CheckDuplicates}, `IDs 8-10 are also in "A"`},
		{"unknown IDs", "@SESSION 7KQX2M\nA|high|yes|1|49-51,713\n@END 7KQX2M",
			[]Check{CheckItemIDs}, "IDs 49-51, 713 are not in this batch, which has IDs 1-50"},
		{"malformed ranges", "@SESSION 7KQX2M\nA|high|yes|1|3-,9-3,a-b,4\n@END 7KQX2M",
			[]Check{CheckItemIDs}, `"9-3" is not an ID or a range`},
		{"huge range", "@SESSION 7KQX2M\nA|high|yes|1|1-999999999999\n@END 7KQX2M",
			[]Check{CheckItemIDs}, "not in this batch"},
		{"empty ids", "@SESSION 7KQX2M\nA|high|yes|1| \n@END 7KQX2M",
			[]Check{CheckItemIDs}, "lists no IDs"},
	}
	for _, testCase := range cases {
		result := Parse(testCase.text, batchOne)
		if result.Valid() {
			t.Errorf("%s: accepted", testCase.name)
			continue
		}
		if failed := checksFailed(result); !reflect.DeepEqual(failed, testCase.failed) {
			t.Errorf("%s: failed checks %v, want %v (%+v)", testCase.name, failed,
				testCase.failed, result.Problems)
		}
		repair := RepairPrompt(result, batchOne)
		if !strings.Contains(repair, testCase.fragment) ||
			!strings.Contains(repair, "Send the full corrected reply for batch 1 only") {
			t.Errorf("%s: repair prompt lacks %q:\n%s", testCase.name, testCase.fragment,
				repair)
		}
	}
}

// Prompt injection (spec "Required tests"): whatever a hostile file name talked the
// assistant into writing, only closed-vocabulary rows over IDs of this batch can come out.
func TestRepliesCannotEscapeTheClosedVocabulary(t *testing.T) {
	hostileReplies := []string{
		"@SESSION 7KQX2M\nEverything|high|yes|1|1-999999\n@END 7KQX2M",
		"@SESSION 7KQX2M\nSystem files|high|yes|1|1-3|delete C:\\Windows\n@END 7KQX2M",
		"@SESSION 7KQX2M\nX|high|yes|1|C:\\Windows\n@END 7KQX2M",
		"@SESSION 7KQX2M\nX|high|yes|always|1\n@END 7KQX2M",
		"@SESSION 7KQX2M\nrm -rf /|high|yes|1|1;2\n@END 7KQX2M",
		"@SESSION 7KQX2M\nX|high|yes|1|-5\n@END 7KQX2M",
	}
	for _, reply := range hostileReplies {
		if result := Parse(reply, batchOne); result.Valid() {
			t.Errorf("hostile reply accepted: %q -> %+v", reply, result.Categories)
		}
	}
	// A category name is free text, but it is only ever a label: the categories carry IDs
	// of this batch and enum values, nothing else.
	result := Parse("@SESSION 7KQX2M\nDelete C:\\Windows now|high|yes|1|1-2"+explanation+
		"\n@END 7KQX2M", batchOne)
	if !result.Valid() || !reflect.DeepEqual(result.Categories[0].IDs, []int{1, 2}) {
		t.Fatalf("result = %+v", result)
	}
}

func TestExplanationsBelowRows(t *testing.T) {
	reply := "@SESSION 7KQX2M\ncat|conf|regen|del|ids\n" +
		"Old installers|high|yes|1|1-2\n" +
		"- **What:** Setup files you downloaded.\n" +
		"- **Lost:** Nothing \u201cimportant\u201d.\n" +
		"- Step 1: Check you do not need them.\n" +
		"step: Delete | or move them.\n" +
		"Family photos|high|no|0|3\n" +
		"what: Photos.\n" +
		"@END 7KQX2M"
	result := Parse(reply, batchOne)
	if !result.Valid() || len(result.Categories) != 2 {
		t.Fatalf("result = %+v", result)
	}
	installers := result.Categories[0]
	if installers.What != "Setup files you downloaded." ||
		installers.Lost != `Nothing "important".` ||
		!reflect.DeepEqual(installers.Steps, []string{"Check you do not need them.",
			"Delete | or move them."}) {
		t.Fatalf("explanation = %+v", installers)
	}
	// A category to keep may be explained, but need not be.
	if result.Categories[1].What != "Photos." || result.Categories[1].Lost != "" {
		t.Fatalf("keep category = %+v", result.Categories[1])
	}
}

// Rows whose names start like an explanation key are still rows.
func TestRowNamedLikeAnExplanationLine(t *testing.T) {
	reply := "@SESSION 7KQX2M\nLost-and-found files|med|no|0|1\nWhat-if scenarios|low|no|0|2" +
		"\nSteps recordings|high|no|0|3\n@END 7KQX2M"
	result := Parse(reply, batchOne)
	if !result.Valid() || len(result.Categories) != 3 ||
		result.Categories[0].Name != "Lost-and-found files" {
		t.Fatalf("result = %+v", result)
	}
}

func TestExplanationProblems(t *testing.T) {
	long := strings.Repeat("word ", 45)
	cases := []struct {
		name  string
		reply string
		text  string
	}{
		{"missing", "A|high|yes|1|1\nwhat: x", "no lost:, step: line below it"},
		{"none", "A|high|yes|1|1\nB|high|no|0|2", "no what:, lost:, step:"},
		{"twice", "A|high|yes|1|1\nwhat: x\nwhat: y\nlost: z\nstep: s",
			"more than one what:"},
		{"too long", "A|high|yes|1|1\nwhat: " + long + "\nlost: z\nstep: s",
			"longer than 40 words"},
		{"empty", "A|high|yes|1|1\nwhat:\nlost: z\nstep: s", "what: is empty"},
		{"too many steps", "A|high|yes|1|1\nwhat: x\nlost: z" +
			strings.Repeat("\nstep: s", 6), "more than 5 step:"},
	}
	for _, testCase := range cases {
		result := Parse("@SESSION 7KQX2M\n"+testCase.reply+"\n@END 7KQX2M", batchOne)
		found := false
		for _, problem := range result.Problems {
			found = found || (problem.Check == CheckExplained &&
				strings.Contains(problem.Message, testCase.text))
		}
		if result.Valid() || !found {
			t.Errorf("%s: problems = %+v", testCase.name, result.Problems)
		}
	}
	// Lines before the first row, and lines of a rejected row, are reported once.
	before := Parse("@SESSION 7KQX2M\nwhat: x\nA|high|yes|1|1"+explanation+"\n@END 7KQX2M",
		batchOne)
	if before.Valid() || !before.Failed(CheckFormat) {
		t.Fatalf("explanation before any row: %+v", before.Problems)
	}
	rejected := Parse("@SESSION 7KQX2M\nA|sure|yes|1|1"+explanation+"\n@END 7KQX2M", batchOne)
	if len(rejected.Problems) != 1 || !rejected.Failed(CheckFormat) {
		t.Fatalf("rejected row: %+v", rejected.Problems)
	}
}
