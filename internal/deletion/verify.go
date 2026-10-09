package deletion

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/toldry/ullage/internal/platform"
	"github.com/toldry/ullage/internal/scanner"
)

// Fingerprint summarizes what is deleted. Logical sizes are compared, not allocated ones:
// a hard-linked file's allocated bytes are counted at whichever link the scan reached
// first, so they can differ between a whole-drive scan and a re-walk of one folder
// without anything having changed.
type Fingerprint struct {
	Files          int64 `json:"files"`
	Directories    int64 `json:"directories"`
	Links          int64 `json:"links"`
	LogicalBytes   int64 `json:"logicalBytes"`
	AllocatedBytes int64 `json:"allocatedBytes"`
	Newest         int64 `json:"newest"`
	Unreadable     bool  `json:"unreadable"`
}

// Check is the verdict on one target and, when ready, exactly what would be deleted.
type Check struct {
	Status Status      `json:"status"`
	Reason string      `json:"reason,omitempty"`
	Path   string      `json:"path"`
	Scan   Fingerprint `json:"scan"`
	Now    Fingerprint `json:"now"`
	// Paths are the entries that are deleted: the item itself, or for loose contents the
	// folder's small files and unlisted subfolders.
	Paths []string `json:"-"`
	// OnlyContentChanged: every check passed except that the contents differ from the
	// baseline, so Now may be accepted as a new baseline by a re-scan (decision D-072).
	OnlyContentChanged bool `json:"-"`
}

// lister reads each directory at most once per verification.
type lister struct {
	reader platform.DirectoryReader
	cache  map[string][]platform.DirectoryEntry
	errors map[string]error
}

func newLister(reader platform.DirectoryReader) *lister {
	return &lister{reader: reader, cache: map[string][]platform.DirectoryEntry{},
		errors: map[string]error{}}
}

func (lister *lister) list(path string) ([]platform.DirectoryEntry, error) {
	if entries, found := lister.cache[path]; found {
		return entries, lister.errors[path]
	}
	entries, err := lister.reader.ReadDirectory(path)
	lister.cache[path], lister.errors[path] = entries, err
	return entries, err
}

// entry finds a name in a directory. Names are matched exactly: the scan recorded them as
// the file system reported them.
func (lister *lister) entry(directory, name string) (platform.DirectoryEntry, bool, error) {
	entries, err := lister.list(directory)
	if err != nil {
		return platform.DirectoryEntry{}, false, err
	}
	for _, entry := range entries {
		if entry.Name == name {
			return entry, true, nil
		}
	}
	return platform.DirectoryEntry{}, false, nil
}

// Verify checks one target against the scan, the protected locations and the disk.
func (guard *Guard) Verify(target Target) Check {
	check := Check{Path: guard.Tree.Path(target.Node)}
	if target.Kind != KindLoose {
		if status, reason := guard.checkLocation(check.Path); status != StatusReady {
			check.Status, check.Reason = status, reason
			return check
		}
	}
	if status, reason := guard.checkScanTarget(target); status != StatusReady {
		check.Status, check.Reason = status, reason
		return check
	}
	disk := newLister(guard.Reader)
	if status, reason := guard.checkAncestors(disk, target.Node); status != StatusReady {
		check.Status, check.Reason = status, reason
		return check
	}
	check.Scan = guard.scanFingerprint(target)
	since := "the scan"
	if target.Baseline != nil {
		check.Scan, since = *target.Baseline, "the re-scan"
	}
	var status Status
	var reason string
	check.Now, check.Paths, status, reason = guard.diskFingerprint(disk, target)
	if status == StatusReady {
		for _, path := range check.Paths {
			if locationStatus, why := guard.checkLocation(path); locationStatus != StatusReady {
				status, reason = locationStatus, why
				break
			}
		}
	}
	if status == StatusReady {
		status, reason = compareFingerprints(target.Kind, check.Scan, check.Now, since)
		check.OnlyContentChanged = status == StatusChanged
	}
	check.Status, check.Reason = status, reason
	return check
}

func (guard *Guard) checkScanTarget(target Target) (Status, string) {
	if target.Kind == KindLoose {
		node := guard.Tree.Node(target.Node)
		if node.Kind != platform.EntryDirectory {
			return StatusProtected, "the item is not a folder"
		}
		for _, index := range guard.deletedNodes(target) {
			if status, reason := guard.checkNode(target, index); status != StatusReady {
				return status, reason
			}
		}
		return StatusReady, ""
	}
	return guard.checkScan(target)
}

