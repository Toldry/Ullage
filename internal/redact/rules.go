// Package redact decides which names may leave the machine and renders paths for prompts
// with placeholders (<R1>, <R2>.pdf) in place of redacted names.
package redact

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/toldry/ullage/internal/platform"
)

// RuleType selects which items a rule matches.
type RuleType string

const (
	// RuleExact matches one file or folder at exactly this path.
	RuleExact RuleType = "exact"
	// RuleSubtree matches a folder and everything inside it.
	RuleSubtree RuleType = "subtree"
	// RuleGlob matches paths against a pattern: * within a segment, ** across segments.
	RuleGlob RuleType = "glob"
	// RuleName matches a file or folder name at any depth, on any drive.
	RuleName RuleType = "name"
)

// Treatment says how a matched item is rendered.
type Treatment string

const (
	// TreatmentSubtree replaces the name and every name inside it.
	TreatmentSubtree Treatment = "subtree"
	// TreatmentKeepExtension replaces the name but keeps a file's extension.
	TreatmentKeepExtension Treatment = "keep-extension"
	// TreatmentNone shows the item as is; only valid as a session override.
	TreatmentNone Treatment = "none"
)

// Rule is one remembered redaction rule. Pattern is in display form: forward slashes and
// ~ for the current user's profile, or a bare name pattern for RuleName.
type Rule struct {
	ID            string    `json:"id"`
	Type          RuleType  `json:"type"`
	Pattern       string    `json:"pattern"`
	Treatment     Treatment `json:"treatment"`
	KeepWellKnown bool      `json:"keepWellKnown"`
	// Anchor is the file ID of the matched item for exact and subtree rules, so the rule
	// follows the item when it is renamed or moved within the same volume.
	Anchor      *platform.FileID `json:"anchor,omitempty"`
	Created     time.Time        `json:"created"`
	LastMatched time.Time        `json:"lastMatched,omitempty"`
}

const maximumPatternLength = 1000

// Validate checks a rule before it is stored.
func (rule Rule) Validate() error {
	switch rule.Type {
	case RuleExact, RuleSubtree, RuleGlob, RuleName:
	default:
		return errors.New("rule type must be exact, subtree, glob or name")
	}
	switch rule.Treatment {
	case TreatmentSubtree, TreatmentKeepExtension:
	default:
		return errors.New("treatment must be subtree or keep-extension")
	}
	pattern := strings.TrimSpace(rule.Pattern)
	if pattern == "" || len(pattern) > maximumPatternLength {
		return errors.New("pattern must be between 1 and 1000 characters")
	}
	if rule.Type == RuleName && strings.ContainsAny(pattern, `/\`) {
		return errors.New("a name rule matches one name; it cannot contain / or \\")
	}
	return nil
}

// NewRuleID returns a random identifier for a rule.
func NewRuleID() string {
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(bytes)
}
