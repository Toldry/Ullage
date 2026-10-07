package deletion

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/toldry/ullage/internal/platform"
)

// Remover performs the actual deletion. Production code uses the platform's Recycle Bin
// and PermanentRemover; tests substitute fakes.
type Remover interface {
	Recycle(paths []string) error
	// RemovePermanently deletes one entry and everything below it, never following links.
	RemovePermanently(path string) error
}

// Outcome is what happened to one target.
type Outcome struct {
	Check
	// Remaining lists entries that still exist after a failed or partial deletion.
	Remaining int `json:"remaining"`
}

// Delete re-verifies the target and, only if it is unchanged, deletes it. The result says
// exactly what happened, checked on disk afterwards rather than assumed from the error.
func (guard *Guard) Delete(target Target, remover Remover, permanent bool) Outcome {
	check := guard.Verify(target)
	if check.Status != StatusReady {
		return Outcome{Check: check}
	}
	var failure error
	if permanent {
		var failures []error
		for _, path := range check.Paths {
			if err := remover.RemovePermanently(path); err != nil {
				failures = append(failures, err)
			}
		}
		failure = errors.Join(failures...)
	} else {
		failure = remover.Recycle(check.Paths)
	}
	remaining := 0
	disk := newLister(guard.Reader)
	for _, path := range check.Paths {
		// An entry whose folder cannot be read is counted as remaining: the report must
		// never claim a deletion it cannot confirm.
		if _, found, err := disk.entry(filepath.Dir(path), filepath.Base(path)); found ||
			err != nil {
			remaining++
		}
	}
	outcome := Outcome{Check: check, Remaining: remaining}
	switch {
	case remaining == 0:
		outcome.Status, outcome.Reason = StatusDeleted, ""
		if !permanent {
			outcome.Status = StatusRecycled
		}
	case remaining == len(check.Paths) && guard.unchanged(target, check):
		outcome.Status = StatusFailed
		outcome.Reason = "nothing changed: " + describeFailure(failure)
	default:
		outcome.Status = StatusPartial
		outcome.Reason = fmt.Sprintf("partly removed; %d of %d entries remain: %s",
			remaining, len(check.Paths), describeFailure(failure))
	}
	return outcome
}

// unchanged reports whether a failed deletion left the target as it was.
func (guard *Guard) unchanged(target Target, before Check) bool {
	after := guard.Verify(target)
	return after.Status == StatusReady && after.Now == before.Now
}

func describeFailure(err error) string {
	switch {
	case err == nil:
		return "the item could not be removed"
	case platform.IsInUse(err):
		return "in use by another program; close it and retry"
	case platform.IsAccessDenied(err) || errors.Is(err, os.ErrPermission):
		return "access denied"
	default:
		return err.Error()
	}
}

// PermanentRemover deletes without the Recycle Bin. It walks folders itself so that a
// junction or symbolic link inside is removed as a link and its target is never touched.
type PermanentRemover struct {
	Reader platform.DirectoryReader
}

// RemovePermanently implements Remover. The entry's kind comes from the directory
// reader, which reports every junction and reparse point as a link, rather than from
// os.Lstat, whose view of Windows reparse points differs between Go versions.
func (remover PermanentRemover) RemovePermanently(path string) error {
	entries, err := remover.Reader.ReadDirectory(filepath.Dir(path))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name != filepath.Base(path) {
			continue
		}
		if entry.Kind == platform.EntryDirectory {
			return remover.removeDirectory(path)
		}
		return os.Remove(path)
	}
	return fmt.Errorf("%s: %w", path, os.ErrNotExist)
}

func (remover PermanentRemover) removeDirectory(path string) error {
	entries, err := remover.Reader.ReadDirectory(path)
	if err != nil {
		return err
	}
	var failures []error
	for _, entry := range entries {
		child := filepath.Join(path, entry.Name)
		var childErr error
		if entry.Kind == platform.EntryDirectory {
			childErr = remover.removeDirectory(child)
		} else {
			// Files and links alike: removing a link deletes the link, not its target.
			childErr = os.Remove(child)
		}
		if childErr != nil {
			failures = append(failures, childErr)
		}
	}
	if len(failures) > 0 {
		return errors.Join(failures...)
	}
	return os.Remove(path)
}

// Recycle implements Remover with the platform Recycle Bin.
func (remover PermanentRemover) Recycle(paths []string) error {
	return platform.MoveToRecycleBin(paths)
}
