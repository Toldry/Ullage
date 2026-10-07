// Package applog writes Ullage's log files: one file per day, kept for two weeks. It
// only writes what it is given; callers make every line safe first (see the session's
// safePath and safeText), so no unredacted path of a redacted item reaches the disk
// (safety rule 8).
package applog

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	retentionDays = 14
	// maximumFileBytes stops a runaway day from filling the disk Ullage is meant to free.
	maximumFileBytes = 10 << 20
	filePrefix       = "ullage-"
	fileSuffix       = ".log"
)

// Logger appends lines to the current day's file. A nil Logger discards everything, and
// failing to write never affects the app.
type Logger struct {
	directory string
	now       func() time.Time
	lock      sync.Mutex
	file      *os.File
	day       string
	written   int64
}

// Open prepares the log directory and removes files older than the retention period.
func Open(directory string) *Logger {
	logger := &Logger{directory: directory, now: time.Now}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil
	}
	logger.prune()
	return logger
}

// Write appends one line: time, event and text. Newlines in text are flattened so a line
// is always one entry.
func (logger *Logger) Write(event, text string) {
	if logger == nil {
		return
	}
	logger.lock.Lock()
	defer logger.lock.Unlock()
	now := logger.now()
	if !logger.ensureFile(now) || logger.written >= maximumFileBytes {
		return
	}
	text = strings.NewReplacer("\r", " ", "\n", " ").Replace(text)
	line := fmt.Sprintf("%s %-9s %s\n", now.UTC().Format("2006-01-02T15:04:05Z"), event, text)
	if logger.written+int64(len(line)) >= maximumFileBytes {
		line = now.UTC().Format("2006-01-02T15:04:05Z") +
			" log       size limit reached; nothing more is logged today\n"
	}
	count, _ := logger.file.WriteString(line)
	logger.written += int64(count)
}

func (logger *Logger) ensureFile(now time.Time) bool {
	day := now.Format("20060102")
	if logger.file != nil && logger.day == day {
		return true
	}
	if logger.file != nil {
		_ = logger.file.Close()
		logger.file = nil
	}
	path := filepath.Join(logger.directory, filePrefix+day+fileSuffix)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return false
	}
	info, _ := file.Stat()
	logger.file, logger.day, logger.written = file, day, 0
	if info != nil {
		logger.written = info.Size()
	}
	return true
}

// prune deletes log files older than the retention period.
func (logger *Logger) prune() {
	entries, err := os.ReadDir(logger.directory)
	if err != nil {
		return
	}
	cutoff := logger.now().AddDate(0, 0, -retentionDays).Format("20060102")
	var names []string
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, filePrefix) && strings.HasSuffix(name, fileSuffix) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		day := strings.TrimSuffix(strings.TrimPrefix(name, filePrefix), fileSuffix)
		if day < cutoff {
			_ = os.Remove(filepath.Join(logger.directory, name))
		}
	}
}

// Close closes the current file.
func (logger *Logger) Close() {
	if logger == nil {
		return
	}
	logger.lock.Lock()
	defer logger.lock.Unlock()
	if logger.file != nil {
		_ = logger.file.Close()
		logger.file = nil
	}
}
