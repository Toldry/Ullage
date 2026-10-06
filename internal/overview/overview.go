// Package overview turns a finished scan into the numbers the Overview screen shows.
package overview

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/toldry/ullage/internal/platform"
	"github.com/toldry/ullage/internal/recipes"
	"github.com/toldry/ullage/internal/scanner"
	"github.com/toldry/ullage/internal/tiers"
)

// Item is one labelled block of the treemap.
type Item struct {
	Label string `json:"label"`
	Bytes int64  `json:"bytes"`
	// Path is the display path when the item is a single folder or file.
	Path string `json:"path,omitempty"`
}

// TierGroup is one tier's block in the treemap.
type TierGroup struct {
	Tier  string `json:"tier"`
	Bytes int64  `json:"bytes"`
	Items []Item `json:"items"`
}

// Root summarizes one scan root and its volume.
type Root struct {
	Path         string `json:"path"`
	Display      string `json:"display"`
	IsVolumeRoot bool   `json:"isVolumeRoot"`
	FileSystem   string `json:"fileSystem"`
	TotalBytes   int64  `json:"totalBytes"`
	FreeBytes    int64  `json:"freeBytes"`
	UsedBytes    int64  `json:"usedBytes"`
	ScannedBytes int64  `json:"scannedBytes"`
}

// Totals are the stacked bar on the Overview screen. Unaccounted is space the volume
// reports as used that the scan could not attribute: unreadable folders and file-system
// metadata. It is not a tier, because those bytes were never scanned.
type Totals struct {
	System      int64 `json:"system"`
	Known       int64 `json:"known"`
	Unknown     int64 `json:"unknown"`
	Unaccounted int64 `json:"unaccounted"`
	Free        int64 `json:"free"`
	Capacity    int64 `json:"capacity"`
	Used        int64 `json:"used"`
}

// KnownSummary describes the Known tier for the ledger.
type KnownSummary struct {
	RecipesMatched     int   `json:"recipesMatched"`
	CleanupCandidates  int64 `json:"cleanupCandidates"`
	InstalledAppsBytes int64 `json:"installedAppsBytes"`
	InstalledAppsCount int   `json:"installedAppsCount"`
}

// Model is the Overview screen's data.
type Model struct {
	ScannedAt       time.Time    `json:"scannedAt"`
	DurationSeconds float64      `json:"durationSeconds"`
	Files           int64        `json:"files"`
	Directories     int64        `json:"directories"`
	Roots           []Root       `json:"roots"`
	Elevated        bool         `json:"elevated"`
	Partial         bool         `json:"partial"`
	DeniedCount     int64        `json:"deniedCount"`
	DeniedSample    []string     `json:"deniedSample"`
	Totals          Totals       `json:"totals"`
	Groups          []TierGroup  `json:"groups"`
	Known           KnownSummary `json:"known"`
	UnknownFolders  int          `json:"unknownFolders"`
	Placeholders    int64        `json:"placeholders"`
	NotLocalBytes   int64        `json:"notLocalBytes"`
	Links           int64        `json:"links"`
}

// Input is everything Build needs.
type Input struct {
	Result         *scanner.Result
	Classifier     *tiers.Classifier
	Volumes        map[string]platform.Volume
	Elevated       bool
	CurrentProfile string
	Files          int64
	Directories    int64
}

const (
	maximumGroupItems = 9
	maximumExpansions = 40
	// A block is split into its children only while it dominates the tier, so the
	// treemap shows the level where the space actually diverges.
	dominantShare      = 0.6
	deniedSampleLength = 20
)

