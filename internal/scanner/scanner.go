package scanner

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/toldry/ullage/internal/platform"
)

// DefaultRetainFileBytes is the size from which files are kept as individual nodes.
const DefaultRetainFileBytes = 1 << 20

// Classification is the tier of a node and, for Known items, the claim that owns it.
type Classification struct {
	Tier    Tier
	ClaimID int32
}

// ClassifierState is opaque per-directory state owned by a Classifier. A nil state means
// nothing below this directory can change classification, so the scanner skips the calls.
type ClassifierState any

// Classifier assigns tiers while the walk proceeds, so tier totals can stream live.
type Classifier interface {
	Root(rootPath string) (Classification, ClassifierState)
	Child(parent Classification, state ClassifierState, name string,
		kind platform.EntryKind) (Classification, ClassifierState)
	// ClaimLabel names a claim for the scan log.
	ClaimLabel(claimID int32) string
}

// Options configures one scan.
type Options struct {
	Roots      []string
	Reader     platform.DirectoryReader
	Classifier Classifier
	Workers    int
	// RetainFileBytes is the minimum size of a file kept as its own node.
	RetainFileBytes int64
	// Exclude returns true for directories the user asked never to scan.
	Exclude func(path string) bool
	// OnEvent receives scan log events. It must be safe for concurrent use.
	OnEvent func(Event)
}

// EventKind classifies scan log lines.
type EventKind string

const (
	EventStart    EventKind = "start"
	EventRecipe   EventKind = "recipe"
	EventSkip     EventKind = "skip"
	EventDenied   EventKind = "denied"
	EventFinished EventKind = "done"
)

// Event is one scan log line. Path is native and must only be shown in the local UI.
type Event struct {
	Time  time.Time
	Kind  EventKind
	Text  string
	Path  string
	Bytes int64
}

// Progress is a snapshot of a running scan.
type Progress struct {
	Files          int64
	Directories    int64
	AllocatedBytes int64
	TierBytes      [tierCount]int64
	DeniedCount    int64
	LinkCount      int64
	Placeholders   int64
	CurrentPath    string
	Elapsed        time.Duration
	Paused         bool
}

// Result is a finished scan.
type Result struct {
	Tree *Tree
	// ClaimRoots lists, for each claim, the nodes where the claim starts.
	ClaimRoots    map[int32][]NodeIndex
	DeniedPaths   []string
	DeniedCount   int64
	ErrorCount    int64
	LinkCount     int64
	Placeholders  int64
	NotLocalBytes int64
	Started       time.Time
	Finished      time.Time
}

const maximumListedDeniedPaths = 500
const maximumLinkEvents = 50

// Scan is a running or finished scan.
type Scan struct {
	options  Options
	tree     *Tree
	started  time.Time
	finished chan struct{}
	result   *Result
	err      error

	queueLock    sync.Mutex
	queueChanged *sync.Cond
	queue        []directoryJob
	pending      int
	paused       bool

	files, directories, allocated atomic.Int64
	denied, errorCount, links     atomic.Int64
	placeholders, notLocalBytes   atomic.Int64
	tierBytes                     [tierCount]atomic.Int64
	currentPath                   atomic.Pointer[string]
	deniedPathsLock               sync.Mutex
	deniedPaths                   []string
	claimRootsLock                sync.Mutex
	claimRoots                    map[int32][]NodeIndex
	reportedClaims                map[int32]bool
	hardLinks                     *hardLinkTable
	elapsedAtFinish               time.Duration
}

type directoryJob struct {
	node           NodeIndex
	path           string
	classification Classification
	state          ClassifierState
}

type unknownClassifier struct{}

func (unknownClassifier) Root(string) (Classification, ClassifierState) {
	return Classification{Tier: TierUnknown, ClaimID: NoClaim}, nil
}

func (unknownClassifier) Child(parent Classification, _ ClassifierState, _ string,
	_ platform.EntryKind) (Classification, ClassifierState) {
	return parent, nil
}

func (unknownClassifier) ClaimLabel(int32) string { return "" }

