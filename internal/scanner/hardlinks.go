package scanner

import (
	"sync"
	"sync/atomic"

	"github.com/toldry/ullage/internal/platform"
)

const hardLinkShardCount = 64

// linkOwner is the sighting of a file whose bytes are currently counted.
type linkOwner struct {
	directory NodeIndex
	// file is the retained node of the sighting, or NoNode for an aggregated small file.
	file  NodeIndex
	bytes int64
}

type hardLinkShard struct {
	lock   sync.Mutex
	owners map[platform.FileID]linkOwner
}

// hardLinkTable counts each file identity once per volume. Windows directory enumeration
// does not report link counts, so every file is tracked there; a file with one link can
// never be seen twice, so the totals equal "deduplicate when link count > 1".
type hardLinkTable struct {
	shards [hardLinkShardCount]hardLinkShard
}

func newHardLinkTable() *hardLinkTable {
	table := &hardLinkTable{}
	for index := range table.shards {
		table.shards[index].owners = map[platform.FileID]linkOwner{}
	}
	return table
}

func (table *hardLinkTable) shardFor(id platform.FileID) *hardLinkShard {
	hash := id.Low ^ id.High*0x9E3779B97F4A7C15 ^ id.Volume*0xC2B2AE3D27D4EB4F
	return &table.shards[hash%hardLinkShardCount]
}

func mayBeHardLinked(entry *platform.DirectoryEntry) bool {
	return !entry.ID.IsZero() && entry.LinkCount != 1
}

// claimHardLinkBytes decides whether this sighting owns the file's bytes. The owner is
// the sighting with the smallest path, so totals do not depend on the order in which
// parallel workers happen to reach the links. When a new sighting takes over, the bytes
// are removed from the previous owner and the caller adds them at the new one.
func (scan *Scan) claimHardLinkBytes(entry *platform.DirectoryEntry, directory NodeIndex,
	file NodeIndex) bool {
	if !mayBeHardLinked(entry) {
		return true
	}
	shard := scan.hardLinks.shardFor(entry.ID)
	shard.lock.Lock()
	defer shard.lock.Unlock()
	candidate := linkOwner{directory: directory, file: file, bytes: entry.AllocatedBytes}
	current, seen := shard.owners[entry.ID]
	if !seen {
		shard.owners[entry.ID] = candidate
		return true
	}
	if !scan.sightingPrecedes(candidate, current) {
		return false
	}
	scan.releaseBytes(current)
	shard.owners[entry.ID] = candidate
	return true
}

func (scan *Scan) sightingPrecedes(candidate, current linkOwner) bool {
	order := scan.tree.comparePaths(candidate.directory, current.directory)
	if order != 0 {
		return order < 0
	}
	if candidate.file != NoNode && current.file != NoNode {
		return scan.tree.comparePaths(candidate.file, current.file) < 0
	}
	return false
}

func (scan *Scan) releaseBytes(owner linkOwner) {
	holder := owner.directory
	if owner.file != NoNode {
		holder = owner.file
	}
	node := scan.tree.Node(holder)
	atomic.AddInt64(&node.OwnBytes, -owner.bytes)
	scan.allocated.Add(-owner.bytes)
	scan.tierBytes[node.Tier].Add(-owner.bytes)
}

// markHardLinkDuplicates flags retained file nodes whose bytes are counted elsewhere.
// It runs after the walk, when no worker can still change ownership.
func (scan *Scan) markHardLinkDuplicates() {
	owningFiles := map[NodeIndex]bool{}
	for index := range scan.hardLinks.shards {
		for _, owner := range scan.hardLinks.shards[index].owners {
			if owner.file != NoNode {
				owningFiles[owner.file] = true
			}
		}
	}
	scan.tree.Walk(func(index NodeIndex, node *Node) {
		if node.Kind == platform.EntryFile && !node.ID.IsZero() && node.OwnBytes == 0 &&
			node.OwnLogicalBytes > 0 && !owningFiles[index] {
			node.Flags |= FlagHardLinkDuplicate
		}
	})
}
