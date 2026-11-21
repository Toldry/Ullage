package redact

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/toldry/ullage/internal/scanner"
	"github.com/toldry/ullage/internal/scanner/scannertest"
)

const (
	megabyte = 2 << 20
	root     = "/data"
)

var profile = filepath.FromSlash("/data/Users/mara")

func testContext() Context {
	return Context{CurrentProfile: profile, CaseInsensitive: true,
		SensitiveWords: []string{"mara", "DESKTOP-ZX81", "bob", "al"}}
}

type fixture struct {
	fileSystem *scannertest.FileSystem
	result     *scanner.Result
	registry   *Registry
}

func newFixture(t *testing.T, files ...string) *fixture {
	t.Helper()
	fileSystem := scannertest.New()
	for _, file := range files {
		fileSystem.AddFile(file, megabyte, 0)
	}
	return &fixture{fileSystem: fileSystem, result: fileSystem.Scan(t, root, nil),
		registry: NewRegistry(nil, 1, true)}
}

func (fixture *fixture) redactor(rules []Rule, overrides map[scanner.NodeIndex]Override) *Redactor {
	return New(Options{Tree: fixture.result.Tree, Context: testContext(), Rules: rules,
		Overrides: overrides, Registry: fixture.registry})
}

func (fixture *fixture) render(t *testing.T, redactor *Redactor, slashPath string) string {
	t.Helper()
	return redactor.Render(scannertest.Find(t, fixture.result, slashPath))
}

var placeholderPattern = regexp.MustCompile(`<R\d+>`)

func TestAlwaysOnRedactions(t *testing.T) {
	fixture := newFixture(t,
		"/data/Users/mara/projects/app/big.bin",
		"/data/Users/mara/Documents/mara-notes.pdf",
		"/data/Users/mara/Documents/maraBudget.xlsx",
		"/data/Users/bob/Desktop/x.bin",
		"/data/Users/mara/AppData/Local/al-notes/x.bin",
		"/data/Users/mara/AppData/Local/Calendar/x.bin",
		"/data/Backups/DESKTOP-ZX81-image/disk.vhdx",
		"/data/$Recycle.Bin/S-1-5-21-3623811015-3361044348-30300820-1013/x.bin",
		"/data/Users/mara/odd/x.mara")
	redactor := fixture.redactor(nil, nil)
	cases := map[string]string{
		"/data/Users/mara/projects/app/big.bin":         "/data/Users/~/projects/app/big.bin",
		"/data/Users/mara/AppData/Local/Calendar/x.bin": "/data/Users/~/AppData/Local/Calendar/x.bin",
	}
	for path, want := range cases {
		if got := fixture.render(t, redactor, path); got != want {
			t.Errorf("Render(%s) = %s, want %s", path, got, want)
		}
	}
	redactedNames := []struct{ path, pattern string }{
		{"/data/Users/mara/Documents/mara-notes.pdf",
			`^/data/Users/~/Documents/<R\d+>\.pdf$`},
		{"/data/Users/mara/Documents/maraBudget.xlsx",
			`^/data/Users/~/Documents/<R\d+>\.xlsx$`},
		{"/data/Users/bob/Desktop/x.bin",
			`^/data/Users/<R\d+>/Desktop/x\.bin$`},
		{"/data/Users/mara/AppData/Local/al-notes/x.bin",
			`^/data/Users/~/AppData/Local/<R\d+>/x\.bin$`},
		{"/data/Backups/DESKTOP-ZX81-image/disk.vhdx",
			`^/data/Backups/<R\d+>/disk\.vhdx$`},
		{"/data/$Recycle.Bin/S-1-5-21-3623811015-3361044348-30300820-1013/x.bin",
			`^/data/\$Recycle\.Bin/<R\d+>/x\.bin$`},
		{"/data/Users/mara/odd/x.mara",
			`^/data/Users/~/odd/<R\d+>\.mara$`},
	}
	for _, entry := range redactedNames {
		path, pattern := entry.path, entry.pattern
		if got := fixture.render(t, redactor, path); !regexp.MustCompile(pattern).MatchString(got) {
			t.Errorf("Render(%s) = %s, want match %s", path, got, pattern)
		}
	}
}