// Start begins scanning in the background.
func Start(ctx context.Context, options Options) *Scan {
	if options.Classifier == nil {
		options.Classifier = unknownClassifier{}
	}
	if options.Workers <= 0 {
		options.Workers = min(2*runtime.NumCPU(), 32)
	}
	if options.RetainFileBytes <= 0 {
		options.RetainFileBytes = DefaultRetainFileBytes
	}
	scan := &Scan{
		options:        options,
		tree:           newTree(),
		started:        time.Now(),
		finished:       make(chan struct{}),
		claimRoots:     map[int32][]NodeIndex{},
		reportedClaims: map[int32]bool{},
		hardLinks:      newHardLinkTable(),
	}
	scan.queueChanged = sync.NewCond(&scan.queueLock)
	go scan.run(ctx)
	return scan
}

// Wait blocks until the scan finishes and returns its result.
func (scan *Scan) Wait() (*Result, error) {
	<-scan.finished
	return scan.result, scan.err
}

// Done is closed when the scan finishes.
func (scan *Scan) Done() <-chan struct{} {
	return scan.finished
}

// Pause stops workers from starting new directories.
func (scan *Scan) Pause() {
	scan.queueLock.Lock()
	scan.paused = true
	scan.queueLock.Unlock()
}

// Resume undoes Pause.
func (scan *Scan) Resume() {
	scan.queueLock.Lock()
	scan.paused = false
	scan.queueLock.Unlock()
	scan.queueChanged.Broadcast()
}

// Snapshot returns current progress counters.
func (scan *Scan) Snapshot() Progress {
	progress := Progress{
		Files:          scan.files.Load(),
		Directories:    scan.directories.Load(),
		AllocatedBytes: scan.allocated.Load(),
		DeniedCount:    scan.denied.Load(),
		LinkCount:      scan.links.Load(),
		Placeholders:   scan.placeholders.Load(),
	}
	for tier := range progress.TierBytes {
		progress.TierBytes[tier] = scan.tierBytes[tier].Load()
	}
	if current := scan.currentPath.Load(); current != nil {
		progress.CurrentPath = *current
	}
	select {
	case <-scan.finished:
		progress.Elapsed = scan.elapsedAtFinish
	default:
		progress.Elapsed = time.Since(scan.started)
	}
	scan.queueLock.Lock()
	progress.Paused = scan.paused
	scan.queueLock.Unlock()
	return progress
}

func (scan *Scan) emit(kind EventKind, text, path string, bytes int64) {
	if scan.options.OnEvent != nil {
		scan.options.OnEvent(Event{Time: time.Now(), Kind: kind, Text: text, Path: path,
			Bytes: bytes})
	}
}

func (scan *Scan) run(ctx context.Context) {
	defer close(scan.finished)
	workerContext, stopWorkers := context.WithCancel(ctx)
	defer stopWorkers()
	go func() {
		<-workerContext.Done()
		scan.queueLock.Lock()
		scan.queueLock.Unlock()
		scan.queueChanged.Broadcast()
	}()

	for _, root := range scan.options.Roots {
		scan.addRoot(root)
	}

	var workers sync.WaitGroup
	for worker := 0; worker < scan.options.Workers; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			scan.work(workerContext)
		}()
	}
	workers.Wait()

	scan.elapsedAtFinish = time.Since(scan.started)
	if ctx.Err() != nil {
		scan.err = ctx.Err()
		return
	}
	scan.markHardLinkDuplicates()
	scan.tree.rollUp()
	scan.result = &Result{
		Tree:          scan.tree,
		ClaimRoots:    scan.claimRoots,
		DeniedPaths:   scan.deniedPaths,
		DeniedCount:   scan.denied.Load(),
		ErrorCount:    scan.errorCount.Load(),
		LinkCount:     scan.links.Load(),
		Placeholders:  scan.placeholders.Load(),
		NotLocalBytes: scan.notLocalBytes.Load(),
		Started:       scan.started,
		Finished:      time.Now(),
	}
	scan.reportClaimTotals()
	scan.emit(EventFinished, "scan finished", "", scan.allocated.Load())
}

