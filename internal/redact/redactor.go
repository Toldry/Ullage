package redact

import (
	"path/filepath"
	"strings"

	"github.com/toldry/ullage/internal/pathpattern"
	"github.com/toldry/ullage/internal/platform"
	"github.com/toldry/ullage/internal/scanner"
)

// Decision is how one node's own name is rendered.
type Decision uint8

const (
	DecisionVisible Decision = iota
	// DecisionHome is the current user's profile folder, rendered as ~.
	DecisionHome
	// DecisionHidden is a node inside a redacted subtree.
	DecisionHidden
	// DecisionRedacted replaces the whole name with a placeholder.
	DecisionRedacted
	// DecisionRedactedKeepExtension replaces the name but keeps a file's extension.
	DecisionRedactedKeepExtension
)

// Source says why a node got its decision.
type Source uint8

const (
	SourceNone Source = iota
	SourceRule
	SourceSession
	SourceAlways
	SourceAncestor
)

// NodeState is the redaction state of one node.
type NodeState struct {
	Decision Decision
	Source   Source
	// Rule is the index of the rule behind the decision, or -1.
	Rule int32
	// redactedHome marks the current profile folder under a redaction: it still reads ~,
	// which says nothing about the user, but it carries no metadata (decision D-075).
	redactedHome       bool
	hideBelow          bool
	keepWellKnownBelow bool
	keepExtensionBelow bool
}

// NameHidden reports whether the node's own name is not shown as-is. Such items carry no
// metadata in prompts (decision D-030).
func (state NodeState) NameHidden() bool {
	switch state.Decision {
	case DecisionRedacted, DecisionRedactedKeepExtension, DecisionHidden:
		return true
	}
	return false
}

// Override is a choice made in the redaction review for this session only.
type Override struct {
	Treatment     Treatment `json:"treatment"`
	KeepWellKnown bool      `json:"keepWellKnown"`
}

// Context holds the always-on redaction inputs.
type Context struct {
	// CurrentProfile is the native path rendered as ~.
	CurrentProfile string
	// SensitiveWords are account names and the machine name; any visible name containing
	// one is redacted (decision D-029).
	SensitiveWords  []string
	CaseInsensitive bool
}

// RuleMatch summarizes what one rule matched in the current scan.
type RuleMatch struct {
	// Nodes are the outermost matched nodes.
	Nodes []scanner.NodeIndex
	Bytes int64
	// MovedTo is set when an anchored rule found its item at a new path.
	MovedTo scanner.NodeIndex
}

// Options configures a Redactor.
type Options struct {
	Tree      *scanner.Tree
	Context   Context
	Rules     []Rule
	Overrides map[scanner.NodeIndex]Override
	Registry  *Registry
}

// Redactor holds the decisions for one scan. It is immutable after New, except for
// placeholder allocation, which is synchronized in the Registry.
type Redactor struct {
	tree        *scanner.Tree
	context     Context
	rules       []Rule
	registry    *Registry
	states      []NodeState
	matches     []RuleMatch
	profileKeys []string
}

const homeOwner int32 = -2

// New computes decisions for every node of the tree.
func New(options Options) *Redactor {
	redactor := &Redactor{
		tree:     options.Tree,
		context:  options.Context,
		rules:    options.Rules,
		registry: options.Registry,
		states:   make([]NodeState, options.Tree.Len()),
		matches:  make([]RuleMatch, len(options.Rules)),
	}
	for index := range redactor.matches {
		redactor.matches[index].MovedTo = scanner.NoNode
	}
	redactor.profileKeys = redactor.normalizedSegments(options.Context.CurrentProfile)
	patterns, nameRules := redactor.compileRules()
	anchored := redactor.findAnchoredNodes()
	redactor.decide(patterns, nameRules, anchored, options.Overrides)
	return redactor
}

func (redactor *Redactor) normalizedSegments(path string) []string {
	segments := pathpattern.SplitPath(path)
	if redactor.context.CaseInsensitive {
		for index := range segments {
			segments[index] = strings.ToLower(segments[index])
		}
	}
	return segments
}