func TestSubtreeRuleHidesEveryNameButWellKnownOnes(t *testing.T) {
	fixture := newFixture(t,
		"/data/Users/mara/projects/acme/build/out.bin",
		"/data/Users/mara/projects/acme/src/engine/core.bin",
		"/data/Users/mara/projects/other/old-acme-site/x.bin")
	rules := []Rule{{ID: "1", Type: RuleExact, Pattern: "~/projects/acme",
		Treatment: TreatmentSubtree, KeepWellKnown: true}}
	redactor := fixture.redactor(rules, nil)

	build := fixture.render(t, redactor, "/data/Users/mara/projects/acme/build/out.bin")
	if !regexp.MustCompile(`^/data/Users/~/projects/<R\d+>/build/<R\d+>$`).MatchString(build) {
		t.Errorf("build path = %s", build)
	}
	source := fixture.render(t, redactor, "/data/Users/mara/projects/acme/src/engine/core.bin")
	if strings.Contains(source, "src") || strings.Contains(source, "engine") ||
		strings.Contains(source, "acme") {
		t.Errorf("hidden subtree leaked a name: %s", source)
	}
	// Selecting an item redacts that item only; an unrelated folder with a similar name
	// stays visible (decision D-027, revised).
	unrelated := fixture.render(t, redactor,
		"/data/Users/mara/projects/other/old-acme-site/x.bin")
	if unrelated != "/data/Users/~/projects/other/old-acme-site/x.bin" {
		t.Errorf("unrelated folder was redacted: %s", unrelated)
	}
	match := redactor.Matches()[0]
	if len(match.Nodes) != 1 || match.Bytes != 2*megabyte {
		t.Errorf("match = %+v", match)
	}
	withoutKeep := fixture.redactor([]Rule{{ID: "1", Type: RuleExact,
		Pattern: "~/projects/acme", Treatment: TreatmentSubtree}}, nil)
	if got := fixture.render(t, withoutKeep,
		"/data/Users/mara/projects/acme/build/out.bin"); strings.Contains(got, "build") {
		t.Errorf("well-known name kept although the option is off: %s", got)
	}
}

func TestNameGlobAndSubtreeKeepExtensionRules(t *testing.T) {
	fixture := newFixture(t,
		"/data/Users/mara/Downloads/2025-tax-return.pdf",
		"/data/Users/mara/Desktop/TAX notes.txt",
		"/data/Users/mara/clients/northwind/contract.docx",
		"/data/Users/mara/legal/case-114/brief.docx",
		"/data/Users/mara/legal/case-114/exhibits/photo.jpg")
	rules := []Rule{
		{ID: "n", Type: RuleName, Pattern: "*tax*", Treatment: TreatmentKeepExtension},
		{ID: "g", Type: RuleGlob, Pattern: "~/clients/*", Treatment: TreatmentSubtree},
		{ID: "s", Type: RuleSubtree, Pattern: "~/legal/case-114",
			Treatment: TreatmentKeepExtension},
	}
	redactor := fixture.redactor(rules, nil)
	expectations := []struct{ path, pattern string }{
		{"/data/Users/mara/Downloads/2025-tax-return.pdf",
			`^/data/Users/~/Downloads/<R\d+>\.pdf$`},
		{"/data/Users/mara/Desktop/TAX notes.txt",
			`^/data/Users/~/Desktop/<R\d+>\.txt$`},
		{"/data/Users/mara/clients/northwind/contract.docx",
			`^/data/Users/~/clients/<R\d+>/<R\d+>$`},
		{"/data/Users/mara/legal/case-114/brief.docx",
			`^/data/Users/~/legal/<R\d+>/<R\d+>\.docx$`},
		{"/data/Users/mara/legal/case-114/exhibits/photo.jpg",
			`^/data/Users/~/legal/<R\d+>/<R\d+>/<R\d+>\.jpg$`},
	}
	for _, entry := range expectations {
		path, pattern := entry.path, entry.pattern
		if got := fixture.render(t, redactor, path); !regexp.MustCompile(pattern).MatchString(got) {
			t.Errorf("Render(%s) = %s, want match %s", path, got, pattern)
		}
	}
	if count := len(redactor.Matches()[0].Nodes); count != 2 {
		t.Errorf("name rule matched %d items, want 2", count)
	}
}

