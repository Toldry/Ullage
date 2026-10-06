// Package tiers decides which tier every scanned item belongs to: System, Known (claimed by
// a recipe) or Unknown.
package tiers

import (
	"strings"

	"github.com/toldry/ullage/internal/pathpattern"
	"github.com/toldry/ullage/internal/platform"
	"github.com/toldry/ullage/internal/recipes"
	"github.com/toldry/ullage/internal/scanner"
)

// Claim is one owner of Known items: a recipe, or one installed application.
type Claim struct {
	ID          int32
	Recipe      *recipes.Recipe
	Label       string
	Application *platform.InstalledApplication
}

type rule struct {
	tier    scanner.Tier
	claimID int32
}

// Classifier implements scanner.Classifier from recipes and System-tier rules.
type Classifier struct {
	patterns *pathpattern.Set
	rules    []rule
	Claims   []Claim
}

// New builds a classifier for one platform. Known rules are added before System rules,
// and recipes in priority order, so the first accepted rule wins.
func New(library *recipes.Library, locations platform.KnownLocations, platformName string,
	applications []platform.InstalledApplication) *Classifier {
	classifier := &Classifier{patterns: pathpattern.NewSet(locations.CaseInsensitive)}

	for _, recipe := range library.Recipes {
		if recipe.Dynamic == recipes.DynamicInstalledApplications {
			classifier.addApplications(recipe, applications, locations)
			continue
		}
		claimID := classifier.addClaim(Claim{Recipe: recipe, Label: recipe.Name})
		ruleIndex := classifier.addRule(rule{tier: scanner.TierKnown, claimID: claimID})
		for _, pattern := range recipe.Paths[platformName] {
			classifier.addPattern(pattern, ruleIndex, false, locations)
		}
	}

	systemRule := classifier.addRule(rule{tier: scanner.TierSystem, claimID: scanner.NoClaim})
	for _, pattern := range library.System.Paths[platformName] {
		classifier.addPattern(pattern, systemRule, false, locations)
	}
	return classifier
}

func (classifier *Classifier) addClaim(claim Claim) int32 {
	claim.ID = int32(len(classifier.Claims))
	classifier.Claims = append(classifier.Claims, claim)
	return claim.ID
}

func (classifier *Classifier) addRule(newRule rule) int32 {
	classifier.rules = append(classifier.rules, newRule)
	return int32(len(classifier.rules) - 1)
}

func (classifier *Classifier) addPattern(pattern string, ruleIndex int32, literal bool,
	locations platform.KnownLocations) {
	for _, expanded := range pathpattern.ExpandVariables(pattern, locations) {
		classifier.patterns.Add(expanded, literal, ruleIndex)
	}
}

func (classifier *Classifier) addApplications(recipe *recipes.Recipe,
	applications []platform.InstalledApplication, locations platform.KnownLocations) {
	guard := newInstallLocationGuard(locations)
	for index := range applications {
		application := &applications[index]
		if !guard.acceptable(application.InstallLocation) {
			continue
		}
		claimID := classifier.addClaim(Claim{Recipe: recipe, Label: application.DisplayName,
			Application: application})
		ruleIndex := classifier.addRule(rule{tier: scanner.TierKnown, claimID: claimID})
		classifier.addPattern(application.InstallLocation, ruleIndex, true, locations)
	}
}

// ClaimByID returns a claim, or nil for NoClaim.
func (classifier *Classifier) ClaimByID(claimID int32) *Claim {
	if claimID < 0 || int(claimID) >= len(classifier.Claims) {
		return nil
	}
	return &classifier.Claims[claimID]
}

// ClaimLabel implements scanner.Classifier.
func (classifier *Classifier) ClaimLabel(claimID int32) string {
	claim := classifier.ClaimByID(claimID)
	if claim == nil {
		return ""
	}
	if claim.Application != nil {
		return claim.Recipe.Name + " · " + claim.Label
	}
	return claim.Label
}

// Root implements scanner.Classifier. A root below C:\Windows starts out as System.
func (classifier *Classifier) Root(rootPath string) (scanner.Classification,
	scanner.ClassifierState) {
	classification := scanner.Classification{Tier: scanner.TierUnknown,
		ClaimID: scanner.NoClaim}
	var state scanner.ClassifierState = classifier.patterns.Start()
	for _, segment := range pathpattern.SplitPath(rootPath) {
		if state == nil {
			break
		}
		classification, state = classifier.Child(classification, state, segment,
			platform.EntryDirectory)
	}
	return classification, state
}

// Child implements scanner.Classifier.
func (classifier *Classifier) Child(parent scanner.Classification,
	state scanner.ClassifierState, name string, _ platform.EntryKind) (
	scanner.Classification, scanner.ClassifierState) {
	current, _ := state.(*pathpattern.State)
	if parent.Tier == scanner.TierKnown || current == nil {
		return parent, nil
	}
	next := classifier.patterns.Advance(current, name)
	if next == nil {
		return parent, nil
	}
	best := int32(-1)
	for _, ruleIndex := range classifier.patterns.Accepted(next) {
		if best == -1 || ruleIndex < best {
			best = ruleIndex
		}
	}
	if best == -1 {
		return parent, next
	}
	matched := classifier.rules[best]
	if matched.tier == scanner.TierKnown {
		// A Known claim covers the whole subtree, so nothing below needs matching.
		return scanner.Classification{Tier: scanner.TierKnown, ClaimID: matched.claimID}, nil
	}
	return scanner.Classification{Tier: matched.tier, ClaimID: scanner.NoClaim}, next
}

// installLocationGuard rejects install folders so broad that claiming them would hide
// unrelated data, for example an installer that registered C:\ or a whole user profile.
type installLocationGuard struct {
	caseInsensitive bool
	protected       [][]string
}

func newInstallLocationGuard(locations platform.KnownLocations) installLocationGuard {
	guard := installLocationGuard{caseInsensitive: locations.CaseInsensitive}
	for _, values := range locations.Variables {
		for _, value := range values {
			guard.addProtected(value)
		}
	}
	for _, profile := range locations.Variables["UserProfile"] {
		for _, below := range []string{"AppData", "AppData/Local", "AppData/Roaming",
			"AppData/Local/Programs", "Documents", "Desktop", "Downloads"} {
			guard.addProtected(profile + "/" + below)
		}
	}
	return guard
}

func (guard *installLocationGuard) addProtected(location string) {
	if segments := guard.normalize(location); len(segments) > 0 {
		guard.protected = append(guard.protected, segments)
	}
}

func (guard installLocationGuard) normalize(location string) []string {
	segments := pathpattern.SplitPath(location)
	if guard.caseInsensitive {
		for index := range segments {
			segments[index] = strings.ToLower(segments[index])
		}
	}
	return segments
}

func (guard installLocationGuard) acceptable(location string) bool {
	if strings.TrimSpace(location) == "" || strings.ContainsAny(location, "*?") {
		return false
	}
	candidate := guard.normalize(location)
	if len(candidate) < 2 {
		return false
	}
	for _, protected := range guard.protected {
		if isPrefix(candidate, protected) {
			return false
		}
	}
	return true
}

// isPrefix reports whether prefix equals or is an ancestor of path.
func isPrefix(prefix, path []string) bool {
	if len(prefix) > len(path) {
		return false
	}
	for index := range prefix {
		if prefix[index] != path[index] {
			return false
		}
	}
	return true
}