// checkAncestors confirms that every folder between the scanned root and the item is
// still the folder the scan saw. A folder swapped for a junction or symbolic link could
// otherwise redirect the deletion somewhere else entirely.
func (guard *Guard) checkAncestors(disk *lister, index scanner.NodeIndex) (Status, string) {
	var chain []scanner.NodeIndex
	for current := guard.Tree.Node(index).Parent; current != scanner.NoNode; {
		node := guard.Tree.Node(current)
		if node.Parent == scanner.NoNode {
			break
		}
		chain = append([]scanner.NodeIndex{current}, chain...)
		current = node.Parent
	}
	for _, ancestor := range chain {
		node := guard.Tree.Node(ancestor)
		entry, found, err := disk.entry(guard.Tree.Path(node.Parent), node.Name)
		switch {
		case err != nil:
			return StatusChanged, "cannot read " + guard.Tree.Path(node.Parent) + ": " +
				err.Error()
		case !found:
			return StatusMissing, guard.Tree.Path(ancestor) + " no longer exists"
		case entry.Kind != platform.EntryDirectory:
			return StatusChanged, guard.Tree.Path(ancestor) + " is no longer a plain folder"
		case !sameIdentity(entry.ID, node.ID):
			return StatusChanged, guard.Tree.Path(ancestor) + " was replaced after the scan"
		}
	}
	return StatusReady, ""
}

func sameIdentity(now, scanned platform.FileID) bool {
	return now.IsZero() || scanned.IsZero() || now == scanned
}

// scanFingerprint summarizes the target as the scan recorded it.
func (guard *Guard) scanFingerprint(target Target) Fingerprint {
	var print Fingerprint
	if target.Kind == KindLoose {
		folder := guard.Tree.Node(target.Node)
		print.Files = int64(folder.SmallFileCount)
		print.LogicalBytes = folder.OwnLogicalBytes
		print.Newest = folder.SmallFilesModified
		for _, child := range guard.looseChildren(target) {
			print.add(guard.scanSubtree(child))
		}
		return print
	}
	print = guard.scanSubtree(target.Node)
	// Allocated size is compared for single files only, and not for a hard link whose
	// bytes the scan counted at another path.
	if node := guard.Tree.Node(target.Node); target.Kind == KindFile &&
		node.Flags&scanner.FlagHardLinkDuplicate == 0 {
		print.AllocatedBytes = node.OwnBytes
	}
	return print
}

func (guard *Guard) scanSubtree(root scanner.NodeIndex) Fingerprint {
	var print Fingerprint
	for _, index := range guard.subtree(root) {
		node := guard.Tree.Node(index)
		print.Newest = max(print.Newest, node.ModifiedUnixNanos)
		switch node.Kind {
		case platform.EntryDirectory:
			print.Directories++
			print.Files += int64(node.SmallFileCount)
			print.LogicalBytes += node.OwnLogicalBytes
			print.Newest = max(print.Newest, node.SmallFilesModified)
		case platform.EntryFile:
			print.Files++
			print.LogicalBytes += node.OwnLogicalBytes
		case platform.EntryLink:
			print.Links++
		}
	}
	return print
}

func (print *Fingerprint) add(other Fingerprint) {
	print.Files += other.Files
	print.Directories += other.Directories
	print.Links += other.Links
	print.LogicalBytes += other.LogicalBytes
	print.AllocatedBytes += other.AllocatedBytes
	print.Newest = max(print.Newest, other.Newest)
	print.Unreadable = print.Unreadable || other.Unreadable
}

// diskFingerprint re-reads the target and returns what it holds now and the entries a
// deletion would remove.
func (guard *Guard) diskFingerprint(disk *lister, target Target) (Fingerprint, []string,
	Status, string) {
	node := guard.Tree.Node(target.Node)
	path := guard.Tree.Path(target.Node)
	entry, found, err := disk.entry(guard.Tree.Path(node.Parent), node.Name)
	switch {
	case err != nil:
		return Fingerprint{}, nil, StatusChanged, "cannot read its folder: " + err.Error()
	case !found:
		return Fingerprint{}, nil, StatusMissing, "no longer exists"
	case entry.Kind != node.Kind:
		return Fingerprint{}, nil, StatusChanged, "was replaced by a " + kindName(entry.Kind)
	case !sameIdentity(entry.ID, node.ID):
		return Fingerprint{}, nil, StatusChanged, "was replaced by a different " +
			kindName(entry.Kind)
	}
	switch target.Kind {
	case KindFile:
		return fileFingerprint(entry), []string{path}, StatusReady, ""
	case KindFolder:
		return walk(disk, path, entry), []string{path}, StatusReady, ""
	}
	return guard.looseFingerprint(disk, target, path)
}

func fileFingerprint(entry platform.DirectoryEntry) Fingerprint {
	return Fingerprint{Files: 1, LogicalBytes: entry.LogicalBytes,
		AllocatedBytes: entry.AllocatedBytes, Newest: entry.ModifiedUnixNanos}
}

