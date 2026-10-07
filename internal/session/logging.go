package session

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/toldry/ullage/internal/scanner"
)

// Every line written to the log file is built from safe parts: counts, enums, paths
// rendered by safePath and free text passed through safeText. Rule patterns, prompts and
// replies are never logged (safety rule 8).

// pathWithheld replaces a path that cannot be rendered safely.
const pathWithheld = "<path withheld>"

// maximumPendingEvents bounds the scan events held back until the scan finishes.
const maximumPendingEvents = 5000

var (
	windowsPathPattern = regexp.MustCompile(`(?i)(?:\\\\\?\\)?[a-z]:\\[^:"'<>|*?\r\n]*`)
	unixPathPattern    = regexp.MustCompile(`(^|[\s"'(=])(/[^:"'<>|*?\s][^:"'<>|*?\r\n]*)`)
	sidPattern         = regexp.MustCompile(`(?i)\bS-1-\d+(?:-\d+)+\b`)
)

// Log writes a line that is safe by construction (no paths, no user text). Callers
// outside the session use it for start-up and shutdown lines.
func (manager *Manager) Log(event, text string) {
	manager.environment.Logger.Write(event, text)
}

func (manager *Manager) logf(event, format string, arguments ...any) {
	manager.environment.Logger.Write(event, fmt.Sprintf(format, arguments...))
}

// safeNode renders a scan node exactly as a prompt would. Callers hold manager.lock.
func (manager *Manager) safeNode(index scanner.NodeIndex) string {
	if manager.completed == nil {
		return pathWithheld
	}
	redactor, err := manager.currentRedactor()
	if err != nil {
		// Unreadable rules: nothing can be shown safely (safety rule 7 applies here too).
		return pathWithheld
	}
	return redactor.Render(index)
}

// safePath renders a native path through the redactor. The deepest scanned node on the
// path is rendered; anything below it, which the scan never recorded by name, is shown
// as "…". A path outside the scan, or any path while the rules cannot be read, is
// withheld. Callers hold manager.lock.
func (manager *Manager) safePath(native string) string {
	if manager.completed == nil || native == "" {
		return pathWithheld
	}
	index, hidden := manager.locate(native)
	if index == scanner.NoNode {
		return pathWithheld
	}
	rendered := manager.safeNode(index)
	if rendered == pathWithheld || hidden == 0 {
		return rendered
	}
	return strings.TrimSuffix(rendered, "/") + "/…"
}

// locate finds the deepest scan node on a path and how many segments lie below it.
func (manager *Manager) locate(native string) (scanner.NodeIndex, int) {
	tree := manager.completed.Result.Tree
	fold := manager.environment.Locations.CaseInsensitive
	equal := func(first, second string) bool {
		if fold {
			return strings.EqualFold(first, second)
		}
		return first == second
	}
	cleaned := filepath.Clean(native)
	separator := string(filepath.Separator)
	for _, root := range tree.Roots {
		rootPath := filepath.Clean(tree.Path(root))
		prefix := strings.TrimSuffix(rootPath, separator) + separator
		var rest string
		switch {
		case equal(cleaned, rootPath):
			return root, 0
		case len(cleaned) > len(prefix) && equal(cleaned[:len(prefix)], prefix):
			rest = cleaned[len(prefix):]
		default:
			continue
		}
		segments := strings.Split(rest, separator)
		current := root
		for position, segment := range segments {
			next := scanner.NoNode
			for _, child := range tree.Node(current).Children {
				if equal(tree.Node(child).Name, segment) {
					next = child
					break
				}
			}
			if next == scanner.NoNode {
				return current, len(segments) - position
			}
			current = next
		}
		return current, 0
	}
	return scanner.NoNode, 0
}

// safeText makes free text, such as an error message, safe to log: embedded paths are
// rendered with safePath, and account names, the machine name and SIDs are replaced.
// Callers hold manager.lock.
func (manager *Manager) safeText(text string) string {
	return manager.scrub(text, manager.safePath)
}

// scrub replaces embedded paths with render's result, then account names, the machine
// name and SIDs.
func (manager *Manager) scrub(text string, render func(string) string) string {
	text = windowsPathPattern.ReplaceAllStringFunc(text, func(path string) string {
		trimmed := strings.TrimRight(path, " .")
		return render(strings.TrimPrefix(trimmed, `\\?\`)) + path[len(trimmed):]
	})
	text = unixPathPattern.ReplaceAllStringFunc(text, func(match string) string {
		parts := unixPathPattern.FindStringSubmatch(match)
		trimmed := strings.TrimRight(parts[2], " .")
		return parts[1] + render(trimmed) + parts[2][len(trimmed):]
	})
	text = sidPattern.ReplaceAllString(text, "<sid>")
	for _, word := range manager.environment.SensitiveWords {
		if strings.TrimSpace(word) == "" {
			continue
		}
		pattern := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(word))
		text = pattern.ReplaceAllString(text, "<account>")
	}
	return text
}

// flushScanEvents writes the scan's held-back events once paths can be rendered.
// Callers hold manager.lock.
func (manager *Manager) flushScanEvents() {
	for _, event := range manager.pendingEvents {
		line := string(event.Kind) + " " + manager.safeText(event.Text)
		if event.Path != "" {
			line += " · " + manager.safePath(event.Path)
		}
		if event.Bytes > 0 {
			line += fmt.Sprintf(" · %d bytes", event.Bytes)
		}
		manager.environment.Logger.Write("scan", line)
	}
	if manager.droppedEvents > 0 {
		manager.logf("scan", "%d more events not logged", manager.droppedEvents)
	}
	manager.pendingEvents, manager.droppedEvents = nil, 0
}

// logScanFinished writes the summary and the held-back events. Callers hold
// manager.lock.
func (manager *Manager) logScanFinished(progress scanner.Progress) {
	result := manager.completed.Result
	var roots []string
	for _, root := range result.Tree.Roots {
		roots = append(roots, manager.safeNode(root))
	}
	manager.logf("scan", "finished %s in %s: %s; %d files, %d folders, %d bytes, "+
		"%d denied, %d errors, %d links", manager.completed.Code,
		result.Finished.Sub(result.Started).Round(1e6), strings.Join(roots, ", "),
		progress.Files, progress.Directories, progress.AllocatedBytes, result.DeniedCount,
		result.ErrorCount, result.LinkCount)
	manager.flushScanEvents()
}

func pluralWord(count int, singular, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
}
