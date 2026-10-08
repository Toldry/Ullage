// Package recipes loads the built-in, data-driven cleanup recipes. Recipes are TOML files
// embedded in the executable; this package only parses and validates them.
package recipes

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

//go:embed data/*.toml
var embeddedFiles embed.FS

// Regenerable says whether deleted data comes back on its own.
type Regenerable string

const (
	RegenerableYes       Regenerable = "yes"
	RegenerablePartially Regenerable = "partially"
	RegenerableNo        Regenerable = "no"
)

// DynamicInstalledApplications marks the recipe whose paths come from the OS's list of
// installed programs rather than from the file.
const DynamicInstalledApplications = "installed-applications"

// ManagedStep is one step of the cleanup that goes through the owning tool.
type ManagedStep struct {
	Text    string `toml:"text" json:"text"`
	Command string `toml:"command" json:"command,omitempty"`
}

// BruteForcePolicy says whether Ullage itself may delete the matched items.
type BruteForcePolicy struct {
	Allowed bool   `toml:"allowed" json:"allowed"`
	Reason  string `toml:"reason" json:"reason,omitempty"`
	// Warning says what can go wrong when the managed steps are skipped, for recipes where
	// they are the better way but deleting is still reasonable (an uninstalled tool's
	// leftovers). It is shown wherever the items can be selected and confirmed.
	Warning string `toml:"warning" json:"warning,omitempty"`
}

// Recipe describes one well-known kind of disk usage.
type Recipe struct {
	ID           string              `toml:"id" json:"id"`
	Version      int                 `toml:"version" json:"version"`
	Name         string              `toml:"name" json:"name"`
	What         string              `toml:"what" json:"what"`
	Lost         string              `toml:"lost" json:"lost"`
	Regenerable  Regenerable         `toml:"regenerable" json:"regenerable"`
	Paths        map[string][]string `toml:"paths" json:"-"`
	Dynamic      string              `toml:"dynamic" json:"-"`
	ManagedSteps []ManagedStep       `toml:"managed_steps" json:"managedSteps"`
	ManagedNote  string              `toml:"managed_note" json:"managedNote,omitempty"`
	BruteForce   BruteForcePolicy    `toml:"brute_force" json:"bruteForce"`
	// Priority orders recipes when patterns overlap; lower wins. Defaults to file order.
	Priority int `toml:"priority" json:"-"`
}

// SystemRules lists the path patterns of the System tier.
type SystemRules struct {
	Paths map[string][]string `toml:"paths"`
}

// Library is the validated set of built-in recipes plus the System-tier rules.
type Library struct {
	Recipes []*Recipe
	System  SystemRules
	byID    map[string]*Recipe
}

// ByID returns a recipe or nil.
func (library *Library) ByID(id string) *Recipe {
	return library.byID[id]
}

// Load reads and validates the embedded recipe files.
func Load() (*Library, error) {
	return LoadFrom(embeddedFiles, "data")
}

// LoadFrom reads recipes from any file system; tests use it with fixture directories.
func LoadFrom(fileSystem fs.FS, directory string) (*Library, error) {
	entries, err := fs.ReadDir(fileSystem, directory)
	if err != nil {
		return nil, err
	}
	library := &Library{byID: map[string]*Recipe{}}
	var problems []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".toml") {
			continue
		}
		content, err := fs.ReadFile(fileSystem, directory+"/"+entry.Name())
		if err != nil {
			return nil, err
		}
		if entry.Name() == "_system.toml" {
			if err := decodeStrict(content, &library.System); err != nil {
				problems = append(problems, entry.Name()+": "+err.Error())
			}
			continue
		}
		recipe := &Recipe{}
		if err := decodeStrict(content, recipe); err != nil {
			problems = append(problems, entry.Name()+": "+err.Error())
			continue
		}
		for _, problem := range recipe.validate() {
			problems = append(problems, entry.Name()+": "+problem)
		}
		if library.byID[recipe.ID] != nil {
			problems = append(problems, entry.Name()+": duplicate id "+recipe.ID)
		}
		library.byID[recipe.ID] = recipe
		library.Recipes = append(library.Recipes, recipe)
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("invalid recipes:\n  %s", strings.Join(problems, "\n  "))
	}
	sort.SliceStable(library.Recipes, func(first, second int) bool {
		return library.Recipes[first].Priority < library.Recipes[second].Priority
	})
	return library, nil
}

func decodeStrict(content []byte, target any) error {
	metadata, err := toml.NewDecoder(bytes.NewReader(content)).Decode(target)
	if err != nil {
		return err
	}
	if undecoded := metadata.Undecoded(); len(undecoded) > 0 {
		return fmt.Errorf("unknown keys %v", undecoded)
	}
	return nil
}

var identifierPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// protectedVariables are locations Ullage never deletes in (safety rule 4); a recipe
// whose patterns start there must disallow brute force.
var protectedVariables = []string{"{SystemRoot}", "{ProgramFiles}", "{ProgramFilesX86}"}

func (recipe *Recipe) validate() []string {
	var problems []string
	require := func(condition bool, problem string) {
		if !condition {
			problems = append(problems, problem)
		}
	}
	require(identifierPattern.MatchString(recipe.ID), "id must be lowercase-with-dashes")
	require(recipe.Version > 0, "version must be positive")
	require(strings.TrimSpace(recipe.Name) != "", "name is required")
	require(strings.TrimSpace(recipe.What) != "", "what is required")
	require(strings.TrimSpace(recipe.Lost) != "", "lost is required")
	switch recipe.Regenerable {
	case RegenerableYes, RegenerablePartially, RegenerableNo:
	default:
		problems = append(problems, "regenerable must be yes, partially or no")
	}
	require(len(recipe.ManagedSteps) > 0, "at least one managed step is required")
	for _, step := range recipe.ManagedSteps {
		require(strings.TrimSpace(step.Text) != "", "managed step text is required")
	}
	require(recipe.BruteForce.Allowed || strings.TrimSpace(recipe.BruteForce.Reason) != "",
		"brute_force.reason is required when brute force is not allowed")
	require(recipe.BruteForce.Allowed || strings.TrimSpace(recipe.BruteForce.Warning) == "",
		"brute_force.warning is only for recipes that allow brute force")
	require(recipe.Dynamic == "" || recipe.Dynamic == DynamicInstalledApplications,
		"unknown dynamic source "+recipe.Dynamic)

	patternCount := 0
	for platformName, patterns := range recipe.Paths {
		require(platformName == "windows" || platformName == "linux",
			"unknown platform "+platformName)
		for _, pattern := range patterns {
			patternCount++
			for _, variable := range protectedVariables {
				require(!strings.HasPrefix(pattern, variable) || !recipe.BruteForce.Allowed,
					pattern+" is in a protected location, so brute force must be disallowed")
			}
		}
	}
	require(patternCount > 0 || recipe.Dynamic != "", "at least one path pattern is required")
	return problems
}