func (scan *Scan) addRoot(rootPath string) {
	rootPath = filepath.Clean(rootPath)
	indices := scan.tree.allocate(1)
	node := scan.tree.Node(indices[0])
	node.Name = rootPath
	node.Parent = NoNode
	node.Kind = platform.EntryDirectory
	node.Flags = FlagRoot
	classification, state := scan.options.Classifier.Root(rootPath)
	node.Tier, node.ClaimID = classification.Tier, classification.ClaimID
	if info, err := platform.DescribeRoot(rootPath); err == nil {
		node.ID = info.ID
		node.ModifiedUnixNanos = info.ModifiedUnixNanos
	}
	scan.tree.Roots = append(scan.tree.Roots, indices[0])
	scan.recordClaim(indices[0], classification, NoClaim)
	scan.emit(EventStart, "scanning "+rootPath, rootPath, 0)
	scan.enqueue(directoryJob{node: indices[0], path: rootPath,
		classification: classification, state: state})
}

func (scan *Scan) enqueue(job directoryJob) {
	scan.queueLock.Lock()
	scan.queue = append(scan.queue, job)
	scan.pending++
	scan.queueLock.Unlock()
	scan.queueChanged.Signal()
}

// next blocks until a job is available, or returns false when the walk is complete or
// cancelled. A job counts as pending until finishJob, so idle workers do not exit while
// another worker may still discover subdirectories.
func (scan *Scan) next(ctx context.Context) (directoryJob, bool) {
	scan.queueLock.Lock()
	defer scan.queueLock.Unlock()
	for {
		if ctx.Err() != nil || scan.pending == 0 {
			return directoryJob{}, false
		}
		if !scan.paused && len(scan.queue) > 0 {
			// LIFO keeps the walk depth-first, which bounds queue memory.
			last := len(scan.queue) - 1
			job := scan.queue[last]
			scan.queue = scan.queue[:last]
			return job, true
		}
		scan.queueChanged.Wait()
	}
}

func (scan *Scan) finishJob() {
	scan.queueLock.Lock()
	scan.pending--
	done := scan.pending == 0
	scan.queueLock.Unlock()
	if done {
		scan.queueChanged.Broadcast()
	}
}

func (scan *Scan) work(ctx context.Context) {
	for {
		job, ok := scan.next(ctx)
		if !ok {
			return
		}
		scan.processDirectory(job)
		scan.finishJob()
	}
}

func (scan *Scan) processDirectory(job directoryJob) {
	scan.currentPath.Store(&job.path)
	scan.directories.Add(1)
	directoryNode := scan.tree.Node(job.node)

	entries, err := scan.options.Reader.ReadDirectory(job.path)
	if err != nil {
		scan.recordReadError(directoryNode, job.path, err)
		return
	}

	type retainedEntry struct {
		entry          *platform.DirectoryEntry
		classification Classification
		state          ClassifierState
	}
	var retained []retainedEntry
	for position := range entries {
		entry := &entries[position]
		classification, state := job.classification, ClassifierState(nil)
		if job.state != nil {
			classification, state = scan.options.Classifier.Child(job.classification,
				job.state, entry.Name, entry.Kind)
		}
		keep := entry.Kind != platform.EntryFile ||
			entry.AllocatedBytes >= scan.options.RetainFileBytes ||
			classification != job.classification
		if keep {
			retained = append(retained, retainedEntry{entry, classification, state})
			continue
		}
		scan.countSmallFile(job.node, directoryNode, job.classification.Tier, entry)
	}
	if len(retained) == 0 {
		return
	}

	indices := scan.tree.allocate(len(retained))
	directoryNode.Children = indices
	for position, item := range retained {
		index := indices[position]
		node := scan.tree.Node(index)
		entry := item.entry
		node.Name = entry.Name
		node.Parent = job.node
		node.Kind = entry.Kind
		node.Tier = item.classification.Tier
		node.ClaimID = item.classification.ClaimID
		node.Attributes = entry.Attributes
		node.ModifiedUnixNanos = entry.ModifiedUnixNanos
		node.ID = entry.ID
		node.LinkTarget = entry.LinkTarget
		scan.recordClaim(index, item.classification, job.classification.ClaimID)

		childPath := filepath.Join(job.path, entry.Name)
		switch entry.Kind {
		case platform.EntryLink:
			scan.recordLink(childPath, entry)
		case platform.EntryFile:
			scan.countRetainedFile(index, node, entry)
		case platform.EntryDirectory:
			if scan.options.Exclude != nil && scan.options.Exclude(childPath) {
				node.Flags |= FlagExcluded
				scan.emit(EventSkip, "excluded by your settings", childPath, 0)
				continue
			}
			scan.enqueue(directoryJob{node: index, path: childPath,
				classification: item.classification, state: item.state})
		}
	}
}

