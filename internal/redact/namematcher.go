package redact

import (
	"strings"
)

// nameMatcher answers "does this name contain any of these redacted names?" in time
// linear in the name's length, however many names there are. A subtree rule over a large
// folder can contribute thousands of names, and every visible node is checked.
// Long names match as substrings through an Aho-Corasick automaton; short names match as
// whole words (decision D-028).
type nameMatcher struct {
	shortWords map[string]bool
	automaton  *ahoCorasick
}

func newNameMatcher(names []string) *nameMatcher {
	matcher := &nameMatcher{shortWords: map[string]bool{}}
	var long []string
	for _, name := range names {
		lower := strings.ToLower(name)
		if lower == "" {
			continue
		}
		if len([]rune(name)) < shortNameLength {
			matcher.shortWords[lower] = true
		} else {
			long = append(long, lower)
		}
	}
	if len(long) > 0 {
		matcher.automaton = buildAhoCorasick(long)
	}
	return matcher
}

func (matcher *nameMatcher) matches(name string) bool {
	if matcher == nil {
		return false
	}
	if len(matcher.shortWords) > 0 {
		for _, word := range splitWords(name) {
			if matcher.shortWords[strings.ToLower(word)] {
				return true
			}
		}
	}
	return matcher.automaton != nil && matcher.automaton.containsAny(strings.ToLower(name))
}

type ahoNode struct {
	next     map[byte]int32
	fail     int32
	terminal bool
}

type ahoCorasick struct {
	nodes []ahoNode
}

func buildAhoCorasick(patterns []string) *ahoCorasick {
	automaton := &ahoCorasick{nodes: []ahoNode{{next: map[byte]int32{}}}}
	for _, pattern := range patterns {
		current := int32(0)
		for index := 0; index < len(pattern); index++ {
			character := pattern[index]
			child, exists := automaton.nodes[current].next[character]
			if !exists {
				automaton.nodes = append(automaton.nodes, ahoNode{next: map[byte]int32{}})
				child = int32(len(automaton.nodes) - 1)
				automaton.nodes[current].next[character] = child
			}
			current = child
		}
		automaton.nodes[current].terminal = true
	}
	automaton.linkFailures()
	return automaton
}

// linkFailures sets each node's fallback to the longest proper suffix that is also a
// prefix, breadth first, and propagates terminal flags along those links.
func (automaton *ahoCorasick) linkFailures() {
	queue := []int32{}
	for _, child := range automaton.nodes[0].next {
		automaton.nodes[child].fail = 0
		queue = append(queue, child)
	}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for character, child := range automaton.nodes[current].next {
			fallback := automaton.nodes[current].fail
			for fallback != 0 {
				if _, exists := automaton.nodes[fallback].next[character]; exists {
					break
				}
				fallback = automaton.nodes[fallback].fail
			}
			if target, exists := automaton.nodes[fallback].next[character]; exists &&
				target != child {
				automaton.nodes[child].fail = target
			} else {
				automaton.nodes[child].fail = 0
			}
			if automaton.nodes[automaton.nodes[child].fail].terminal {
				automaton.nodes[child].terminal = true
			}
			queue = append(queue, child)
		}
	}
}

func (automaton *ahoCorasick) containsAny(text string) bool {
	current := int32(0)
	for index := 0; index < len(text); index++ {
		character := text[index]
		for current != 0 {
			if _, exists := automaton.nodes[current].next[character]; exists {
				break
			}
			current = automaton.nodes[current].fail
		}
		if next, exists := automaton.nodes[current].next[character]; exists {
			current = next
		}
		if automaton.nodes[current].terminal {
			return true
		}
	}
	return false
}
