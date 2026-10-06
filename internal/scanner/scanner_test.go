package scanner

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/toldry/ullage/internal/platform"
)

// fakeFileSystem is an in-memory DirectoryReader keyed by native path.
type fakeFileSystem struct {
	lock        sync.Mutex
	directories map[string][]platform.DirectoryEntry
	failures    map[string]error
	reads       int
	gate        chan struct{}
}

func newFakeFileSystem() *fakeFileSystem {
	return &fakeFileSystem{directories: map[string][]platform.DirectoryEntry{},
		failures: map[string]error{}}
}

func (fileSystem *fakeFileSystem) ReadDirectory(path string) ([]platform.DirectoryEntry,
	error) {
	if fileSystem.gate != nil {
		<-fileSystem.gate
	}
	fileSystem.lock.Lock()
	defer fileSystem.lock.Unlock()
	fileSystem.reads++
	if err, failed := fileSystem.failures[path]; failed {
		return nil, err
	}
	return append([]platform.DirectoryEntry(nil), fileSystem.directories[path]...), nil
}

func (fileSystem *fakeFileSystem) addDirectory(path string) {
	parent, name := filepath.Split(path)
	parent = filepath.Clean(parent)
	if _, exists := fileSystem.directories[path]; !exists {
		fileSystem.directories[path] = nil
	}
	if parent != path && name != "" {
		if _, exists := fileSystem.directories[parent]; !exists {
			fileSystem.addDirectory(parent)
		}
		fileSystem.directories[parent] = append(fileSystem.directories[parent],
			platform.DirectoryEntry{Name: name, Kind: platform.EntryDirectory})
	}
}

func (fileSystem *fakeFileSystem) addEntry(directory string, entry platform.DirectoryEntry) {
	if _, exists := fileSystem.directories[directory]; !exists {
		fileSystem.addDirectory(directory)
	}
	fileSystem.directories[directory] = append(fileSystem.directories[directory], entry)
}

func file(name string, allocated int64) platform.DirectoryEntry {
	return platform.DirectoryEntry{Name: name, Kind: platform.EntryFile,
		AllocatedBytes: allocated, LogicalBytes: allocated, LinkCount: 1}
}

func runScan(t *testing.T, options Options) *Result {
	t.Helper()
	result, err := Start(context.Background(), options).Wait()
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}
	return result
}

func findNode(t *testing.T, result *Result, path string) *Node {
	t.Helper()
	var found *Node
	result.Tree.Walk(func(index NodeIndex, node *Node) {
		if result.Tree.Path(index) == path {
			found = node
		}
	})
	if found == nil {
		t.Fatalf("node %s not found", path)
	}
	return found
}

func totalAcrossRoots(result *Result) int64 {
	var total int64
	for _, root := range result.Tree.Roots {
		total += result.Tree.Node(root).TotalBytes()
	}
	return total
}

func TestAllocatedSizeIsReportedNotApparentSize(t *testing.T) {
	fileSystem := newFakeFileSystem()
	root := filepath.Join("/", "r")
	compressed := file("compressed.bin", 4<<20)
	compressed.LogicalBytes = 40 << 20
	compressed.Attributes = platform.AttributeCompressed
	fileSystem.addEntry(root, compressed)
	fileSystem.addEntry(root, file("small.txt", 4096))

	result := runScan(t, Options{Roots: []string{root}, Reader: fileSystem})
	if got := totalAcrossRoots(result); got != 4<<20+4096 {
		t.Fatalf("total = %d, want allocated bytes %d", got, 4<<20+4096)
	}
	rootNode := findNode(t, result, root)
	if rootNode.TotalFiles != 2 || rootNode.SmallFileCount != 1 {
		t.Fatalf("files = %d small = %d", rootNode.TotalFiles, rootNode.SmallFileCount)
	}
}

func TestSmallFilesAreAggregatedAndLargeFilesRetained(t *testing.T) {
	fileSystem := newFakeFileSystem()
	directory := filepath.Join("/", "r", "cache")
	for index := 0; index < 100; index++ {
		fileSystem.addEntry(directory, file("f"+strings.Repeat("x", index), 1000))
	}
	fileSystem.addEntry(directory, file("large.bin", 5<<20))

	result := runScan(t, Options{Roots: []string{filepath.Join("/", "r")}, Reader: fileSystem})
	cache := findNode(t, result, directory)
	if len(cache.Children) != 1 || cache.SmallFileCount != 100 {
		t.Fatalf("children = %d small = %d", len(cache.Children), cache.SmallFileCount)
	}
	if cache.TotalBytes() != 100*1000+5<<20 {
		t.Fatalf("total = %d", cache.TotalBytes())
	}
}

