// Package history keeps a local record of past sessions: when, which drives, how much
// space was used and reclaimed. It stores drive names and numbers only, never paths, so
// it holds nothing that redaction would have to protect.
package history

import (
	"encoding/json"
	"errors"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/toldry/ullage/internal/storage"
)

// maximumEntries bounds the file; the oldest sessions are dropped first.
const maximumEntries = 500

// Entry is one session, identified by the scan's session code.
type Entry struct {
	Code     string    `json:"code"`
	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished"`
	// Drives are volume names such as "C:"; a folder scan records its drive.
	Drives     []string `json:"drives"`
	UsedBefore int64    `json:"usedBefore"`
	// UsedAfter is measured after the last deletion; zero when nothing was deleted.
	UsedAfter int64 `json:"usedAfter"`
	// Batches is how many prompt batches had a reply imported.
	Batches  int   `json:"batches"`
	Items    int   `json:"items"`
	Recycled int64 `json:"recycled"`
	Deleted  int64 `json:"deleted"`
	Failed   int   `json:"failed"`
}

// Reclaimed is what left the scanned folders, whether to the Recycle Bin or for good.
func (entry Entry) Reclaimed() int64 { return entry.Recycled + entry.Deleted }

// Store is the history file. It is safe for concurrent use.
type Store struct {
	path    string
	lock    sync.Mutex
	entries []Entry
}

// Open reads the history file. A missing file is an empty history; an unreadable one is
// moved aside and history starts over, since losing it costs nothing but the record.
func Open(path string) *Store {
	store := &Store{path: path}
	content, err := os.ReadFile(path)
	if err != nil {
		return store
	}
	if err := json.Unmarshal(content, &store.entries); err != nil {
		store.entries = nil
		_ = os.Rename(path, path+".unreadable")
	}
	return store
}

// Record creates or updates the entry for a session and saves the file.
func (store *Store) Record(code string, update func(*Entry)) error {
	store.lock.Lock()
	defer store.lock.Unlock()
	index := -1
	for position := range store.entries {
		if store.entries[position].Code == code {
			index = position
		}
	}
	if index < 0 {
		store.entries = append(store.entries, Entry{Code: code})
		index = len(store.entries) - 1
	}
	update(&store.entries[index])
	sort.SliceStable(store.entries, func(first, second int) bool {
		return store.entries[first].Started.Before(store.entries[second].Started)
	})
	if len(store.entries) > maximumEntries {
		store.entries = store.entries[len(store.entries)-maximumEntries:]
	}
	return store.save()
}

// Entries returns the sessions, newest first.
func (store *Store) Entries() []Entry {
	store.lock.Lock()
	defer store.lock.Unlock()
	entries := make([]Entry, len(store.entries))
	for index, entry := range store.entries {
		entries[len(entries)-1-index] = entry
	}
	return entries
}

// Clear deletes every entry.
func (store *Store) Clear() error {
	store.lock.Lock()
	defer store.lock.Unlock()
	store.entries = nil
	if err := os.Remove(store.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (store *Store) save() error {
	content, err := json.MarshalIndent(store.entries, "", "  ")
	if err != nil {
		return err
	}
	return storage.WriteFileAtomically(store.path, content)
}