func TestSessionOverrides(t *testing.T) {
	fixture := newFixture(t, "/data/Users/mara/Documents/report.pdf",
		"/data/Users/mara/projects/kept/x.bin")
	documents := scannertest.Find(t, fixture.result, "/data/Users/mara/Documents")
	kept := scannertest.Find(t, fixture.result, "/data/Users/mara/projects/kept")
	rules := []Rule{{ID: "1", Type: RuleExact, Pattern: "~/projects/kept",
		Treatment: TreatmentSubtree}}
	redactor := fixture.redactor(rules, map[scanner.NodeIndex]Override{
		documents: {Treatment: TreatmentSubtree},
		kept:      {Treatment: TreatmentNone},
	})
	if got := fixture.render(t, redactor, "/data/Users/mara/Documents/report.pdf"); !regexp.
		MustCompile(`^/data/Users/~/<R\d+>/<R\d+>$`).MatchString(got) {
		t.Errorf("session redaction not applied: %s", got)
	}
	if got := fixture.render(t, redactor, "/data/Users/mara/projects/kept/x.bin"); got !=
		"/data/Users/~/projects/kept/x.bin" {
		t.Errorf("session 'don't redact' not applied: %s", got)
	}
	if redactor.State(documents).Source != SourceSession {
		t.Errorf("source = %v", redactor.State(documents).Source)
	}
}

func TestAnchoredRuleFollowsRename(t *testing.T) {
	fileSystem := scannertest.New()
	fileSystem.AddFile("/data/Users/mara/projects/zeta/x.bin", megabyte, 0)
	before := fileSystem.Scan(t, root, nil)
	anchor := before.Tree.Node(scannertest.Find(t, before, "/data/Users/mara/projects/zeta")).ID
	rule := Rule{ID: "1", Type: RuleExact, Pattern: "~/projects/zeta",
		Treatment: TreatmentSubtree, Anchor: &anchor}

	fileSystem.Rename("/data/Users/mara/projects/zeta", "/data/Users/mara/archive/omega")
	fileSystem.AddFile("/data/Users/mara/projects/zeta/new.bin", megabyte, 0)
	after := fileSystem.Scan(t, root, nil)
	redactor := New(Options{Tree: after.Tree, Context: testContext(), Rules: []Rule{rule},
		Registry: NewRegistry(nil, 1, true)})

	moved := scannertest.Find(t, after, "/data/Users/mara/archive/omega")
	if redactor.State(moved).Decision != DecisionRedacted {
		t.Fatalf("rule did not follow the renamed folder")
	}
	if redactor.Matches()[0].MovedTo != moved {
		t.Fatalf("move not reported")
	}
	replacement := scannertest.Find(t, after, "/data/Users/mara/projects/zeta")
	if redactor.State(replacement).Decision != DecisionVisible {
		t.Fatalf("new folder at the old path was redacted instead")
	}
}

func TestPlaceholdersAreStableAndSanitized(t *testing.T) {
	fixture := newFixture(t, "/data/Users/mara/a/secret/x.bin",
		"/data/Users/mara/b/secret/y.bin", "/data/weird/<R1>|name/z.bin")
	rules := []Rule{{ID: "1", Type: RuleName, Pattern: "secret", Treatment: TreatmentSubtree}}
	first := fixture.redactor(rules, nil)
	a := fixture.render(t, first, "/data/Users/mara/a/secret/x.bin")
	b := fixture.render(t, first, "/data/Users/mara/b/secret/y.bin")
	if strings.Split(a, "/")[5] != strings.Split(b, "/")[5] {
		t.Errorf("same name got different placeholders: %s %s", a, b)
	}
	numbers, next, changed := fixture.registry.Snapshot()
	if !changed {
		t.Fatalf("registry change not reported")
	}
	restored := NewRegistry(numbers, next, true)
	second := New(Options{Tree: fixture.result.Tree, Context: testContext(), Rules: rules,
		Registry: restored})
	if got := fixture.render(t, second, "/data/Users/mara/a/secret/x.bin"); got != a {
		t.Errorf("placeholder changed after restore: %s vs %s", got, a)
	}
	weird := fixture.render(t, first, "/data/weird/<R1>|name/z.bin")
	if placeholderPattern.MatchString(weird) || strings.Contains(weird, "|") {
		t.Errorf("a real name imitates a placeholder: %s", weird)
	}
}

