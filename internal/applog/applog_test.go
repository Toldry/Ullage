package applog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWritesDailyFilesAndPrunesOldOnes(t *testing.T) {
	directory := t.TempDir()
	old := filepath.Join(directory, "ullage-20000101.log")
	if err := os.WriteFile(old, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	logger := Open(directory)
	clock := time.Date(2026, 10, 7, 23, 59, 0, 0, time.Local)
	logger.now = func() time.Time { return clock }
	logger.Write("scan", "started\nwith two lines")
	clock = clock.Add(2 * time.Minute)
	logger.Write("scan", "finished")
	logger.Close()

	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("old log kept: %v", err)
	}
	first, _ := os.ReadFile(filepath.Join(directory, "ullage-20261007.log"))
	second, _ := os.ReadFile(filepath.Join(directory, "ullage-20261008.log"))
	if strings.Count(string(first), "\n") != 1 || !strings.Contains(string(first),
		"started with two lines") || !strings.Contains(string(second), "finished") {
		t.Fatalf("logs:\n%s\n%s", first, second)
	}
}

func TestSizeLimit(t *testing.T) {
	directory := t.TempDir()
	logger := Open(directory)
	line := strings.Repeat("x", 1<<16)
	for index := 0; index < 200; index++ {
		logger.Write("test", line)
	}
	logger.Close()
	entries, _ := os.ReadDir(directory)
	info, _ := entries[0].Info()
	if info.Size() > maximumFileBytes+(1<<17) {
		t.Fatalf("log grew to %d bytes", info.Size())
	}
}

func TestNilLoggerIsSafe(t *testing.T) {
	var logger *Logger
	logger.Write("x", "y")
	logger.Close()
}
