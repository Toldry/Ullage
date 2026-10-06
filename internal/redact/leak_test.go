package redact

import (
	"math/rand"
	"path"
	"strings"
	"testing"

	"github.com/toldry/ullage/internal/scanner"
	"github.com/toldry/ullage/internal/scanner/scannertest"
)

// The required redaction leak test: for every redacted item, no rendered path may contain
// its original name (case-insensitive), including through descendants, and the username
// and machine name never appear. Phase 3 runs the same assertions on complete prompts.

var leakVocabulary = []string{"projects", "acme", "acme-corp", "build", "src", "tax-2025",
	"client-northwind", "notes", "photos", "case-114", "DESKTOP-ZX81", "mara", "MaraPhotos",
	"bob", "al", "Local", "S-1-5-21-111-222-333-1001", "x", "node_modules", "renders"}

var leakExtensions = []string{"pdf", "mp4", "bin", "mara", "docx", ""}

func generateLeakTree(t *testing.T, seed int64) *scanner.Result {
	random := rand.New(rand.NewSource(seed))
	fileSystem := scannertest.New()
	bases := []string{"/data/Users/mara", "/data/Users/bob", "/data/Shared"}
	for file := 0; file < 1500; file++ {
		segments := []string{bases[random.Intn(len(bases))]}
		for depth := random.Intn(5); depth >= 0; depth-- {
			segments = append(segments, leakVocabulary[random.Intn(len(leakVocabulary))])
		}
		name := leakVocabulary[random.Intn(len(leakVocabulary))]
		if extension := leakExtensions[random.Intn(len(leakExtensions))]; extension != "" {
			name += "." + extension
		}
		fileSystem.AddFile(path.Join(append(segments, name)...), megabyte, 0)
	}
	return fileSystem.Scan(t, root, nil)
}

var leakRules = []Rule{
	{ID: "1", Type: RuleExact, Pattern: "~/acme", Treatment: TreatmentSubtree,
		KeepWellKnown: true},
	{ID: "2", Type: RuleName, Pattern: "*tax*", Treatment: TreatmentKeepExtension},
	{ID: "3", Type: RuleGlob, Pattern: "~/projects/*/client-*", Treatment: TreatmentSubtree},
	{ID: "4", Type: RuleSubtree, Pattern: "~/case-114", Treatment: TreatmentKeepExtension},
	{ID: "5", Type: RuleName, Pattern: "build", Treatment: TreatmentSubtree},
}

// mustNotAppear applies the same short-word threshold the redactor uses (decision D-028).
func mustNotAppear(t *testing.T, rendered, secret, why string) {
	t.Helper()
	for _, segment := range strings.Split(rendered, "/") {
		if containsSensitive(segment, secret) {
			t.Errorf("%s %q leaked in %s", why, secret, rendered)
			return
		}
	}
}

func TestRedactionLeak(t *testing.T) {
	for seed := int64(1); seed <= 5; seed++ {
		result := generateLeakTree(t, seed)
		redactor := New(Options{Tree: result.Tree, Context: testContext(), Rules: leakRules,
			Registry: NewRegistry(nil, 1, true)})
		tree := result.Tree

		rendered := make([]string, tree.Len())
		tree.Walk(func(index scanner.NodeIndex, _ *scanner.Node) {
			rendered[index] = redactor.Render(index)
		})

		secretSet := map[string]bool{}
		tree.Walk(func(index scanner.NodeIndex, node *scanner.Node) {
			state := redactor.State(index)
			explicit := state.Source == SourceRule || state.Source == SourceSession
			// In keep-extension mode the extension is public by design; the secret is the
			// rest of the name.
			switch {
			case explicit && state.Decision == DecisionRedacted:
				secretSet[strings.ToLower(node.Name)] = true
			case explicit && state.Decision == DecisionRedactedKeepExtension:
				secretSet[strings.ToLower(stem(node.Name))] = true
			}
			if state.Decision == DecisionHidden && !placeholderPattern.MatchString(
				redactor.RenderName(index)) {
				return
			}
			if state.Decision == DecisionHidden {
				// A hidden name must not show anywhere inside its redacted subtree.
				for descendant := range descendantsOf(tree, index) {
					mustNotAppear(t, rendered[descendant], node.Name, "hidden name")
				}
			}
		})
		var secrets []string
		for secret := range secretSet {
			secrets = append(secrets, secret)
		}
		if len(secrets) == 0 {
			t.Fatalf("seed %d: fixture produced no redactions", seed)
		}
		sensitive := append([]string{"S-1-5-21-111-222-333-1001"},
			testContext().SensitiveWords...)
		for _, line := range rendered {
			for _, secret := range secrets {
				mustNotAppear(t, line, secret, "redacted name")
			}
			for _, word := range sensitive {
				mustNotAppear(t, line, word, "account or machine name")
			}
		}
	}
}

func descendantsOf(tree *scanner.Tree, index scanner.NodeIndex) map[scanner.NodeIndex]bool {
	result := map[scanner.NodeIndex]bool{index: true}
	tree.Walk(func(candidate scanner.NodeIndex, node *scanner.Node) {
		if node.Parent != scanner.NoNode && result[node.Parent] {
			result[candidate] = true
		}
	})
	return result
}