// expandHome turns a display pattern ("~/x", "C:/x") into a native-like path.
func (redactor *Redactor) expandHome(pattern string) string {
	if pattern == "~" || strings.HasPrefix(pattern, "~/") {
		return redactor.context.CurrentProfile + pattern[1:]
	}
	return pattern
}

func (redactor *Redactor) compileRules() (*pathpattern.Set, []int32) {
	patterns := pathpattern.NewSet(redactor.context.CaseInsensitive)
	var nameRules []int32
	for index, rule := range redactor.rules {
		switch rule.Type {
		case RuleExact, RuleSubtree:
			patterns.Add(redactor.expandHome(rule.Pattern), true, int32(index))
		case RuleGlob:
			patterns.Add(redactor.expandHome(rule.Pattern), false, int32(index))
		case RuleName:
			nameRules = append(nameRules, int32(index))
		}
	}
	if redactor.context.CurrentProfile != "" {
		patterns.Add(redactor.context.CurrentProfile, true, homeOwner)
	}
	return patterns, nameRules
}

// findAnchoredNodes locates items of anchored rules by file ID, wherever they are now.
func (redactor *Redactor) findAnchoredNodes() map[int32]scanner.NodeIndex {
	ruleByAnchor := map[platform.FileID]int32{}
	for index, rule := range redactor.rules {
		if rule.Anchor != nil && !rule.Anchor.IsZero() &&
			(rule.Type == RuleExact || rule.Type == RuleSubtree) {
			ruleByAnchor[*rule.Anchor] = int32(index)
		}
	}
	anchored := map[int32]scanner.NodeIndex{}
	if len(ruleByAnchor) == 0 {
		return anchored
	}
	redactor.tree.Walk(func(index scanner.NodeIndex, node *scanner.Node) {
		if ruleIndex, found := ruleByAnchor[node.ID]; found && !node.ID.IsZero() {
			anchored[ruleIndex] = index
		}
	})
	return anchored
}

func (redactor *Redactor) decide(patterns *pathpattern.Set, nameRules []int32,
	anchored map[int32]scanner.NodeIndex, overrides map[scanner.NodeIndex]Override) {
	tree := redactor.tree
	matchStates := make([]*pathpattern.State, tree.Len())
	tree.Walk(func(index scanner.NodeIndex, node *scanner.Node) {
		var accepted []int32
		if node.Parent == scanner.NoNode {
			state := patterns.Start()
			for _, segment := range pathpattern.SplitPath(node.Name) {
				if state = patterns.Advance(state, segment); state == nil {
					break
				}
			}
			matchStates[index] = state
		} else if parentState := matchStates[node.Parent]; parentState != nil {
			matchStates[index] = patterns.Advance(parentState, node.Name)
		}
		if matchStates[index] != nil {
			accepted = patterns.Accepted(matchStates[index])
		}
		redactor.states[index] = redactor.decideNode(index, node, accepted, nameRules,
			anchored, overrides)
	})
}

func (redactor *Redactor) decideNode(index scanner.NodeIndex, node *scanner.Node,
	accepted []int32, nameRules []int32, anchored map[int32]scanner.NodeIndex,
	overrides map[scanner.NodeIndex]Override) NodeState {
	none := NodeState{Rule: -1}
	if node.Parent != scanner.NoNode {
		parent := redactor.states[node.Parent]
		if parent.hideBelow {
			return NodeState{Decision: DecisionHidden, Source: SourceAncestor,
				Rule: parent.Rule, hideBelow: true,
				keepWellKnownBelow: parent.keepWellKnownBelow}
		}
		if parent.keepExtensionBelow {
			none = NodeState{Decision: DecisionRedactedKeepExtension, Source: SourceRule,
				Rule: parent.Rule, keepExtensionBelow: true}
		}
	}

	if override, found := overrides[index]; found {
		state := applyTreatment(override.Treatment, override.KeepWellKnown, SourceSession, -1)
		state.redactedHome = isHome(accepted) && state.Decision != DecisionVisible
		return state
	}
	if ruleIndex := redactor.firstMatchingRule(index, node, accepted, nameRules,
		anchored); ruleIndex >= 0 {
		rule := redactor.rules[ruleIndex]
		state := applyTreatment(rule.Treatment, rule.KeepWellKnown, SourceRule, ruleIndex)
		state.keepExtensionBelow = rule.Type == RuleSubtree &&
			rule.Treatment == TreatmentKeepExtension
		redactor.recordMatch(index, node, ruleIndex)
		state.redactedHome = isHome(accepted) && state.Decision != DecisionVisible
		return state
	}
	if none.Decision != DecisionVisible {
		return none
	}
	if isHome(accepted) {
		return NodeState{Decision: DecisionHome, Rule: -1}
	}
	if redactor.containsAlwaysRedacted(node.Name) {
		return applyTreatment(TreatmentKeepExtension, false, SourceAlways, -1)
	}
	return none
}

