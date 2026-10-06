// Package tiers decides which tier every scanned item belongs to: System, Known (claimed by
// a recipe) or Unknown. It compiles recipe path patterns into a small NFA that advances
// one path segment at a time, matching the order in which the scanner walks.
package tiers

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

// compiledPattern is one expanded pattern; rule is the index of the rule it belongs to.
type compiledPattern struct {
	segments []segmentMatcher
	rule     int32
}

var variablePattern = regexp.MustCompile(`\{([A-Za-z0-9]+)\}`)

// expandVariables replaces each {Variable} with every value the platform knows for it.
// A pattern that refers to an unknown or empty variable expands to nothing.
func expandVariables(pattern string, locations platform.KnownLocations) []string {
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
		expanded = append(expanded, expandVariables(replaced, locations)...)
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

// matchState is the set of live positions after a path prefix. It is immutable once
// built, so children can share their parent's state safely across goroutines.
type matchState struct {
	positions []position
}

type patternSet struct {
	patterns        []compiledPattern
	caseInsensitive bool
}

// closure adds positions reachable without consuming a segment (skipping `**`).
func (set *patternSet) closure(positions []position) []position {
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

func (set *patternSet) start() *matchState {
	positions := make([]position, len(set.patterns))
	for index := range set.patterns {
		positions[index] = position{pattern: int32(index)}
	}
	return &matchState{positions: set.closure(positions)}
}

// advance consumes one path segment. It returns nil when no pattern can match anymore.
func (set *patternSet) advance(state *matchState, name string) *matchState {
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
	return &matchState{positions: set.closure(next)}
}

// acceptedRules returns the rules of patterns fully matched by the current path.
func (set *patternSet) acceptedRules(state *matchState) []int32 {
	var rules []int32
	for _, current := range state.positions {
		if int(current.matched) == len(set.patterns[current.pattern].segments) {
			rules = append(rules, set.patterns[current.pattern].rule)
		}
	}
	return rules
}
