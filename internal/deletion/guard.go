// Package deletion re-verifies and deletes scan items. It accepts only node indices that
// the session resolved from the current scan, never paths or text from a reply (safety
// rule 3). Every target is checked against the scan, the protected locations and the
// disk, right before it is deleted (safety rules 4 to 6).
package deletion

import (
	"path/filepath"
	"strings"

	"github.com/toldry/ullage/internal/platform"
	"github.com/toldry/ullage/internal/scanner"
)

// Kind is what a target stands for.
type Kind string

const (
	KindFile   Kind = "file"
	KindFolder Kind = "folder"
	// KindLoose is the contents of a folder not listed as their own items: its small
	// files and the unlisted subfolders (decision D-041).
	KindLoose Kind = "loose"
)

// Target is one selected item, resolved by the session from the current scan.
type Target struct {
	Key      string
	Category string
	Node     scanner.NodeIndex
	Kind     Kind
	// Listed are the children of a loose-contents folder that are separate items and are
	// therefore left alone.
	Listed []scanner.NodeIndex
	// Tier and ClaimID say what everything deleted must belong to: Unknown for AI
	// categories, Known with the recipe's claim for recipe categories.
	Tier    scanner.Tier
	ClaimID int32
	// Bytes is the reclaimable size the Results screen showed, for progress only.
	Bytes int64
	// Baseline, when set, replaces the scan as what the item must still match: the state
	// a re-scan recorded and the user confirmed after seeing it (decision D-072).
	Baseline *Fingerprint
}

// Status is the outcome of checking or deleting one target.
type Status string

const (
	StatusReady     Status = "ready"
	StatusChanged   Status = "changed"
	StatusMissing   Status = "missing"
	StatusProtected Status = "protected"
	StatusRecycled  Status = "recycled"
	StatusDeleted   Status = "deleted"
	StatusFailed    Status = "failed"
	StatusPartial   Status = "partial"
	StatusSkipped   Status = "skipped"
)

// Locations are the places the guard protects, as native paths.
type Locations struct {
	// Roots are the scanned roots. Targets must be strictly inside one of them.
	Roots []string
	// ProtectedTrees are never deleted from: the Windows folder and Program Files.
	ProtectedTrees []string
	// KeptPaths are never deleted, and neither is any folder containing one: user
	// profiles, the profiles folder, Ullage's own configuration.
	KeptPaths       []string
	CaseInsensitive bool
}

// Guard checks targets against a finished scan and the disk.
type Guard struct {
	Tree      *scanner.Tree
	Reader    platform.DirectoryReader
	Locations Locations
	// Canonical normalizes a path for comparisons (platform.CanonicalPath in production).
	Canonical func(string) string
}

func (guard *Guard) canonical(path string) string {
	if guard.Canonical != nil {
		path = guard.Canonical(path)
	}
	path = filepath.Clean(path)
	if guard.Locations.CaseInsensitive {
		path = strings.ToLower(path)
	}
	return path
}

// within reports whether path is location or below it.
func (guard *Guard) within(path, location string) bool {
	path, location = guard.canonical(path), guard.canonical(location)
	if path == location {
		return true
	}
	return strings.HasPrefix(path, strings.TrimSuffix(location, string(filepath.Separator))+
		string(filepath.Separator))
}

// checkLocation applies safety rule 4 to a path.
func (guard *Guard) checkLocation(path string) (Status, string) {
	for _, tree := range guard.Locations.ProtectedTrees {
		if tree != "" && guard.within(path, tree) {
			return StatusProtected, "inside " + tree + ", which Ullage never deletes from"
		}
	}
	for _, kept := range guard.Locations.KeptPaths {
		if kept != "" && guard.within(kept, path) {
			return StatusProtected, "contains " + kept + ", which Ullage never deletes"
		}
	}
	for _, root := range guard.Locations.Roots {
		if guard.within(path, root) && guard.canonical(path) != guard.canonical(root) {
			return StatusReady, ""
		}
	}
	return StatusProtected, "not inside a scanned folder"
}

// checkScan applies the checks that need only the scan: everything to be deleted belongs
// to the target's tier and claim (so never to the System tier), and every part of it was
// read completely and is stored on this PC.
func (guard *Guard) checkScan(target Target) (Status, string) {
	node := guard.Tree.Node(target.Node)
	if node.Parent == scanner.NoNode {
		return StatusProtected, "a scanned root is never deleted as a whole"
	}
	if node.Kind == platform.EntryLink {
		return StatusProtected, "links are never deleted"
	}
	switch target.Kind {
	case KindFile:
		if node.Kind != platform.EntryFile {
			return StatusProtected, "the item is not a file"
		}
	case KindFolder, KindLoose:
		if node.Kind != platform.EntryDirectory {
			return StatusProtected, "the item is not a folder"
		}
	}
	for _, index := range guard.deletedNodes(target) {
		if status, reason := guard.checkNode(target, index); status != StatusReady {
			return status, reason
		}
	}
	return StatusReady, ""
}

func (guard *Guard) checkNode(target Target, index scanner.NodeIndex) (Status, string) {
	node := guard.Tree.Node(index)
	switch {
	case node.Tier == scanner.TierSystem:
		return StatusProtected, "contains System files"
	case node.Tier != target.Tier ||
		(node.Tier == scanner.TierKnown && node.ClaimID != target.ClaimID):
		return StatusProtected, "contains items of another category"
	case node.Flags&(scanner.FlagAccessDenied|scanner.FlagReadError) != 0:
		return StatusProtected, "contains folders Ullage could not read, so it cannot verify them"
	case node.Flags&scanner.FlagExcluded != 0:
		return StatusProtected, "contains folders you excluded from the scan"
	}
	return StatusReady, ""
}

// deletedNodes lists every scan node the target would delete, in tree order. For loose
// contents that is the folder itself (its small files) and the unlisted children that
// belong entirely to the target; mixed children are left alone.
func (guard *Guard) deletedNodes(target Target) []scanner.NodeIndex {
	if target.Kind != KindLoose {
		return guard.subtree(target.Node)
	}
	nodes := []scanner.NodeIndex{}
	if node := guard.Tree.Node(target.Node); node.SmallFileCount > 0 {
		nodes = append(nodes, target.Node)
	}
	for _, child := range guard.looseChildren(target) {
		nodes = append(nodes, guard.subtree(child)...)
	}
	return nodes
}

// looseChildren are the unlisted children of a loose-contents folder whose whole subtree
// belongs to the target.
func (guard *Guard) looseChildren(target Target) []scanner.NodeIndex {
	listed := map[scanner.NodeIndex]bool{}
	for _, index := range target.Listed {
		listed[index] = true
	}
	var children []scanner.NodeIndex
	for _, child := range guard.Tree.Node(target.Node).Children {
		if listed[child] || guard.Tree.Node(child).Kind == platform.EntryLink {
			continue
		}
		pure := true
		for _, index := range guard.subtree(child) {
			if status, _ := guard.checkNode(target, index); status != StatusReady {
				pure = false
				break
			}
		}
		if pure {
			children = append(children, child)
		}
	}
	return children
}

func (guard *Guard) subtree(root scanner.NodeIndex) []scanner.NodeIndex {
	nodes := []scanner.NodeIndex{}
	stack := []scanner.NodeIndex{root}
	for len(stack) > 0 {
		index := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		nodes = append(nodes, index)
		stack = append(stack, guard.Tree.Node(index).Children...)
	}
	return nodes
}
