package redact

import (
	"strconv"
	"strings"
	"sync"
)

// Registry assigns stable placeholder numbers to redacted names. The mapping is the only
// place real names and placeholders meet; it is persisted encrypted and never exported.
type Registry struct {
	lock            sync.Mutex
	numbers         map[string]int
	next            int
	caseInsensitive bool
	changed         bool
}

// NewRegistry restores a registry from stored state.
func NewRegistry(numbers map[string]int, next int, caseInsensitive bool) *Registry {
	copied := make(map[string]int, len(numbers))
	for name, number := range numbers {
		copied[name] = number
		if number >= next {
			next = number + 1
		}
	}
	if next < 1 {
		next = 1
	}
	return &Registry{numbers: copied, next: next, caseInsensitive: caseInsensitive}
}

func (registry *Registry) key(name string) string {
	if registry.caseInsensitive {
		return strings.ToLower(name)
	}
	return name
}

// Placeholder returns "<Rn>" for name, allocating a number the first time.
func (registry *Registry) Placeholder(name string) string {
	registry.lock.Lock()
	defer registry.lock.Unlock()
	key := registry.key(name)
	number, known := registry.numbers[key]
	if !known {
		number = registry.next
		registry.next++
		registry.numbers[key] = number
		registry.changed = true
	}
	return "<R" + strconv.Itoa(number) + ">"
}

// Lookup returns the real name behind a placeholder number, for local display only.
func (registry *Registry) Lookup(number int) (string, bool) {
	registry.lock.Lock()
	defer registry.lock.Unlock()
	for name, candidate := range registry.numbers {
		if candidate == number {
			return name, true
		}
	}
	return "", false
}

// Dirty reports whether numbers were allocated since the last snapshot.
func (registry *Registry) Dirty() bool {
	registry.lock.Lock()
	defer registry.lock.Unlock()
	return registry.changed
}

// Snapshot returns the state to persist and whether it changed since the last snapshot.
func (registry *Registry) Snapshot() (map[string]int, int, bool) {
	registry.lock.Lock()
	defer registry.lock.Unlock()
	copied := make(map[string]int, len(registry.numbers))
	for name, number := range registry.numbers {
		copied[name] = number
	}
	changed := registry.changed
	registry.changed = false
	return copied, registry.next, changed
}