func isHome(accepted []int32) bool {
	for _, owner := range accepted {
		if owner == homeOwner {
			return true
		}
	}
	return false
}

func applyTreatment(treatment Treatment, keepWellKnown bool, source Source,
	ruleIndex int32) NodeState {
	switch treatment {
	case TreatmentSubtree:
		return NodeState{Decision: DecisionRedacted, Source: source, Rule: ruleIndex,
			hideBelow: true, keepWellKnownBelow: keepWellKnown}
	case TreatmentKeepExtension:
		return NodeState{Decision: DecisionRedactedKeepExtension, Source: source,
			Rule: ruleIndex}
	default:
		return NodeState{Decision: DecisionVisible, Source: source, Rule: ruleIndex}
	}
}

// firstMatchingRule returns the lowest-index rule that matches the node, or -1. Rules
// whose anchor was found match only their anchored node, so a new item that happens to
// take the old path is not redacted by mistake while the real item is.
func (redactor *Redactor) firstMatchingRule(index scanner.NodeIndex, node *scanner.Node,
	accepted []int32, nameRules []int32, anchored map[int32]scanner.NodeIndex) int32 {
	best := int32(-1)
	consider := func(ruleIndex int32) {
		if best == -1 || ruleIndex < best {
			best = ruleIndex
		}
	}
	for _, ruleIndex := range accepted {
		if ruleIndex < 0 {
			continue
		}
		if _, isAnchored := anchored[ruleIndex]; isAnchored {
			continue
		}
		consider(ruleIndex)
	}
	for ruleIndex, anchoredIndex := range anchored {
		if anchoredIndex == index {
			consider(ruleIndex)
		}
	}
	for _, ruleIndex := range nameRules {
		if pathpattern.MatchName(redactor.rules[ruleIndex].Pattern, node.Name,
			redactor.context.CaseInsensitive) {
			consider(ruleIndex)
		}
	}
	return best
}

func (redactor *Redactor) recordMatch(index scanner.NodeIndex, node *scanner.Node,
	ruleIndex int32) {
	if node.Parent != scanner.NoNode {
		parent := redactor.states[node.Parent]
		if parent.Rule == ruleIndex && parent.Source != SourceNone {
			return
		}
	}
	match := &redactor.matches[ruleIndex]
	match.Nodes = append(match.Nodes, index)
	match.Bytes += node.TotalBytes()
	rule := redactor.rules[ruleIndex]
	if rule.Anchor != nil && node.ID == *rule.Anchor &&
		!redactor.pathMatchesPattern(index, rule.Pattern) {
		match.MovedTo = index
	}
}

func (redactor *Redactor) pathMatchesPattern(index scanner.NodeIndex, pattern string) bool {
	nodePath := redactor.normalizedSegments(redactor.tree.Path(index))
	patternPath := redactor.normalizedSegments(redactor.expandHome(pattern))
	return strings.Join(nodePath, "/") == strings.Join(patternPath, "/")
}

func (redactor *Redactor) containsAlwaysRedacted(name string) bool {
	if windowsSecurityIdentifier.MatchString(name) {
		return true
	}
	for _, word := range redactor.context.SensitiveWords {
		if containsSensitive(name, word) {
			return true
		}
	}
	return false
}

// State returns the decision for a node.
func (redactor *Redactor) State(index scanner.NodeIndex) NodeState {
	return redactor.states[index]
}

// Matches returns what each rule matched, in rule order.
func (redactor *Redactor) Matches() []RuleMatch {
	return redactor.matches
}

