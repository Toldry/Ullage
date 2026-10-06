package response

import (
	"strings"
	"testing"
)

var roundB = ExplanationExpectation{Code: "7KQX2M", Round: 4, Numbers: []int{2, 5}}

const specExample = `@SESSION 7KQX2M
#2
what: Packages installed by npm for JavaScript projects.
lost: Nothing permanent; restored by running npm install.
step: Open the project folder in a terminal.
step: Run npm prune, or delete the folder and reinstall later.
@END 7KQX2M`

func TestExplanationSpecExample(t *testing.T) {
	result := ParseExplanations(specExample, roundB)
	if !result.Valid() || len(result.Explanations) != 1 {
		t.Fatalf("result = %+v", result)
	}
	explanation := result.Explanations[0]
	if explanation.Number != 2 || explanation.What != "Packages installed by npm for "+
		"JavaScript projects." || len(explanation.Steps) != 2 {
		t.Fatalf("explanation = %+v", explanation)
	}
	// #5 was not answered: it stays queued, which is not an error.
	if len(result.Missing) != 1 || result.Missing[0] != 5 {
		t.Fatalf("missing = %v", result.Missing)
	}
}

func TestExplanationTolerantWrapping(t *testing.T) {
	wrapped := "Sure! Here are the explanations:\n\n```text\n" +
		"@SESSION 7KQX2M\n" +
		"**#2** npm packages\n" +
		"- **What:** Packages installed by npm.\n" +
		"- **Lost:** Nothing “permanent”.\n" +
		"- Step 1: Run npm prune.\n" +
		"\n" +
		"### #5\n" +
		"WHAT: A cache.\n" +
		"lost: Nothing.\n" +
		"step: Empty it from the app​'s settings.\n" +
		"@END 7KQX2M\n```\nLet me know if you need more."
	result := ParseExplanations(wrapped, roundB)
	if !result.Valid() || len(result.Explanations) != 2 || len(result.Missing) != 0 {
		t.Fatalf("result = %+v", result)
	}
	if result.Explanations[0].Lost != `Nothing "permanent".` ||
		result.Explanations[1].Steps[0] != "Empty it from the app's settings." {
		t.Fatalf("explanations = %+v", result.Explanations)
	}
}

func TestExplanationProblems(t *testing.T) {
	long := strings.Repeat("word ", 45)
	cases := []struct {
		name  string
		reply string
		check Check
		text  string
	}{
		{"wrong session", strings.ReplaceAll(specExample, "7KQX2M", "AAAAAA"),
			CheckSessionCode, "must be 7KQX2M"},
		{"truncated", strings.TrimSuffix(specExample, "@END 7KQX2M"), CheckComplete,
			"cut off"},
		{"not asked", strings.Replace(specExample, "#2", "#3", 1), CheckItemIDs,
			"not asked about"},
		{"twice", strings.Replace(specExample, "@END", "#2\nwhat: a\nlost: b\nstep: c\n@END",
			1), CheckDuplicates, "explained twice"},
		{"no what", strings.Replace(specExample, "what: Packages", "lost: Packages", 1),
			CheckFormat, "exactly one what"},
		{"no steps", strings.NewReplacer("step: Open", "x", "step: Run", "y").Replace(
			specExample), CheckFormat, "expected #number"},
		{"too long", strings.Replace(specExample, "what: Packages", "what: "+long, 1),
			CheckFormat, "longer than 40 words"},
		{"empty", strings.Replace(specExample, "lost: Nothing permanent; restored by "+
			"running npm install.", "lost:", 1), CheckFormat, "lost: is empty"},
		{"before first", "@SESSION 7KQX2M\nwhat: x\n@END 7KQX2M", CheckFormat,
			"before the first"},
		{"nothing", "@SESSION 7KQX2M\n@END 7KQX2M", CheckFormat, "explains no category"},
		{"round A reply", "@SESSION 7KQX2M\ncat|conf|regen|del|ids\nX|high|yes|1|2\n" +
			"@END 7KQX2M", CheckFormat, "expected #number"},
	}
	for _, testCase := range cases {
		result := ParseExplanations(testCase.reply, roundB)
		if result.Valid() || !hasProblem(result.Problems, testCase.check, testCase.text) {
			t.Errorf("%s: problems = %+v", testCase.name, result.Problems)
		}
		repair := RepairExplanationPrompt(result, roundB)
		if !strings.Contains(repair, "@SESSION 7KQX2M") {
			t.Errorf("%s: repair prompt = %q", testCase.name, repair)
		}
	}
}

func TestExplanationStepLimit(t *testing.T) {
	reply := strings.Replace(specExample, "@END",
		strings.Repeat("step: more.\n", MaximumExplanationSteps)+"@END", 1)
	if result := ParseExplanations(reply, roundB); result.Valid() ||
		!hasProblem(result.Problems, CheckFormat, "1 to 5 step") {
		t.Fatalf("problems = %+v", result.Problems)
	}
}

// Explanation text is displayed, so characters that reorder or hide text are removed.
func TestExplanationTextIsCleaned(t *testing.T) {
	reply := strings.Replace(specExample, "what: Packages",
		"what: ‮Safe‬ to \u0007delete. Packages", 1)
	result := ParseExplanations(reply, roundB)
	if !result.Valid() || strings.ContainsAny(result.Explanations[0].What,
		"‮‬\u0007") {
		t.Fatalf("result = %+v", result)
	}
}

func hasProblem(problems []Problem, check Check, text string) bool {
	for _, problem := range problems {
		if problem.Check == check && strings.Contains(problem.Message, text) {
			return true
		}
	}
	return false
}
