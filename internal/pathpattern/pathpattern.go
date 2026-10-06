// Package pathpattern compiles path patterns into a small NFA that advances one path
// segment at a time, which matches the order in which the scanner walks a tree. Tier
// classification, scan exclusions and redaction rules all use it.
//
// Pattern syntax: segments separated by / or \; * and ? match within one segment;
// ** matches zero or more whole segments. Literal patterns treat every character as-is.
package pathpattern

import (
	"path"
	"regexp"
	"strings"

	"github.com/toldry/ullage/internal/platform"
)

type segmentKind uint8

const (
	segmentLiteral segmentKind = iota
	segmentGlob
	// segmentAnyDepth is `**`: zero or more whole segments.
	segmentAnyDepth
)

type segmentMatcher struct {
	kind  segmentKind
	value string
}

func (matcher segmentMatcher) matches(name string, caseInsensitive bool) bool {
	if caseInsensitive {
		name = strings.ToLower(name)
	}
	switch matcher.kind {
	case segmentLiteral:
		return matcher.value == name
	case segmentGlob:
		matched, _ := path.Match(matcher.value, name)
		return matched
	default:
		return true
	}
}

// compiledPattern is one expanded pattern; owner identifies the rule it belongs to.
type compiledPattern struct {
	segments []segmentMatcher
	owner    int32
}

var variablePattern = regexp.MustCompile(`\{([A-Za-z0-9]+)\}`)

// ExpandVariables replaces each {Variable} with every value the platform knows for it.
// A pattern that refers to an unknown or empty variable expands to nothing.
func ExpandVariables(pattern string, locations platform.KnownLocations) []string {
	match := variablePattern.FindStringSubmatchIndex(pattern)
	if match == nil {
		return []string{pattern}
	}
	name := pattern[match[2]:match[3]]
	var expanded []string
	for _, value := range locations.Variables[name] {
		if value == "" {
			continue
		}
		replaced := pattern[:match[0]] + value + pattern[match[1]:]
		expanded = append(expanded, ExpandVariables(replaced, locations)...)
	}
	return expanded
}

// SplitPath splits a native or slash-separated path into non-empty segments.
func SplitPath(nativePath string) []string {
	normalized := strings.ReplaceAll(nativePath, `\`, "/")
	var segments []string
	for _, segment := range strings.Split(normalized, "/") {
		if segment != "" && segment != "." {
			segments = append(segments, segment)
		}
	}
	return segments
}

// compilePattern turns an expanded path into segment matchers. Literal patterns (from
// installed-application folders) never treat * or ? as wildcards.
func compilePattern(expandedPath string, literal, caseInsensitive bool) []segmentMatcher {
	segments := SplitPath(expandedPath)
	matchers := make([]segmentMatcher, 0, len(segments))
	for _, segment := range segments {
		value := segment
		if caseInsensitive {
			value = strings.ToLower(value)
		}
		switch {
		case !literal && segment == "**":
			matchers = append(matchers, segmentMatcher{kind: segmentAnyDepth})
		case !literal && strings.ContainsAny(segment, "*?["):
			matchers = append(matchers, segmentMatcher{kind: segmentGlob, value: value})
		default:
			matchers = append(matchers, segmentMatcher{kind: segmentLiteral, value: value})
		}
	}
	return matchers
}

// position is a pattern index and how many of its segments have matched.
type position struct {
	pattern int32
	matched int32
}

// State is the set of live positions after a path prefix. It is immutable once built,
// so children can share their parent's state safely across goroutines.
type State struct {
	positions []position
}

// Set is a group of patterns matched together.
type Set struct {
	patterns        []compiledPattern
	caseInsensitive bool
}

// NewSet creates an empty set. caseInsensitive should follow the file system.
func NewSet(caseInsensitive bool) *Set {
	return &Set{caseInsensitive: caseInsensitive}
}

// Add compiles one already-expanded pattern for owner. It returns false for an empty
// pattern.
func (set *Set) Add(pattern string, literal bool, owner int32) bool {
	segments := compilePattern(pattern, literal, set.caseInsensitive)
	if len(segments) == 0 {
		return false
	}
	set.patterns = append(set.patterns, compiledPattern{segments: segments, owner: owner})
	return true
}

// Len returns the number of compiled patterns.
func (set *Set) Len() int { return len(set.patterns) }

// MatchesPath runs a whole path through the set and reports the accepted owners.
func (set *Set) MatchesPath(path string) []int32 {
	if len(set.patterns) == 0 {
		return nil
	}
	state := set.Start()
	for _, segment := range SplitPath(path) {
		if state = set.Advance(state, segment); state == nil {
			return nil
		}
	}
	return set.Accepted(state)
}

// closure adds positions reachable without consuming a segment (skipping `**`).
func (set *Set) closure(positions []position) []position {
	for index := 0; index < len(positions); index++ {
		current := positions[index]
		segments := set.patterns[current.pattern].segments
		if int(current.matched) < len(segments) &&
			segments[current.matched].kind == segmentAnyDepth {
			positions = append(positions, position{current.pattern, current.matched + 1})
		}
	}
	return positions
}

// Start returns the state before any segment has been consumed.
func (set *Set) Start() *State {
	positions := make([]position, len(set.patterns))
	for index := range set.patterns {
		positions[index] = position{pattern: int32(index)}
	}
	return &State{positions: set.closure(positions)}
}

// Advance consumes one path segment. It returns nil when no pattern can match anymore.
func (set *Set) Advance(state *State, name string) *State {
	var next []position
	seen := map[position]bool{}
	add := func(candidate position) {
		if !seen[candidate] {
			seen[candidate] = true
			next = append(next, candidate)
		}
	}
	for _, current := range state.positions {
		segments := set.patterns[current.pattern].segments
		if int(current.matched) >= len(segments) {
			continue
		}
		segment := segments[current.matched]
		if segment.kind == segmentAnyDepth {
			add(current)
			continue
		}
		if segment.matches(name, set.caseInsensitive) {
			add(position{current.pattern, current.matched + 1})
		}
	}
	if len(next) == 0 {
		return nil
	}
	return &State{positions: set.closure(next)}
}

// Accepted returns the owners of patterns fully matched by the current path.
func (set *Set) Accepted(state *State) []int32 {
	var owners []int32
	for _, current := range state.positions {
		if int(current.matched) == len(set.patterns[current.pattern].segments) {
			owners = append(owners, set.patterns[current.pattern].owner)
		}
	}
	return owners
}

// MatchName reports whether a single name matches a one-segment glob such as "*tax*".
func MatchName(pattern, name string, caseInsensitive bool) bool {
	if caseInsensitive {
		pattern, name = strings.ToLower(pattern), strings.ToLower(name)
	}
	matched, _ := path.Match(pattern, name)
	return matched
}