// walk re-reads a folder the way the scan did: links are counted, never followed.
func walk(disk *lister, path string, entry platform.DirectoryEntry) Fingerprint {
	print := Fingerprint{Directories: 1, Newest: entry.ModifiedUnixNanos}
	entries, err := disk.reader.ReadDirectory(path)
	if err != nil {
		print.Unreadable = true
		return print
	}
	for _, child := range entries {
		print.Newest = max(print.Newest, child.ModifiedUnixNanos)
		switch child.Kind {
		case platform.EntryFile:
			print.Files++
			print.LogicalBytes += child.LogicalBytes
		case platform.EntryLink:
			print.Links++
		case platform.EntryDirectory:
			print.add(walk(disk, filepath.Join(path, child.Name), child))
		}
	}
	return print
}

func (guard *Guard) looseFingerprint(disk *lister, target Target, path string) (Fingerprint,
	[]string, Status, string) {
	entries, err := disk.list(path)
	if err != nil {
		return Fingerprint{}, nil, StatusChanged, "cannot read the folder: " + err.Error()
	}
	retained := map[string]scanner.NodeIndex{}
	for _, child := range guard.Tree.Node(target.Node).Children {
		retained[guard.Tree.Node(child).Name] = child
	}
	loose := map[scanner.NodeIndex]bool{}
	for _, child := range guard.looseChildren(target) {
		loose[child] = true
	}
	var print Fingerprint
	var paths []string
	folderInSet := guard.Tree.Node(target.Node).Tier == target.Tier
	for _, entry := range entries {
		childPath := filepath.Join(path, entry.Name)
		index, known := retained[entry.Name]
		switch {
		case !known && entry.Kind == platform.EntryFile:
			if !folderInSet {
				return Fingerprint{}, nil, StatusChanged, "new files appeared in the folder"
			}
			print.Files++
			print.LogicalBytes += entry.LogicalBytes
			print.Newest = max(print.Newest, entry.ModifiedUnixNanos)
			paths = append(paths, childPath)
		case !known:
			return Fingerprint{}, nil, StatusChanged, entry.Name + " appeared after the scan"
		case loose[index]:
			node := guard.Tree.Node(index)
			if entry.Kind != node.Kind || !sameIdentity(entry.ID, node.ID) {
				return Fingerprint{}, nil, StatusChanged, entry.Name + " was replaced"
			}
			if entry.Kind == platform.EntryFile {
				print.add(fileFingerprint(entry))
			} else {
				print.add(walk(disk, childPath, entry))
			}
			paths = append(paths, childPath)
			delete(loose, index)
		}
	}
	if len(loose) > 0 {
		return print, nil, StatusChanged, "some of its contents no longer exist"
	}
	return print, paths, StatusReady, ""
}

// compareFingerprints implements safety rule 6: any difference blocks the item.
func compareFingerprints(kind Kind, scan, now Fingerprint, since string) (Status, string) {
	var changes []string
	if now.Unreadable {
		changes = append(changes, "part of it can no longer be read")
	}
	if delta := now.Files - scan.Files; delta != 0 {
		changes = append(changes, describeCount(delta, "file"))
	}
	if delta := now.Directories - scan.Directories; delta != 0 {
		changes = append(changes, describeCount(delta, "folder"))
	}
	if delta := now.Links - scan.Links; delta != 0 {
		changes = append(changes, describeCount(delta, "link"))
	}
	if now.LogicalBytes != scan.LogicalBytes {
		changes = append(changes, fmt.Sprintf("size changed by %+d bytes",
			now.LogicalBytes-scan.LogicalBytes))
	}
	if kind == KindFile && scan.AllocatedBytes > 0 && now.AllocatedBytes != scan.AllocatedBytes {
		changes = append(changes, "space on disk changed")
	}
	if now.Newest != scan.Newest {
		changes = append(changes, "modified "+time.Unix(0, now.Newest).Format(
			"2006-01-02 15:04")+", after "+since)
	}
	if len(changes) == 0 {
		return StatusReady, ""
	}
	return StatusChanged, strings.Join(changes, "; ")
}

func describeCount(delta int64, noun string) string {
	verb, count := "added", delta
	if delta < 0 {
		verb, count = "removed", -delta
	}
	if count != 1 {
		noun += "s"
	}
	return fmt.Sprintf("%d %s %s", count, noun, verb)
}

func kindName(kind platform.EntryKind) string {
	switch kind {
	case platform.EntryFile:
		return "file"
	case platform.EntryDirectory:
		return "folder"
	default:
		return "link"
	}
}