func TestHardLinksAreCountedOnceWithDeterministicOwner(t *testing.T) {
	for attempt := 0; attempt < 20; attempt++ {
		fileSystem := newFakeFileSystem()
		root := filepath.Join("/", "r")
		shared := platform.FileID{Volume: 7, Low: 42}
		for _, directory := range []string{"b", "a", "c"} {
			linked := file("big.bin", 8<<20)
			linked.ID = shared
			linked.LinkCount = 3
			fileSystem.addEntry(filepath.Join(root, directory), linked)
			smallLinked := file("small.dat", 2000)
			smallLinked.ID = platform.FileID{Volume: 7, Low: 99}
			smallLinked.LinkCount = 0 // unknown, as on Windows
			fileSystem.addEntry(filepath.Join(root, directory), smallLinked)
		}
		result := runScan(t, Options{Roots: []string{root}, Reader: fileSystem, Workers: 8})
		if got := totalAcrossRoots(result); got != 8<<20+2000 {
			t.Fatalf("attempt %d: total = %d, want %d", attempt, got, 8<<20+2000)
		}
		owner := findNode(t, result, filepath.Join(root, "a"))
		if owner.TotalBytes() != 8<<20+2000 {
			t.Fatalf("attempt %d: owner a has %d bytes", attempt, owner.TotalBytes())
		}
		duplicate := findNode(t, result, filepath.Join(root, "c", "big.bin"))
		if duplicate.Flags&FlagHardLinkDuplicate == 0 || duplicate.OwnBytes != 0 {
			t.Fatalf("attempt %d: duplicate not flagged: %+v", attempt, duplicate)
		}
	}
}

func TestLinksAreRecordedButNeverTraversed(t *testing.T) {
	fileSystem := newFakeFileSystem()
	root := filepath.Join("/", "r")
	fileSystem.addEntry(root, platform.DirectoryEntry{Name: "junction",
		Kind: platform.EntryLink, LinkTarget: filepath.Join("/", "elsewhere")})
	// Content behind the link exists, but only a traversal would find it.
	fileSystem.directories[filepath.Join(root, "junction")] = []platform.DirectoryEntry{
		file("trap.bin", 9<<20)}

	result := runScan(t, Options{Roots: []string{root}, Reader: fileSystem})
	link := findNode(t, result, filepath.Join(root, "junction"))
	if link.Kind != platform.EntryLink || link.TotalBytes() != 0 || link.LinkTarget == "" {
		t.Fatalf("link node = %+v", link)
	}
	if totalAcrossRoots(result) != 0 || result.LinkCount != 1 {
		t.Fatalf("link was traversed: total %d links %d", totalAcrossRoots(result),
			result.LinkCount)
	}
}

func TestCloudPlaceholdersCountAsNotStoredLocally(t *testing.T) {
	fileSystem := newFakeFileSystem()
	root := filepath.Join("/", "r")
	placeholder := file("cloud.docx", 0)
	placeholder.LogicalBytes = 3 << 20
	placeholder.Attributes = platform.AttributeRecallOnDataAccess
	fileSystem.addEntry(root, placeholder)
	offline := file("offline.bin", 0)
	offline.LogicalBytes = 1000
	offline.Attributes = platform.AttributeOffline
	fileSystem.addEntry(root, offline)

	result := runScan(t, Options{Roots: []string{root}, Reader: fileSystem})
	if result.Placeholders != 2 || result.NotLocalBytes != 3<<20+1000 {
		t.Fatalf("placeholders = %d notLocal = %d", result.Placeholders, result.NotLocalBytes)
	}
	if totalAcrossRoots(result) != 0 {
		t.Fatalf("placeholders counted as local bytes")
	}
}

func TestAccessDeniedDirectoriesAreRecorded(t *testing.T) {
	fileSystem := newFakeFileSystem()
	root := filepath.Join("/", "r")
	secret := filepath.Join(root, "secret")
	fileSystem.addDirectory(secret)
	fileSystem.failures[secret] = &platform.AccessDeniedError{Path: secret,
		Cause: platform.ErrAccessDenied}

	result := runScan(t, Options{Roots: []string{root}, Reader: fileSystem})
	if result.DeniedCount != 1 || len(result.DeniedPaths) != 1 ||
		result.DeniedPaths[0] != secret {
		t.Fatalf("denied = %d %v", result.DeniedCount, result.DeniedPaths)
	}
	if findNode(t, result, secret).Flags&FlagAccessDenied == 0 {
		t.Fatalf("node not flagged")
	}
}

func TestExcludedDirectoriesAreNotRead(t *testing.T) {
	fileSystem := newFakeFileSystem()
	root := filepath.Join("/", "r")
	vault := filepath.Join(root, "vault")
	fileSystem.addEntry(vault, file("keys.kdbx", 2<<20))

	result := runScan(t, Options{Roots: []string{root}, Reader: fileSystem,
		Exclude: func(path string) bool { return path == vault }})
	if findNode(t, result, vault).Flags&FlagExcluded == 0 || totalAcrossRoots(result) != 0 {
		t.Fatalf("excluded directory was read")
	}
}

