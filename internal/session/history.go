package session

import (
	"time"

	"github.com/toldry/ullage/internal/history"
)

// History never decides anything, so failing to write it is ignored: a read-only profile
// must not block scanning or deleting.

// volumeUsage names the drives of the scanned roots and adds up their used space, each
// drive once. Callers hold manager.lock.
func (manager *Manager) volumeUsage() ([]string, int64) {
	tree := manager.completed.Result.Tree
	seen := map[string]bool{}
	var drives []string
	var used int64
	for _, root := range tree.Roots {
		volume, err := manager.environment.VolumeFor(tree.Path(root))
		if err != nil || seen[volume.RootPath] {
			continue
		}
		seen[volume.RootPath] = true
		drives = append(drives, volume.DisplayName)
		used += volume.UsedBytes()
	}
	return drives, used
}

// recordScan starts the history entry of a finished scan. Callers hold manager.lock.
func (manager *Manager) recordScan() {
	result := manager.completed.Result
	drives, used := manager.volumeUsage()
	_ = manager.history.Record(manager.completed.Code, func(entry *history.Entry) {
		entry.Started, entry.Finished = result.Started, result.Finished
		entry.Drives, entry.UsedBefore = drives, used
	})
}

// recordImports notes how many batches have a reply. Callers hold manager.lock.
func (manager *Manager) recordImports() {
	if manager.completed == nil {
		return
	}
	count := len(manager.exportState.imports)
	_ = manager.history.Record(manager.completed.Code, func(entry *history.Entry) {
		entry.Batches = max(entry.Batches, count)
	})
}

// recordDeletion adds a finished run to the history. Callers hold manager.lock.
func (manager *Manager) recordDeletion(run DeletionRun) {
	if manager.completed == nil {
		return
	}
	_, used := manager.volumeUsage()
	_ = manager.history.Record(manager.completed.Code, func(entry *history.Entry) {
		entry.Items += run.Removed.Count
		entry.Failed += run.Failed.Count
		if run.Permanent {
			entry.Deleted += run.Removed.Bytes
		} else {
			entry.Recycled += run.Removed.Bytes
		}
		if run.Removed.Count > 0 {
			entry.UsedAfter = used
		}
	})
}

// HistoryView is the History screen's data.
type HistoryView struct {
	Entries []HistoryEntry `json:"entries"`
	// Current is the session code of this launch's scan, if any.
	Current   string    `json:"current"`
	Since     time.Time `json:"since,omitempty"`
	Reclaimed int64     `json:"reclaimed"`
	Recycled  int64     `json:"recycled"`
}

// HistoryEntry adds the derived total to a stored entry.
type HistoryEntry struct {
	history.Entry
	Reclaimed int64 `json:"reclaimed"`
}

// History returns the past sessions, newest first.
func (manager *Manager) History() HistoryView {
	manager.lock.Lock()
	defer manager.lock.Unlock()
	view := HistoryView{Entries: []HistoryEntry{}}
	if manager.completed != nil {
		view.Current = manager.completed.Code
	}
	for _, entry := range manager.history.Entries() {
		view.Entries = append(view.Entries, HistoryEntry{Entry: entry,
			Reclaimed: entry.Reclaimed()})
		view.Reclaimed += entry.Reclaimed()
		view.Recycled += entry.Recycled
		view.Since = entry.Started
	}
	return view
}

// ClearHistory deletes the history file.
func (manager *Manager) ClearHistory() (HistoryView, error) {
	if err := manager.history.Clear(); err != nil {
		return HistoryView{}, err
	}
	return manager.History(), nil
}