func (scan *Scan) recordReadError(node *Node, path string, err error) {
	if platform.IsAccessDenied(err) {
		node.Flags |= FlagAccessDenied
		scan.denied.Add(1)
		scan.deniedPathsLock.Lock()
		if len(scan.deniedPaths) < maximumListedDeniedPaths {
			scan.deniedPaths = append(scan.deniedPaths, path)
		}
		scan.deniedPathsLock.Unlock()
		scan.emit(EventDenied, "access denied", path, 0)
		return
	}
	if errors.Is(err, context.Canceled) {
		return
	}
	node.Flags |= FlagReadError
	scan.errorCount.Add(1)
}

func (scan *Scan) recordLink(path string, entry *platform.DirectoryEntry) {
	if scan.links.Add(1) <= maximumLinkEvents {
		text := "link not followed"
		if entry.LinkTarget != "" {
			text += " → " + entry.LinkTarget
		}
		scan.emit(EventSkip, text, path, 0)
	}
}

func (scan *Scan) recordClaim(index NodeIndex, classification Classification,
	parentClaim int32) {
	if classification.ClaimID == NoClaim || classification.ClaimID == parentClaim {
		return
	}
	scan.claimRootsLock.Lock()
	scan.claimRoots[classification.ClaimID] = append(scan.claimRoots[classification.ClaimID],
		index)
	firstTime := !scan.reportedClaims[classification.ClaimID]
	scan.reportedClaims[classification.ClaimID] = true
	scan.claimRootsLock.Unlock()
	if firstTime {
		scan.emit(EventRecipe, scan.options.Classifier.ClaimLabel(classification.ClaimID),
			"", 0)
	}
}

func (scan *Scan) reportClaimTotals() {
	for claimID, roots := range scan.claimRoots {
		var bytes int64
		for _, root := range roots {
			bytes += scan.tree.Node(root).TotalBytes()
		}
		scan.emit(EventRecipe, scan.options.Classifier.ClaimLabel(claimID), "", bytes)
	}
}

func (scan *Scan) countPlaceholder(entry *platform.DirectoryEntry) int64 {
	if !entry.Attributes.NotStoredLocally() {
		return 0
	}
	scan.placeholders.Add(1)
	scan.notLocalBytes.Add(entry.LogicalBytes)
	return entry.LogicalBytes
}

func (scan *Scan) countSmallFile(directoryIndex NodeIndex, directory *Node, tier Tier,
	entry *platform.DirectoryEntry) {
	scan.files.Add(1)
	directory.SmallFileCount++
	directory.OwnLogicalBytes += entry.LogicalBytes
	directory.NotLocalBytes += scan.countPlaceholder(entry)
	if entry.ModifiedUnixNanos > directory.SmallFilesModified {
		directory.SmallFilesModified = entry.ModifiedUnixNanos
	}
	if !scan.claimHardLinkBytes(entry, directoryIndex, NoNode) {
		return
	}
	atomic.AddInt64(&directory.OwnBytes, entry.AllocatedBytes)
	scan.allocated.Add(entry.AllocatedBytes)
	scan.tierBytes[tier].Add(entry.AllocatedBytes)
}

func (scan *Scan) countRetainedFile(index NodeIndex, node *Node,
	entry *platform.DirectoryEntry) {
	scan.files.Add(1)
	node.OwnLogicalBytes = entry.LogicalBytes
	node.NotLocalBytes = scan.countPlaceholder(entry)
	if !scan.claimHardLinkBytes(entry, node.Parent, index) {
		return
	}
	atomic.AddInt64(&node.OwnBytes, entry.AllocatedBytes)
	scan.allocated.Add(entry.AllocatedBytes)
	scan.tierBytes[node.Tier].Add(entry.AllocatedBytes)
}
