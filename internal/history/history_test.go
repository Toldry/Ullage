package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/toldry/ullage/internal/protect"
)

func TestRecordUpdatesAndPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.dpapi")
	keys := protector(t)
	store := Open(path, keys)
	start := time.Date(2026, 9, 28, 9, 10, 0, 0, time.UTC)
	for index, code := range []string{"AAAAAA", "BBBBBB"} {
		if err := store.Record(code, func(entry *Entry) {
			entry.Started = start.Add(time.Duration(index) * time.Hour)
			entry.Drives = []string{"C:"}
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Record("AAAAAA", func(entry *Entry) {
		entry.Recycled += 10
		entry.Items += 2
	}); err != nil {
		t.Fatal(err)
	}
	entries := Open(path, keys).Entries()
	if len(entries) != 2 || entries[0].Code != "BBBBBB" || entries[1].Reclaimed() != 10 ||
		entries[1].Items != 2 {
		t.Fatalf("entries = %+v", entries)
	}
	if err := store.Clear(); err != nil || len(Open(path, keys).Entries()) != 0 {
		t.Fatalf("clear: %v", err)
	}
}

func TestUnreadableHistoryStartsOver(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.dpapi")
	keys := protector(t)
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := Open(path, keys)
	if len(store.Entries()) != 0 {
		t.Fatal("garbage read as entries")
	}
	if _, err := os.Stat(path + ".unreadable"); err != nil {
		t.Fatalf("unreadable file not kept aside: %v", err)
	}
	if err := store.Record("CCCCCC", func(*Entry) {}); err != nil {
		t.Fatal(err)
	}
}

// The file holds drive names and numbers; the type has no field that could hold a path.
func TestEntriesHoldNoPaths(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.dpapi")
	keys := protector(t)
	store := Open(path, keys)
	setDrive := func(entry *Entry) { entry.Drives = []string{"D:"} }
	if err := store.Record("DDDDDD", setDrive); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "D:") {
		t.Fatal("history is stored unencrypted")
	}
	content, _ := json.Marshal(store.Entries())
	for _, forbidden := range []string{`\\`, "/", "Users"} {
		if strings.Contains(string(content), forbidden) {
			t.Fatalf("history contains %q:\n%s", forbidden, content)
		}
	}
}

// protector returns the native protector with its key kept in a temporary folder.
func protector(t *testing.T) protect.Protector {
	return protect.Native(t.TempDir())
}
