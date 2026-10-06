package recipes

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestBuiltInRecipesLoadAndCoverTheRequiredList(t *testing.T) {
	library, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	required := []string{"pagefile", "hibernation-file", "component-store", "restore-points",
		"previous-windows", "delivery-optimization", "recycle-bin", "user-temp",
		"docker-desktop-wsl", "wsl-distro-disk", "npm-cache", "nuget-packages", "pip-cache",
		"gradle-caches", "maven-repository", "cargo-registry", "huggingface-cache",
		"ollama-models", "visual-studio-package-cache", "installed-applications"}
	for _, id := range required {
		if library.ByID(id) == nil {
			t.Errorf("missing recipe %s", id)
		}
	}
	if len(library.System.Paths["windows"]) == 0 {
		t.Errorf("no System-tier rules for windows")
	}
	if library.ByID("docker-desktop-wsl").BruteForce.Allowed {
		t.Errorf("Docker Desktop disk image must be managed-only")
	}
	for position := 1; position < len(library.Recipes); position++ {
		if library.Recipes[position-1].Priority > library.Recipes[position].Priority {
			t.Fatalf("recipes not sorted by priority")
		}
	}
}

func loadSingle(t *testing.T, content string) error {
	t.Helper()
	_, err := LoadFrom(fstest.MapFS{"data/r.toml": {Data: []byte(content)}}, "data")
	return err
}

const validRecipe = `
id = "example"
version = 1
name = "Example"
what = "w"
lost = "l"
regenerable = "yes"
[paths]
windows = ["{UserProfile}/x"]
[[managed_steps]]
text = "do it"
[brute_force]
allowed = true
`

func TestValidationAcceptsAMinimalRecipe(t *testing.T) {
	if err := loadSingle(t, validRecipe); err != nil {
		t.Fatal(err)
	}
}

func TestValidationRejectsBadRecipes(t *testing.T) {
	cases := map[string]string{
		"bad enum":           strings.Replace(validRecipe, `"yes"`, `"maybe"`, 1),
		"unknown key":        validRecipe + "\ncolour = \"red\"\n",
		"no reason":          strings.Replace(validRecipe, "allowed = true", "allowed = false", 1),
		"protected location": strings.Replace(validRecipe, "{UserProfile}/x", "{SystemRoot}/x", 1),
		"bad id":             strings.Replace(validRecipe, `"example"`, `"Bad Id"`, 1),
		"unknown platform":   strings.Replace(validRecipe, "windows =", "macos =", 1),
	}
	for name, content := range cases {
		if err := loadSingle(t, content); err == nil {
			t.Errorf("%s: expected a validation error", name)
		}
	}
}

// Long recipe strings are wrapped with TOML line continuations; this catches a wrap that
// leaves a newline, a doubled space or whitespace inside a path.
func TestWrappedStringsJoinCleanly(t *testing.T) {
	library, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, recipe := range library.Recipes {
		texts := []string{recipe.Name, recipe.What, recipe.Lost, recipe.ManagedNote,
			recipe.BruteForce.Reason}
		for _, step := range recipe.ManagedSteps {
			texts = append(texts, step.Text, step.Command)
		}
		for _, text := range texts {
			if strings.ContainsAny(text, "\n\r\t") || strings.Contains(text, "  ") ||
				text != strings.TrimSpace(text) {
				t.Errorf("%s: badly joined text %q", recipe.ID, text)
			}
		}
		for _, patterns := range recipe.Paths {
			for _, pattern := range patterns {
				if strings.ContainsAny(pattern, "\n\t") || strings.Contains(pattern, "/ ") {
					t.Errorf("%s: badly joined path %q", recipe.ID, pattern)
				}
			}
		}
	}
}
