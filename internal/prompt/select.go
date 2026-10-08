// Package prompt turns the Unknown tier of a scan into the token-minimized, redacted text
// the user copies into a chat assistant. It never sees real names except through the
// redactor, and it never decides what to delete: it only assigns IDs.
package prompt

import (
	"regexp"
	"strings"

	"github.com/toldry/ullage/internal/platform"
	"github.com/toldry/ullage/internal/redact"
	"github.com/toldry/ullage/internal/scanner"
)

// ItemKind says what an ID stands for when it is later acted on.
type ItemKind uint8

const (
	// ItemFile is one file.
	ItemFile ItemKind = iota
	// ItemFolder is a folder and everything inside it; all of it is Unknown tier.
	ItemFolder
	// ItemLooseContents is the contents of a folder that are not listed as their own
	// items: small files directly inside it and small subfolders. Shown as "folder/*".
	ItemLooseContents
)

// Item is one ID in the export.
type Item struct {
	ID    int
	Kind  ItemKind
	Node  scanner.NodeIndex
	Bytes int64
	Files int64
	// Listed are the children of a loose-contents folder that have their own items, so a
	// later deletion of the loose contents leaves them alone.
	Listed []scanner.NodeIndex
	// metadata is shown only when the item's own name is shown (decision D-030).
	nameShown bool
	modified  int64
	created   int64
	types     []scanner.ExtensionShare
}

// Options tune selection and batching.
type Options struct {
	// MinimumItemBytes is the smallest Unknown size worth a line.
	MinimumItemBytes int64
	// MaximumItems bounds how far large folders are split into finer items.
	MaximumItems int
	// BatchLines is the data-line budget per batch. Replies carry an explanation under
	// every deletable category, so a smaller batch keeps each reply short enough that
	// chat assistants do not cut it off (decision D-069).
	BatchLines int
	// CollapseMinimum is how many same-named folders become one "xN" line.
	CollapseMinimum int
	// ManyFilesThreshold: a folder whose large children are all files and more than this
	// many is listed as one line (an archive of videos rather than 300 lines).
	ManyFilesThreshold int
}

// DefaultOptions are used when the user has not changed them.
var DefaultOptions = Options{
	MinimumItemBytes:   100 << 20,
	MaximumItems:       900,
	BatchLines:         150,
	CollapseMinimum:    3,
	ManyFilesThreshold: 20,
}

// homogeneousShare is the share of one file type above which a folder counts as one
// kind of content (a photo library, a folder of downloaded archives).
const homogeneousShare = 0.8

// genericName matches names that say nothing about content: numbers and dates such as
// 2019, 01 or 2019-05-12. Splitting a photo library into such folders adds lines but no
// meaning, and grouping them ("01 x11") would mislead.
var genericName = regexp.MustCompile(`^[\d\s._-]+$`)

// selfDescribingNames are folders whose name already says what they hold; splitting them
// would spend lines on their internals (packages, objects, cache entries) for nothing.
var selfDescribingNames = map[string]bool{
	"node_modules": true, ".git": true, ".svn": true, ".hg": true, ".venv": true,
	"venv": true, "__pycache__": true, ".tox": true, ".gradle": true, ".next": true,
	".nuxt": true, ".terraform": true, "target": true, "build": true, "dist": true,
	"out": true, "obj": true, "bin": true, "deriveddata": true, "pods": true, ".cache": true,
	"cache": true, "caches": true, "cache2": true, "temp": true, "tmp": true, "logs": true,
	"log": true, "crashdumps": true, "thumbnails": true, "proxies": true,
	"render files": true, "renders": true, "media cache files": true, "shadercache": true,
}

// candidate is an item under construction.
type candidate struct {
	node         scanner.NodeIndex
	kind         ItemKind
	bytes        int64
	listed       []scanner.NodeIndex
	insideHidden bool
}

type selector struct {
	tree     *scanner.Tree
	redactor *redact.Redactor
	options  Options
}