func TestRuleValidation(t *testing.T) {
	valid := Rule{Type: RuleExact, Pattern: "~/x", Treatment: TreatmentSubtree}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	invalid := []Rule{
		{Type: "regex", Pattern: "x", Treatment: TreatmentSubtree},
		{Type: RuleExact, Pattern: " ", Treatment: TreatmentSubtree},
		{Type: RuleExact, Pattern: "x", Treatment: TreatmentNone},
		{Type: RuleName, Pattern: "a/b", Treatment: TreatmentSubtree},
	}
	for _, rule := range invalid {
		if rule.Validate() == nil {
			t.Errorf("rule %+v accepted", rule)
		}
	}
}

func TestShortWordsMatchWholeWordsOnly(t *testing.T) {
	cases := []struct {
		name, word string
		want       bool
	}{
		{"Local", "al", false}, {"al-notes", "al", true}, {"notesAl", "al", true},
		{"Albums", "al", false}, {"marathon", "mara", true}, {"MARA", "mara", true},
	}
	for _, testCase := range cases {
		if got := containsSensitive(testCase.name, testCase.word); got != testCase.want {
			t.Errorf("containsSensitive(%q, %q) = %v", testCase.name, testCase.word, got)
		}
	}
}

func BenchmarkRedactorWithManyRedactedNames(b *testing.B) {
	fileSystem := scannertest.New()
	for index := 0; index < 20000; index++ {
		fileSystem.AddFile("/data/Users/mara/docs/"+strings.Repeat("n", index%7+4)+
			"-"+strconv.Itoa(index)+"/file-"+strconv.Itoa(index)+".pdf", megabyte, 0)
	}
	result := fileSystem.Scan(b, root, nil)
	rules := []Rule{{ID: "1", Type: RuleSubtree, Pattern: "~/docs",
		Treatment: TreatmentKeepExtension}}
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		New(Options{Tree: result.Tree, Context: testContext(), Rules: rules,
			Registry: NewRegistry(nil, 1, true)})
	}
}

// A rule on the home folder hides what is inside, but ~ itself says nothing about the
// user, so it keeps reading ~ instead of becoming a placeholder. It still carries no
// metadata, and the well-known profile layout stays readable (decision D-075).
func TestRedactedHomeStaysTilde(t *testing.T) {
	fixture := newFixture(t,
		"/data/Users/mara/AppData/Local/Vendor/blob.bin",
		"/data/Users/mara/clients/acme/plan.bin")
	for _, treatment := range []Treatment{TreatmentSubtree, TreatmentKeepExtension} {
		redactor := fixture.redactor([]Rule{{ID: "1", Type: RuleSubtree, Pattern: "~",
			Treatment: treatment, KeepWellKnown: true}}, nil)
		home := scannertest.Find(t, fixture.result, "/data/Users/mara")
		if got := redactor.Render(home); got != "/data/Users/~" {
			t.Errorf("%s: home renders as %q, want /data/Users/~", treatment, got)
		}
		if redactor.NameShown(home) {
			t.Errorf("%s: a redacted home must not count as shown (no metadata)", treatment)
		}
		if state := redactor.State(home); state.Decision == DecisionVisible ||
			state.Decision == DecisionHome {
			t.Errorf("%s: home decision is %v, want a redaction", treatment, state.Decision)
		}
		clients := fixture.render(t, redactor, "/data/Users/mara/clients/acme/plan.bin")
		if strings.Contains(clients, "clients") || strings.Contains(clients, "acme") ||
			strings.Contains(clients, "plan") {
			t.Errorf("%s: names inside the home leaked: %s", treatment, clients)
		}
	}
	redactor := fixture.redactor([]Rule{{ID: "1", Type: RuleSubtree, Pattern: "~",
		Treatment: TreatmentSubtree, KeepWellKnown: true}}, nil)
	rendered := fixture.render(t, redactor, "/data/Users/mara/AppData/Local/Vendor/blob.bin")
	if !regexp.MustCompile(`^/data/Users/~/AppData/Local/<R\d+>/<R\d+>$`).MatchString(rendered) {
		t.Errorf("profile layout should stay readable inside a hidden home, got %s", rendered)
	}
	home := scannertest.Find(t, fixture.result, "/data/Users/mara")
	overridden := fixture.redactor(nil, map[scanner.NodeIndex]Override{
		home: {Treatment: TreatmentSubtree}})
	if got := overridden.Render(home); got != "/data/Users/~" || overridden.NameShown(home) {
		t.Errorf("a session override on home renders %q, shown=%v", got,
			overridden.NameShown(home))
	}
}