// Render returns the path of a node as it may appear in a prompt: forward slashes, ~ for
// the current profile, placeholders for redacted names.
func (redactor *Redactor) Render(index scanner.NodeIndex) string {
	var reversed []string
	current := index
	for ; current != scanner.NoNode; current = redactor.tree.Node(current).Parent {
		node := redactor.tree.Node(current)
		if node.Parent == scanner.NoNode {
			break
		}
		reversed = append(reversed, redactor.renderName(current, node))
	}
	parts := redactor.renderRoot(current)
	for position := len(reversed) - 1; position >= 0; position-- {
		parts = append(parts, reversed[position])
	}
	rendered := strings.Join(parts, "/")
	if strings.HasPrefix(filepath.ToSlash(redactor.tree.Node(current).Name), "/") {
		rendered = "/" + rendered
	}
	return rendered
}

// RenderName returns how the node's own name appears in a prompt.
func (redactor *Redactor) RenderName(index scanner.NodeIndex) string {
	node := redactor.tree.Node(index)
	if node.Parent == scanner.NoNode {
		parts := redactor.renderRoot(index)
		if len(parts) == 0 {
			return "/"
		}
		return parts[len(parts)-1]
	}
	return redactor.renderName(index, node)
}

// NameShown reports whether the node's own name appears as-is in prompts, which is what
// decides whether its metadata may be sent (decision D-030).
func (redactor *Redactor) NameShown(index scanner.NodeIndex) bool {
	if redactor.states[index].redactedHome {
		return false
	}
	return !strings.HasPrefix(redactor.RenderName(index), "<R")
}

func (redactor *Redactor) renderName(index scanner.NodeIndex, node *scanner.Node) string {
	state := redactor.states[index]
	if state.redactedHome {
		return "~"
	}
	switch state.Decision {
	case DecisionHome:
		return "~"
	case DecisionHidden:
		if state.keepWellKnownBelow && isWellKnown(node.Name) &&
			!redactor.containsAlwaysRedacted(node.Name) {
			return sanitize(node.Name)
		}
		return redactor.registry.Placeholder(node.Name)
	case DecisionRedacted:
		return redactor.registry.Placeholder(node.Name)
	case DecisionRedactedKeepExtension:
		return redactor.placeholderKeepingExtension(node)
	default:
		return sanitize(node.Name)
	}
}

func (redactor *Redactor) placeholderKeepingExtension(node *scanner.Node) string {
	if node.Kind != platform.EntryFile {
		return redactor.registry.Placeholder(node.Name)
	}
	// Extensions are never treated as sensitive (decision D-031).
	extension := scanner.NormalizeExtension(node.Name)
	if extension == "" {
		return redactor.registry.Placeholder(node.Name)
	}
	return redactor.registry.Placeholder(stem(node.Name)) + "." + extension
}

func stem(name string) string {
	return strings.TrimSuffix(name, filepath.Ext(name))
}

// renderRoot renders the segments of a scan root such as C:\ or D:\clients\acme. They are
// not nodes, so the always-on and same-name checks are applied to each segment here.
func (redactor *Redactor) renderRoot(rootIndex scanner.NodeIndex) []string {
	segments := pathpattern.SplitPath(redactor.tree.Node(rootIndex).Name)
	rendered := make([]string, 0, len(segments))
	for position, segment := range segments {
		switch {
		case position == 0 && len(segment) == 2 && segment[1] == ':':
			rendered = append(rendered, strings.ToUpper(segment[:1])+":")
		case redactor.isProfileSegment(segments, position):
			rendered = append(rendered, "~")
		case redactor.containsAlwaysRedacted(segment):
			rendered = append(rendered, redactor.registry.Placeholder(segment))
		default:
			rendered = append(rendered, sanitize(segment))
		}
	}
	return rendered
}

func (redactor *Redactor) isProfileSegment(segments []string, position int) bool {
	if position != len(redactor.profileKeys)-1 || len(redactor.profileKeys) == 0 {
		return false
	}
	for index := 0; index <= position; index++ {
		segment := segments[index]
		if redactor.context.CaseInsensitive {
			segment = strings.ToLower(segment)
		}
		if segment != redactor.profileKeys[index] {
			return false
		}
	}
	return true
}
