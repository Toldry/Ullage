// Package scannertest provides an in-memory file system for tests of packages that
// consume scan trees (redaction, prompts, deletion).
package scannertest

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/toldry/ullage/internal/platform"
	"github.com/toldry/ullage/internal/scanner"
)

// FileSystem is a platform.DirectoryReader backed by maps. Paths are slash-separated and
// converted with filepath.FromSlash, so tests read the same on every OS.
type FileSystem struct {
	lock        sync.Mutex
	directories map[string][]platform.DirectoryEntry
	nextID      uint64
}

// New returns an empty file system.
func New() *FileSystem {
	return &FileSystem{directories: map[string][]platform.DirectoryEntry{}}
}

// ReadDirectory implements platform.DirectoryReader.
func (fileSystem *FileSystem) ReadDirectory(path string) ([]platform.DirectoryEntry, error) {
	fileSystem.lock.Lock()
	defer fileSystem.lock.Unlock()
	return append([]platform.DirectoryEntry(nil), fileSystem.directories[path]...), nil
}

func (fileSystem *FileSystem) ensureDirectory(path string) {
	if _, exists := fileSystem.directories[path]; exists {
		return
	}
	fileSystem.directories[path] = nil
	parent := filepath.Dir(path)
	if parent == path {
		return
	}
	fileSystem.ensureDirectory(parent)
	fileSystem.nextID++
	fileSystem.directories[parent] = append(fileSystem.directories[parent],
		platform.DirectoryEntry{Name: filepath.Base(path), Kind: platform.EntryDirectory,
			ID: platform.FileID{Volume: 1, Low: fileSystem.nextID}})
}

// AddDirectory creates a directory and its ancestors.
func (fileSystem *FileSystem) AddDirectory(slashPath string) {
	fileSystem.ensureDirectory(filepath.FromSlash(slashPath))
}

// AddFile creates a file with the given allocated size and modification time.
func (fileSystem *FileSystem) AddFile(slashPath string, bytes int64, modifiedUnixNanos int64) {
	path := filepath.FromSlash(slashPath)
	parent := filepath.Dir(path)
	fileSystem.ensureDirectory(parent)
	fileSystem.nextID++
	fileSystem.directories[parent] = append(fileSystem.directories[parent],
		platform.DirectoryEntry{Name: filepath.Base(path), Kind: platform.EntryFile,
			AllocatedBytes: bytes, LogicalBytes: bytes, LinkCount: 1,
			ModifiedUnixNanos: modifiedUnixNanos, CreatedUnixNanos: modifiedUnixNanos,
			ID: platform.FileID{Volume: 1, Low: fileSystem.nextID}})
}

// Rename moves an entry, keeping its file ID, the way a rename within a volume does.
func (fileSystem *FileSystem) Rename(fromSlash, toSlash string) {
	from, to := filepath.FromSlash(fromSlash), filepath.FromSlash(toSlash)
	parent := filepath.Dir(from)
	for index, entry := range fileSystem.directories[parent] {
		if entry.Name != filepath.Base(from) {
			continue
		}
		fileSystem.directories[parent] = append(fileSystem.directories[parent][:index],
			fileSystem.directories[parent][index+1:]...)
		entry.Name = filepath.Base(to)
		fileSystem.ensureDirectory(filepath.Dir(to))
		fileSystem.directories[filepath.Dir(to)] = append(
			fileSystem.directories[filepath.Dir(to)], entry)
		for path, children := range fileSystem.directories {
			if path == from || len(path) > len(from) && path[:len(from)+1] == from+
				string(filepath.Separator) {
				delete(fileSystem.directories, path)
				fileSystem.directories[to+path[len(from):]] = children
			}
		}
		return
	}
}

// Scan scans the root with the given classifier (nil means everything is Unknown).
func (fileSystem *FileSystem) Scan(t testing.TB, rootSlash string,
	classifier scanner.Classifier) *scanner.Result {
	t.Helper()
	result, err := scanner.Start(context.Background(), scanner.Options{
		Roots: []string{filepath.FromSlash(rootSlash)}, Reader: fileSystem,
		Classifier: classifier}).Wait()
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// Find returns the node at a slash path, or fails the test.
func Find(t testing.TB, result *scanner.Result, slashPath string) scanner.NodeIndex {
	t.Helper()
	wanted := filepath.FromSlash(slashPath)
	found := scanner.NoNode
	result.Tree.Walk(func(index scanner.NodeIndex, _ *scanner.Node) {
		if result.Tree.Path(index) == wanted {
			found = index
		}
	})
	if found == scanner.NoNode {
		t.Fatalf("node %s not found", slashPath)
	}
	return found
}