// pathClassifier claims directories by name, and marks one name as System.
type pathClassifier struct {
	claims map[string]int32
	system string
}

func (classifier pathClassifier) Root(string) (Classification, ClassifierState) {
	return Classification{Tier: TierUnknown, ClaimID: NoClaim}, true
}

func (classifier pathClassifier) Child(parent Classification, _ ClassifierState, name string,
	_ platform.EntryKind) (Classification, ClassifierState) {
	if parent.Tier == TierKnown {
		return parent, nil
	}
	if claim, ok := classifier.claims[name]; ok {
		return Classification{Tier: TierKnown, ClaimID: claim}, nil
	}
	if name == classifier.system {
		return Classification{Tier: TierSystem, ClaimID: NoClaim}, true
	}
	return parent, true
}

func (classifier pathClassifier) ClaimLabel(claimID int32) string { return "claim" }

func TestEveryByteLandsInExactlyOneTier(t *testing.T) {
	fileSystem := newFakeFileSystem()
	root := filepath.Join("/", "r")
	windowsDirectory := filepath.Join(root, "Windows")
	fileSystem.addEntry(windowsDirectory, file("kernel.dll", 3<<20))
	fileSystem.addEntry(windowsDirectory, file("small.ini", 500))
	fileSystem.addEntry(filepath.Join(windowsDirectory, "WinSxS"), file("pkg.cab", 7<<20))
	fileSystem.addEntry(filepath.Join(root, "npm-cache"), file("blob", 100))
	fileSystem.addEntry(filepath.Join(root, "projects"), file("data.bin", 2<<20))
	fileSystem.addEntry(root, file("pagefile.sys", 16<<20))

	classifier := pathClassifier{claims: map[string]int32{"WinSxS": 1, "npm-cache": 2,
		"pagefile.sys": 3}, system: "Windows"}
	result := runScan(t, Options{Roots: []string{root}, Reader: fileSystem,
		Classifier: classifier})

	rootNode := findNode(t, result, root)
	want := map[Tier]int64{TierSystem: 3<<20 + 500, TierKnown: 7<<20 + 100 + 16<<20,
		TierUnknown: 2 << 20}
	var sum int64
	for tier, bytes := range want {
		if rootNode.TierBytes(tier) != bytes {
			t.Errorf("%s = %d, want %d", tier, rootNode.TierBytes(tier), bytes)
		}
		sum += bytes
	}
	if rootNode.TotalBytes() != sum {
		t.Errorf("tiers do not partition the total: %d != %d", rootNode.TotalBytes(), sum)
	}
	if len(result.ClaimRoots) != 3 {
		t.Errorf("claim roots = %v", result.ClaimRoots)
	}
	pagefile := findNode(t, result, filepath.Join(root, "pagefile.sys"))
	if pagefile.ClaimID != 3 {
		t.Errorf("small claimed file was not retained as its own node")
	}
}

func TestPauseResumeAndCancel(t *testing.T) {
	fileSystem := newFakeFileSystem()
	root := filepath.Join("/", "r")
	for index := 0; index < 50; index++ {
		fileSystem.addEntry(filepath.Join(root, "d"+strings.Repeat("y", index)),
			file("f", 10))
	}
	fileSystem.gate = make(chan struct{})
	scan := Start(context.Background(), Options{Roots: []string{root}, Reader: fileSystem,
		Workers: 2})
	scan.Pause()
	close(fileSystem.gate)
	time.Sleep(50 * time.Millisecond)
	if !scan.Snapshot().Paused {
		t.Fatalf("not paused")
	}
	scan.Resume()
	if _, err := scan.Wait(); err != nil {
		t.Fatalf("wait: %v", err)
	}

	cancelContext, cancel := context.WithCancel(context.Background())
	fileSystem.gate = make(chan struct{})
	cancelled := Start(cancelContext, Options{Roots: []string{root}, Reader: fileSystem})
	cancel()
	close(fileSystem.gate)
	if _, err := cancelled.Wait(); err == nil {
		t.Fatalf("cancelled scan returned no error")
	}
}

func TestLargeSparseFilesAreRetainedButCountedAtAllocatedSize(t *testing.T) {
	fileSystem := newFakeFileSystem()
	root := filepath.Join("/", "r")
	sparse := file("disk.img", 4096)
	sparse.LogicalBytes = 1 << 30
	sparse.Attributes = platform.AttributeSparse
	fileSystem.addEntry(root, sparse)

	result := runScan(t, Options{Roots: []string{root}, Reader: fileSystem})
	node := findNode(t, result, filepath.Join(root, "disk.img"))
	if node.TotalBytes() != 4096 || node.OwnLogicalBytes != 1<<30 {
		t.Fatalf("sparse node = %+v", node)
	}
}
