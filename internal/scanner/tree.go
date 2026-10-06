// Package scanner walks directory trees in parallel and builds an in-memory model of
// on-disk usage. It is portable: all OS access goes through platform.DirectoryReader.
package scanner

import (
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/toldry/ullage/internal/platform"
)

// NodeIndex addresses a node in a Tree. Indices are stable for the life of the tree.
type NodeIndex int32

// NoNode marks the absence of a node, for example the parent of a root.
const NoNode NodeIndex = -1

// Tier is the analysis tier every scanned byte belongs to.
type Tier uint8

const (
	TierUnknown Tier = iota
	TierKnown
	TierSystem
	tierCount
)

func (tier Tier) String() string {
	switch tier {
	case TierKnown:
		return "known"
	case TierSystem:
		return "system"
	default:
		return "unknown"
	}
}

// NodeFlags record conditions the UI and later phases need to know about.
type NodeFlags uint16

const (
	FlagRoot NodeFlags = 1 << iota
	FlagAccessDenied
	FlagReadError
	FlagExcluded
	// FlagHardLinkDuplicate marks a retained file whose bytes are counted at another link.
	FlagHardLinkDuplicate
)

// NoClaim is the ClaimID of nodes no recipe claimed.
const NoClaim int32 = -1

// Node is a directory, a link, or a file large enough (or interesting enough) to be kept
// individually. Smaller files are aggregated into their parent's counters, which keeps
// memory proportional to the number of directories rather than files.
type Node struct {
	Name       string
	Parent     NodeIndex
	Kind       platform.EntryKind
	Tier       Tier
	Flags      NodeFlags
	Attributes platform.EntryAttributes
	ClaimID    int32
	// OwnBytes is the allocated size of the file itself, or for a directory the allocated
	// size of the small files directly inside it. Updated atomically during a scan.
	OwnBytes          int64
	OwnLogicalBytes   int64
	SmallFileCount    int32
	NotLocalBytes     int64
	ModifiedUnixNanos int64
	CreatedUnixNanos  int64
	// Extensions are the most common file types directly inside a directory, by size.
	// They help the AI recognize what a folder holds without seeing file names.
	Extensions []ExtensionShare
	// SmallFilesModified is the latest write time among the aggregated small files.
	SmallFilesModified int64
	ID                 platform.FileID
	LinkTarget         string
	Children           []NodeIndex

	// Rollups, valid after the scan finishes.
	TierTotals     [tierCount]int64
	TotalFiles     int64
	LatestModified int64
}

// TotalBytes is the allocated size of the node's subtree across all tiers.
func (node *Node) TotalBytes() int64 {
	var total int64
	for _, bytes := range node.TierTotals {
		total += bytes
	}
	return total
}

// TierBytes returns the subtree bytes in one tier.
func (node *Node) TierBytes(tier Tier) int64 {
	return node.TierTotals[tier]
}

const (
	nodeChunkSize = 1 << 14
	maximumChunks = 1 << 17
)

type nodeChunk [nodeChunkSize]Node

// Tree stores nodes in fixed-size chunks so node pointers stay valid while workers append.
// The chunk table never moves, so readers need no lock.
type Tree struct {
	allocationLock sync.Mutex
	chunks         [maximumChunks]atomic.Pointer[nodeChunk]
	count          int
	Roots          []NodeIndex
}

func newTree() *Tree {
	return &Tree{}
}

// Len returns the number of nodes.
func (tree *Tree) Len() int {
	tree.allocationLock.Lock()
	defer tree.allocationLock.Unlock()
	return tree.count
}

// Node returns a pointer to the node at index.
func (tree *Tree) Node(index NodeIndex) *Node {
	return &tree.chunks[int(index)/nodeChunkSize].Load()[int(index)%nodeChunkSize]
}

// allocate reserves count nodes and returns their indices.
func (tree *Tree) allocate(count int) []NodeIndex {
	tree.allocationLock.Lock()
	defer tree.allocationLock.Unlock()
	indices := make([]NodeIndex, count)
	for position := range indices {
		if tree.count%nodeChunkSize == 0 {
			tree.chunks[tree.count/nodeChunkSize].Store(new(nodeChunk))
		}
		indices[position] = NodeIndex(tree.count)
		tree.count++
	}
	return indices
}

// Path returns the native path of a node.
func (tree *Tree) Path(index NodeIndex) string {
	components := tree.pathComponents(index)
	if len(components) == 1 {
		return components[0]
	}
	return filepath.Join(components...)
}

// pathComponents returns the root path followed by each name down to index.
func (tree *Tree) pathComponents(index NodeIndex) []string {
	var reversed []string
	for current := index; current != NoNode; current = tree.Node(current).Parent {
		reversed = append(reversed, tree.Node(current).Name)
	}
	components := make([]string, len(reversed))
	for position, name := range reversed {
		components[len(reversed)-1-position] = name
	}
	return components
}

// comparePaths orders two nodes by path, component by component, case-insensitively.
// It gives hard-linked files a deterministic owner regardless of walk order.
func (tree *Tree) comparePaths(first, second NodeIndex) int {
	firstComponents := tree.pathComponents(first)
	secondComponents := tree.pathComponents(second)
	shorter := min(len(firstComponents), len(secondComponents))
	for position := 0; position < shorter; position++ {
		left := strings.ToLower(firstComponents[position])
		right := strings.ToLower(secondComponents[position])
		if left != right {
			if left < right {
				return -1
			}
			return 1
		}
	}
	return len(firstComponents) - len(secondComponents)
}

// Walk visits every node in index order. Parents always precede their children.
func (tree *Tree) Walk(visit func(index NodeIndex, node *Node)) {
	count := tree.Len()
	for index := 0; index < count; index++ {
		visit(NodeIndex(index), tree.Node(NodeIndex(index)))
	}
}

// rollUp computes subtree totals. Children always have larger indices than their parent,
// so one reverse pass is a post-order traversal.
func (tree *Tree) rollUp() {
	count := tree.Len()
	for index := count - 1; index >= 0; index-- {
		node := tree.Node(NodeIndex(index))
		node.TierTotals[node.Tier] += node.OwnBytes
		node.TotalFiles += int64(node.SmallFileCount)
		if node.Kind == platform.EntryFile {
			node.TotalFiles++
		}
		node.LatestModified = max(node.LatestModified, node.ModifiedUnixNanos,
			node.SmallFilesModified)
		if node.Parent == NoNode {
			continue
		}
		parent := tree.Node(node.Parent)
		for tier := range node.TierTotals {
			parent.TierTotals[tier] += node.TierTotals[tier]
		}
		parent.TotalFiles += node.TotalFiles
		if node.LatestModified > parent.LatestModified {
			parent.LatestModified = node.LatestModified
		}
	}
}