// Build computes the overview model.
func Build(input Input) Model {
	result := input.Result
	tree := result.Tree
	model := Model{
		ScannedAt:       result.Finished,
		DurationSeconds: result.Finished.Sub(result.Started).Seconds(),
		Files:           input.Files,
		Directories:     input.Directories,
		Elevated:        input.Elevated,
		Partial:         !input.Elevated,
		DeniedCount:     result.DeniedCount,
		Placeholders:    result.Placeholders,
		NotLocalBytes:   result.NotLocalBytes,
		Links:           result.LinkCount,
	}
	for index, path := range result.DeniedPaths {
		if index == deniedSampleLength {
			break
		}
		model.DeniedSample = append(model.DeniedSample, DisplayPath(path, input.CurrentProfile))
	}

	allVolumeRoots := true
	for _, rootIndex := range tree.Roots {
		root := buildRoot(tree, rootIndex, input)
		model.Roots = append(model.Roots, root)
		node := tree.Node(rootIndex)
		model.Totals.System += node.TierBytes(scanner.TierSystem)
		model.Totals.Known += node.TierBytes(scanner.TierKnown)
		model.Totals.Unknown += node.TierBytes(scanner.TierUnknown)
		if root.IsVolumeRoot {
			model.Totals.Unaccounted += max(0, root.UsedBytes-root.ScannedBytes)
			model.Totals.Free += root.FreeBytes
			model.Totals.Capacity += root.TotalBytes
		} else {
			allVolumeRoots = false
		}
	}
	scanned := model.Totals.System + model.Totals.Known + model.Totals.Unknown
	if !allVolumeRoots {
		// A folder scan has no meaningful "rest of the volume", so the bar shows only
		// what was scanned.
		model.Totals.Unaccounted, model.Totals.Free = 0, 0
		model.Totals.Capacity = scanned
	}
	model.Totals.Used = scanned + model.Totals.Unaccounted

	model.Groups = []TierGroup{
		{Tier: "unknown", Bytes: model.Totals.Unknown,
			Items: unknownItems(tree, input.CurrentProfile)},
		{Tier: "known", Bytes: model.Totals.Known,
			Items: knownItems(result, input.Classifier, &model.Known)},
		{Tier: "system", Bytes: model.Totals.System,
			Items: []Item{{Label: "not analyzed", Bytes: model.Totals.System}}},
	}
	if model.Totals.Unaccounted > 0 {
		label := "not in any scanned folder"
		if model.Partial {
			label = "unreadable or not in any scanned folder"
		}
		model.Groups = append(model.Groups, TierGroup{Tier: "unaccounted",
			Bytes: model.Totals.Unaccounted,
			Items: []Item{{Label: label, Bytes: model.Totals.Unaccounted}}})
	}
	model.UnknownFolders = countUnknownFolders(tree)
	return model
}

func buildRoot(tree *scanner.Tree, rootIndex scanner.NodeIndex, input Input) Root {
	node := tree.Node(rootIndex)
	root := Root{Path: node.Name, Display: DisplayPath(node.Name, input.CurrentProfile),
		ScannedBytes: node.TotalBytes()}
	if volume, ok := input.Volumes[node.Name]; ok {
		root.IsVolumeRoot = true
		root.FileSystem = volume.FileSystem
		root.TotalBytes = volume.TotalBytes
		root.FreeBytes = volume.FreeBytes
		root.UsedBytes = volume.UsedBytes()
		root.Display = volume.DisplayName
	}
	return root
}