// selectItems decides which lines represent the Unknown tier. First every folder that
// also holds Known or System bytes is split, so no item ever covers them. Then the
// largest remaining folders are split into their large children, until the item budget
// is used or nothing is worth splitting. Large things get detail; small ones stay whole.
func selectItems(tree *scanner.Tree, redactor *redact.Redactor, options Options) ([]Item,
	int64) {
	selection := &selector{tree: tree, redactor: redactor, options: options}
	var candidates []candidate
	for _, root := range tree.Roots {
		candidates = append(candidates, selection.forced(root, false)...)
	}
	candidates = selection.refine(candidates)

	items := make([]Item, 0, len(candidates))
	var represented int64
	for _, current := range candidates {
		items = append(items, selection.toItem(current))
		represented += current.bytes
	}
	return items, represented
}

// forced returns the candidates for a node, splitting it only if it is not purely
// Unknown.
func (selection *selector) forced(index scanner.NodeIndex, insideHidden bool) []candidate {
	node := selection.tree.Node(index)
	unknown := selection.unknown(index)
	if unknown < selection.options.MinimumItemBytes || node.Kind == platform.EntryLink {
		return nil
	}
	if node.Kind == platform.EntryFile {
		return []candidate{{node: index, kind: ItemFile, bytes: unknown}}
	}
	hidden := insideHidden || selection.hidesSubtree(index)
	if unknown == node.TotalBytes() {
		return []candidate{{node: index, kind: ItemFolder, bytes: unknown,
			insideHidden: hidden}}
	}
	return selection.split(index, hidden)
}

// split replaces a folder by its large children and, if what is left is still large, a
// loose-contents item for the rest.
func (selection *selector) split(index scanner.NodeIndex, hidden bool) []candidate {
	var result []candidate
	var listed []scanner.NodeIndex
	var listedBytes int64
	for _, child := range selection.significantChildren(index, hidden) {
		listed = append(listed, child)
		listedBytes += selection.unknown(child)
		result = append(result, selection.forced(child, hidden)...)
	}
	if rest := selection.unknown(index) - listedBytes; rest >= selection.options.MinimumItemBytes {
		result = append(result, candidate{node: index, kind: ItemLooseContents, bytes: rest,
			listed: listed, insideHidden: hidden})
	}
	return result
}

// refine splits the largest splittable folders while the item budget allows.
func (selection *selector) refine(candidates []candidate) []candidate {
	for {
		best := -1
		for position, current := range candidates {
			if selection.splittable(current) &&
				(best == -1 || current.bytes > candidates[best].bytes) {
				best = position
			}
		}
		if best == -1 {
			return candidates
		}
		replacement := selection.split(candidates[best].node, candidates[best].insideHidden)
		if len(candidates)-1+len(replacement) > selection.options.MaximumItems ||
			len(replacement) == 0 {
			candidates[best].kind = ItemFolder
			candidates[best].listed = []scanner.NodeIndex{scanner.NoNode}
			continue
		}
		candidates = append(append(candidates[:best:best], replacement...),
			candidates[best+1:]...)
	}
}

// splittable reports whether splitting a folder item could add useful detail. A listed
// entry of NoNode marks a folder that was refused for lack of budget.
func (selection *selector) splittable(current candidate) bool {
	if current.kind != ItemFolder || len(current.listed) > 0 {
		return false
	}
	node := selection.tree.Node(current.node)
	if selfDescribingNames[strings.ToLower(node.Name)] {
		return false
	}
	children := selection.significantChildren(current.node, current.insideHidden)
	if len(children) == 0 {
		return false
	}
	if !selection.isHomogeneous(current.node) {
		return true
	}
	// One kind of content: split only if the children's names say something.
	allFiles, allGeneric := true, true
	for _, child := range children {
		childNode := selection.tree.Node(child)
		allFiles = allFiles && childNode.Kind == platform.EntryFile
		allGeneric = allGeneric && genericName.MatchString(childNode.Name)
	}
	manyFiles := allFiles && len(children) > selection.options.ManyFilesThreshold
	return !manyFiles && !allGeneric
}

// isHomogeneous reports whether one file type makes up most of the folder's bytes.
func (selection *selector) isHomogeneous(index scanner.NodeIndex) bool {
	shares := scanner.MergeExtensions(selection.subtreeExtensions(index), 1<<10)
	var total int64
	for _, share := range shares {
		total += share.Bytes
	}
	return total > 0 && float64(shares[0].Bytes) >= homogeneousShare*float64(total)
}

