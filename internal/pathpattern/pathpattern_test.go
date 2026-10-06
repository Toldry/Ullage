package pathpattern

import (
	"testing"

	"github.com/toldry/ullage/internal/platform"
)

func matches(set *Set, names ...string) bool {
	state := set.Start()
	for _, name := range names {
		if state = set.Advance(state, name); state == nil {
			return false
		}
	}
	return len(set.Accepted(state)) > 0
}

func TestAnyDepthPatterns(t *testing.T) {
	set := NewSet(false)
	set.Add("a/**/c", false, 0)
	for _, path := range [][]string{{"a", "c"}, {"a", "b", "c"}, {"a", "b", "b", "c"}} {
		if !matches(set, path...) {
			t.Errorf("%v should match a/**/c", path)
		}
	}
	if matches(set, "a", "b") || matches(set, "x", "c") {
		t.Errorf("unexpected match")
	}
}

func TestLiteralAndCaseRules(t *testing.T) {
	set := NewSet(true)
	set.Add(`D:\Apps\Brackets [x]`, true, 7)
	if owners := set.MatchesPath(`d:/apps/BRACKETS [X]`); len(owners) != 1 || owners[0] != 7 {
		t.Fatalf("literal case-insensitive match failed: %v", owners)
	}
	if set.MatchesPath(`D:/Apps/Brackets x`) != nil {
		t.Fatalf("literal pattern matched as a glob")
	}
	sensitive := NewSet(false)
	sensitive.Add("/home/*/.npm", false, 0)
	if sensitive.MatchesPath("/home/ada/.NPM") != nil {
		t.Fatalf("case-sensitive set matched different case")
	}
}

func TestExpandVariables(t *testing.T) {
	locations := platform.KnownLocations{Variables: map[string][]string{
		"UserProfile": {`C:\Users\a`, `C:\Users\b`}, "Empty": {""}}}
	if got := ExpandVariables("{UserProfile}/x", locations); len(got) != 2 {
		t.Fatalf("expanded = %v", got)
	}
	if got := ExpandVariables("{Empty}/x", locations); len(got) != 0 {
		t.Fatalf("empty variable should expand to nothing: %v", got)
	}
	if got := ExpandVariables("{Missing}/x", locations); len(got) != 0 {
		t.Fatalf("unknown variable should expand to nothing: %v", got)
	}
}

func TestMatchName(t *testing.T) {
	if !MatchName("*tax*", "2025-TAX-return.pdf", true) || MatchName("*tax*", "notes", true) {
		t.Fatalf("name glob wrong")
	}
}