// DisplayPath renders a native path with forward slashes and the current profile as ~.
func DisplayPath(nativePath, currentProfile string) string {
	display := strings.ReplaceAll(nativePath, `\`, "/")
	if currentProfile != "" {
		profile := strings.TrimRight(strings.ReplaceAll(currentProfile, `\`, "/"), "/")
		if len(display) >= len(profile) && strings.EqualFold(display[:len(profile)], profile) &&
			(len(display) == len(profile) || display[len(profile)] == '/') {
			display = "~" + display[len(profile):]
		}
	}
	return display
}

// candidate is a treemap block under construction: a node, or the files directly inside
// one (filesOnly), which cannot be split further.
type candidate struct {
	node      scanner.NodeIndex
	bytes     int64
	filesOnly bool
}

// unknownItems splits the Unknown tier into a handful of blocks by repeatedly replacing a
// dominant folder with its children. C:/ and C:/Users usually dissolve into the profile's
// folders, while a folder holding less than most of the tier stays one block.
func unknownItems(tree *scanner.Tree, currentProfile string) []Item {
	var candidates []candidate
	var total int64
	for _, root := range tree.Roots {
		if bytes := tree.Node(root).TierBytes(scanner.TierUnknown); bytes > 0 {
			candidates = append(candidates, candidate{node: root, bytes: bytes})
			total += bytes
		}
	}
	for expansion := 0; expansion < maximumExpansions; expansion++ {
		sortCandidates(candidates)
		largest := -1
		for index, current := range candidates {
			if index >= maximumGroupItems {
				break
			}
			dominant := float64(current.bytes) >= dominantShare*float64(total) ||
				len(candidates) < 3
			if dominant && !current.filesOnly && len(tree.Node(current.node).Children) > 0 &&
				tree.Node(current.node).Kind == platform.EntryDirectory {
				largest = index
				break
			}
		}
		if largest == -1 {
			break
		}
		expanded := expandCandidate(tree, candidates[largest])
		if len(candidates)-1+len(expanded) > 4*maximumGroupItems {
			break
		}
		candidates = append(append(candidates[:largest:largest], candidates[largest+1:]...),
			expanded...)
	}
	sortCandidates(candidates)

	items := make([]Item, 0, maximumGroupItems)
	var restBytes int64
	restCount := 0
	for index, current := range candidates {
		if index < maximumGroupItems-1 || len(candidates) == maximumGroupItems {
			path := DisplayPath(tree.Path(current.node), currentProfile)
			label := path
			if current.filesOnly {
				label = "files in " + path
			}
			items = append(items, Item{Label: label, Bytes: current.bytes, Path: path})
			continue
		}
		restBytes += current.bytes
		restCount++
	}
	if restCount > 0 {
		items = append(items, Item{Label: pluralize(restCount, "smaller item"),
			Bytes: restBytes})
	}
	return items
}

func expandCandidate(tree *scanner.Tree, parent candidate) []candidate {
	node := tree.Node(parent.node)
	var children []candidate
	childBytes := int64(0)
	for _, child := range node.Children {
		if bytes := tree.Node(child).TierBytes(scanner.TierUnknown); bytes > 0 {
			children = append(children, candidate{node: child, bytes: bytes})
			childBytes += bytes
		}
	}
	if own := parent.bytes - childBytes; own > 0 {
		children = append(children, candidate{node: parent.node, bytes: own, filesOnly: true})
	}
	return children
}

func sortCandidates(candidates []candidate) {
	sort.SliceStable(candidates, func(first, second int) bool {
		return candidates[first].bytes > candidates[second].bytes
	})
}

func knownItems(result *scanner.Result, classifier *tiers.Classifier,
	summary *KnownSummary) []Item {
	byLabel := map[string]int64{}
	recipesSeen := map[string]bool{}
	for claimID, roots := range result.ClaimRoots {
		claim := classifier.ClaimByID(claimID)
		if claim == nil {
			continue
		}
		var bytes int64
		for _, root := range roots {
			bytes += result.Tree.Node(root).TierBytes(scanner.TierKnown)
		}
		recipesSeen[claim.Recipe.ID] = true
		if claim.Recipe.Dynamic == recipes.DynamicInstalledApplications {
			summary.InstalledAppsBytes += bytes
			summary.InstalledAppsCount++
		} else {
			summary.CleanupCandidates += bytes
		}
		byLabel[claim.Recipe.Name] += bytes
	}
	summary.RecipesMatched = len(recipesSeen)

	items := make([]Item, 0, len(byLabel))
	for label, bytes := range byLabel {
		if bytes > 0 {
			items = append(items, Item{Label: label, Bytes: bytes})
		}
	}
	sort.Slice(items, func(first, second int) bool {
		if items[first].Bytes != items[second].Bytes {
			return items[first].Bytes > items[second].Bytes
		}
		return items[first].Label < items[second].Label
	})
	if len(items) > maximumGroupItems {
		var rest int64
		for _, item := range items[maximumGroupItems-1:] {
			rest += item.Bytes
		}
		count := len(items) - (maximumGroupItems - 1)
		items = append(items[:maximumGroupItems-1], Item{
			Label: pluralize(count, "smaller recipe"), Bytes: rest})
	}
	return items
}

// countUnknownFolders counts Unknown-tier folders that directly hold files, whether the
// files were aggregated into the folder or kept as their own nodes.
func countUnknownFolders(tree *scanner.Tree) int {
	holding := map[scanner.NodeIndex]bool{}
	tree.Walk(func(index scanner.NodeIndex, node *scanner.Node) {
		switch {
		case node.Kind == platform.EntryDirectory && node.Tier == scanner.TierUnknown &&
			node.SmallFileCount > 0:
			holding[index] = true
		case node.Kind == platform.EntryFile && node.Tier == scanner.TierUnknown &&
			node.Parent != scanner.NoNode:
			holding[node.Parent] = true
		}
	})
	return len(holding)
}

func pluralize(count int, noun string) string {
	if count == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(count) + " " + noun + "s"
}