func (selection *selector) toItem(current candidate) Item {
	node := selection.tree.Node(current.node)
	files := node.TotalFiles
	listed := current.listed
	if current.kind == ItemLooseContents {
		files = selection.looseFileCount(current.node, listed)
	} else {
		listed = nil
	}
	if current.kind == ItemFile {
		files = 1
	}
	item := Item{Kind: current.kind, Node: current.node, Bytes: current.bytes, Files: files,
		Listed: listed, nameShown: selection.redactor.NameShown(current.node)}
	if item.nameShown {
		item.modified, item.created, item.types = selection.metadata(current.node,
			current.kind, listed)
	}
	return item
}

func (selection *selector) unknown(index scanner.NodeIndex) int64 {
	return selection.tree.Node(index).TierBytes(scanner.TierUnknown)
}

func (selection *selector) hidesSubtree(index scanner.NodeIndex) bool {
	state := selection.redactor.State(index)
	return state.Decision == redact.DecisionRedacted &&
		selection.tree.Node(index).Kind == platform.EntryDirectory
}

func (selection *selector) significantChildren(index scanner.NodeIndex,
	hidesBelow bool) []scanner.NodeIndex {
	var significant []scanner.NodeIndex
	for _, child := range selection.tree.Node(index).Children {
		if selection.unknown(child) < selection.options.MinimumItemBytes {
			continue
		}
		if hidesBelow && !selection.redactor.NameShown(child) {
			continue
		}
		significant = append(significant, child)
	}
	return significant
}

func (selection *selector) looseFileCount(index scanner.NodeIndex,
	listed []scanner.NodeIndex) int64 {
	isListed := map[scanner.NodeIndex]bool{}
	for _, child := range listed {
		isListed[child] = true
	}
	node := selection.tree.Node(index)
	count := int64(node.SmallFileCount)
	for _, child := range node.Children {
		if !isListed[child] && selection.unknown(child) > 0 {
			count += selection.tree.Node(child).TotalFiles
		}
	}
	return count
}

// metadata returns the newest modification, the creation time and the main file types.
func (selection *selector) metadata(index scanner.NodeIndex, kind ItemKind,
	listed []scanner.NodeIndex) (int64, int64, []scanner.ExtensionShare) {
	node := selection.tree.Node(index)
	switch kind {
	case ItemFile:
		return node.ModifiedUnixNanos, node.CreatedUnixNanos, nil
	case ItemLooseContents:
		isListed := map[scanner.NodeIndex]bool{}
		for _, child := range listed {
			isListed[child] = true
		}
		newest := node.SmallFilesModified
		summaries := [][]scanner.ExtensionShare{node.Extensions}
		for _, child := range node.Children {
			if isListed[child] || selection.unknown(child) == 0 {
				continue
			}
			newest = max(newest, selection.tree.Node(child).LatestModified)
			// Files directly inside are already in node.Extensions.
			if selection.tree.Node(child).Kind != platform.EntryFile {
				summaries = append(summaries, selection.subtreeExtensions(child)...)
			}
		}
		return newest, 0, scanner.MergeExtensions(summaries, 3)
	default:
		var summaries [][]scanner.ExtensionShare
		summaries = append(summaries, selection.subtreeExtensions(index)...)
		return node.LatestModified, node.CreatedUnixNanos, scanner.MergeExtensions(summaries, 3)
	}
}

func (selection *selector) subtreeExtensions(
	index scanner.NodeIndex) [][]scanner.ExtensionShare {
	node := selection.tree.Node(index)
	if node.Kind == platform.EntryFile {
		if extension := scanner.NormalizeExtension(node.Name); extension != "" {
			bytes := max(node.OwnBytes, node.OwnLogicalBytes)
			return [][]scanner.ExtensionShare{{{Extension: extension, Bytes: bytes}}}
		}
		return nil
	}
	summaries := [][]scanner.ExtensionShare{node.Extensions}
	for _, child := range node.Children {
		if selection.tree.Node(child).Kind != platform.EntryFile {
			summaries = append(summaries, selection.subtreeExtensions(child)...)
		}
	}
	return summaries
}
