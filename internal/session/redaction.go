package session

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/toldry/ullage/internal/overview"
	"github.com/toldry/ullage/internal/platform"
	"github.com/toldry/ullage/internal/redact"
	"github.com/toldry/ullage/internal/scanner"
)

// redactionState is guarded by Manager.lock.
type redactionState struct {
	document  redact.Document
	loadError error
	registry  *redact.Registry
	overrides map[scanner.NodeIndex]redact.Override
	// redactor is rebuilt lazily after any rule or override change.
	redactor *redact.Redactor
}

// ErrRulesUnavailable is returned by every redaction operation while the rules file
// cannot be read. Prompt generation checks it too (safety rule 7).
var ErrRulesUnavailable = errors.New("redaction rules cannot be read")

// ErrStaleScan means a request referred to items of a scan that is no longer current.
var ErrStaleScan = errors.New("these items belong to an earlier scan; reload the page")

func (manager *Manager) loadRules() {
	document, err := redact.Load(manager.environment.ConfigDirectory,
		manager.environment.Protector)
	manager.redaction = redactionState{document: document, loadError: err,
		overrides: map[scanner.NodeIndex]redact.Override{}}
	if err == nil {
		manager.redaction.registry = redact.NewRegistry(document.Placeholders,
			document.NextPlaceholder, manager.environment.Locations.CaseInsensitive)
	}
}

func (manager *Manager) resetRedactionForNewScan() {
	manager.redaction.overrides = map[scanner.NodeIndex]redact.Override{}
	manager.redaction.redactor = nil
}

func (manager *Manager) redactionContext() redact.Context {
	return redact.Context{
		CurrentProfile:  manager.CurrentProfile(),
		SensitiveWords:  manager.environment.SensitiveWords,
		CaseInsensitive: manager.environment.Locations.CaseInsensitive,
	}
}

// currentRedactor returns the redactor for the finished scan, rebuilding it if needed.
// Callers hold manager.lock.
func (manager *Manager) currentRedactor() (*redact.Redactor, error) {
	if manager.redaction.loadError != nil {
		return nil, fmt.Errorf("%w: %v", ErrRulesUnavailable, manager.redaction.loadError)
	}
	if manager.completed == nil {
		return nil, errors.New("no finished scan yet")
	}
	if manager.redaction.redactor == nil {
		manager.redaction.redactor = redact.New(redact.Options{
			Tree:      manager.completed.Result.Tree,
			Context:   manager.redactionContext(),
			Rules:     manager.redaction.document.Rules,
			Overrides: manager.redaction.overrides,
			Registry:  manager.redaction.registry,
		})
		manager.applyRuleFeedback()
	}
	return manager.redaction.redactor, nil
}

// applyRuleFeedback records what the latest evaluation learned: rules that matched
// (last-matched date) and anchored rules whose item moved (new pattern). It saves only
// when something changed.
func (manager *Manager) applyRuleFeedback() {
	redactor := manager.redaction.redactor
	tree := manager.completed.Result.Tree
	changed := false
	today := time.Now().Truncate(24 * time.Hour)
	for index, match := range redactor.Matches() {
		rule := &manager.redaction.document.Rules[index]
		if len(match.Nodes) > 0 && rule.LastMatched.Before(today) {
			rule.LastMatched = time.Now().UTC()
			changed = true
		}
		if match.MovedTo != scanner.NoNode {
			rule.Pattern = overview.DisplayPath(tree.Path(match.MovedTo), manager.CurrentProfile())
			changed = true
		}
	}
	if changed {
		_ = manager.saveRules()
	}
}

// saveRules persists rules and placeholders. Callers hold manager.lock. It refuses while
// the stored file is unreadable, so a broken file is never silently replaced.
func (manager *Manager) saveRules() error {
	if manager.redaction.loadError != nil {
		return ErrRulesUnavailable
	}
	numbers, next, _ := manager.redaction.registry.Snapshot()
	manager.redaction.document.Placeholders = numbers
	manager.redaction.document.NextPlaceholder = next
	return redact.Save(manager.environment.ConfigDirectory, manager.environment.Protector,
		manager.redaction.document)
}

// PersistPlaceholders saves placeholder numbers allocated since the last save, so the
// same name keeps its placeholder in later sessions.
func (manager *Manager) PersistPlaceholders() error {
	manager.lock.Lock()
	defer manager.lock.Unlock()
	if manager.redaction.registry == nil || !manager.redaction.registry.Dirty() {
		return nil
	}
	return manager.saveRules()
}

// RedactionSummary is the header of the redaction review and the rules screen.
type RedactionSummary struct {
	Available      bool   `json:"available"`
	Error          string `json:"error,omitempty"`
	StorePath      string `json:"storePath"`
	SessionCode    string `json:"sessionCode"`
	Rules          int    `json:"rules"`
	UnknownItems   int    `json:"unknownItems"`
	Redacted       int    `json:"redacted"`
	ByRules        int    `json:"byRules"`
	BySession      int    `json:"bySession"`
	AlwaysRedacted int    `json:"alwaysRedacted"`
}

// RedactionSummary reports whether redaction works and how many items it affects.
func (manager *Manager) RedactionSummary() RedactionSummary {
	manager.lock.Lock()
	defer manager.lock.Unlock()
	summary := RedactionSummary{
		StorePath: redact.StorePath(manager.environment.ConfigDirectory),
		Rules:     len(manager.redaction.document.Rules),
	}
	redactor, err := manager.currentRedactor()
	if errors.Is(err, ErrRulesUnavailable) {
		summary.Error = "The redaction rules file exists but cannot be read or decrypted. " +
			"Ullage will not generate prompts until this is resolved."
		return summary
	}
	summary.Available = true
	if err != nil {
		return summary
	}
	summary.SessionCode = manager.completed.Code
	manager.completed.Result.Tree.Walk(func(index scanner.NodeIndex, node *scanner.Node) {
		if node.TierBytes(scanner.TierUnknown) == 0 || node.Parent == scanner.NoNode {
			return
		}
		summary.UnknownItems++
		state := redactor.State(index)
		if !state.NameHidden() || state.Decision == redact.DecisionHidden {
			return
		}
		summary.Redacted++
		switch state.Source {
		case redact.SourceRule:
			summary.ByRules++
		case redact.SourceSession:
			summary.BySession++
		default:
			summary.AlwaysRedacted++
		}
	})
	return summary
}

// RedactionItem is one row of the redaction review tree. Name and Path are real and
// shown only in the local UI; Rendered is exactly what a prompt would contain.
type RedactionItem struct {
	ID            scanner.NodeIndex `json:"id"`
	Name          string            `json:"name"`
	Path          string            `json:"path"`
	Kind          string            `json:"kind"`
	Bytes         int64             `json:"bytes"`
	Files         int64             `json:"files"`
	HasChildren   bool              `json:"hasChildren"`
	Decision      string            `json:"decision"`
	Source        string            `json:"source"`
	RuleID        string            `json:"ruleId,omitempty"`
	RulePattern   string            `json:"rulePattern,omitempty"`
	Rendered      string            `json:"rendered"`
	KeepWellKnown bool              `json:"keepWellKnown"`
}

// RedactionChildren is a page of rows below one parent.
type RedactionChildren struct {
	SessionCode string          `json:"sessionCode"`
	Items       []RedactionItem `json:"items"`
	// MoreCount and MoreBytes summarize children beyond the page.
	MoreCount int   `json:"moreCount"`
	MoreBytes int64 `json:"moreBytes"`
	// FilesBytes and FilesCount describe small files aggregated directly in the parent;
	// their names are never stored, so they cannot be reviewed or sent.
	FilesBytes int64 `json:"filesBytes"`
	FilesCount int64 `json:"filesCount"`
}

const redactionPageSize = 200

// Children lists Unknown-tier children of parent (NoNode for the scan roots), largest
// first.
func (manager *Manager) Children(sessionCode string, parent scanner.NodeIndex) (
	RedactionChildren, error) {
	manager.lock.Lock()
	defer manager.lock.Unlock()
	redactor, err := manager.redactorForCode(sessionCode)
	if err != nil {
		return RedactionChildren{}, err
	}
	tree := manager.completed.Result.Tree
	var candidates []scanner.NodeIndex
	result := RedactionChildren{SessionCode: sessionCode, Items: []RedactionItem{}}
	if parent == scanner.NoNode {
		candidates = tree.Roots
	} else {
		if !manager.validNode(parent) {
			return RedactionChildren{}, errors.New("unknown item")
		}
		candidates = tree.Node(parent).Children
		parentNode := tree.Node(parent)
		if parentNode.Tier == scanner.TierUnknown {
			result.FilesBytes = parentNode.OwnBytes
			result.FilesCount = int64(parentNode.SmallFileCount)
		}
	}
	var visible []scanner.NodeIndex
	for _, candidate := range candidates {
		if tree.Node(candidate).TierBytes(scanner.TierUnknown) > 0 {
			visible = append(visible, candidate)
		}
	}
	sort.SliceStable(visible, func(first, second int) bool {
		return tree.Node(visible[first]).TierBytes(scanner.TierUnknown) >
			tree.Node(visible[second]).TierBytes(scanner.TierUnknown)
	})
	for position, index := range visible {
		if position >= redactionPageSize {
			result.MoreCount++
			result.MoreBytes += tree.Node(index).TierBytes(scanner.TierUnknown)
			continue
		}
		result.Items = append(result.Items, manager.redactionItem(redactor, index))
	}
	return result, nil
}

func (manager *Manager) redactorForCode(sessionCode string) (*redact.Redactor, error) {
	redactor, err := manager.currentRedactor()
	if err != nil {
		return nil, err
	}
	if sessionCode != manager.completed.Code {
		return nil, ErrStaleScan
	}
	return redactor, nil
}

func (manager *Manager) validNode(index scanner.NodeIndex) bool {
	return index >= 0 && int(index) < manager.completed.Result.Tree.Len()
}

var decisionNames = map[redact.Decision]string{
	redact.DecisionVisible: "visible", redact.DecisionHome: "home",
	redact.DecisionHidden: "hidden", redact.DecisionRedacted: "subtree",
	redact.DecisionRedactedKeepExtension: "keep-extension",
}

var sourceNames = map[redact.Source]string{
	redact.SourceNone: "", redact.SourceRule: "rule", redact.SourceSession: "session",
	redact.SourceAlways:   "always",
	redact.SourceAncestor: "ancestor",
}

func (manager *Manager) redactionItem(redactor *redact.Redactor,
	index scanner.NodeIndex) RedactionItem {
	tree := manager.completed.Result.Tree
	node := tree.Node(index)
	state := redactor.State(index)
	kind := "folder"
	switch node.Kind {
	case platform.EntryFile:
		kind = "file"
	case platform.EntryLink:
		kind = "link"
	}
	item := RedactionItem{
		ID:          index,
		Name:        node.Name,
		Path:        overview.DisplayPath(tree.Path(index), manager.CurrentProfile()),
		Kind:        kind,
		Bytes:       node.TierBytes(scanner.TierUnknown),
		Files:       node.TotalFiles,
		HasChildren: hasUnknownChildren(tree, node),
		Decision:    decisionNames[state.Decision],
		Source:      sourceNames[state.Source],
		Rendered:    redactor.Render(index),
	}
	if override, found := manager.redaction.overrides[index]; found {
		item.KeepWellKnown = override.KeepWellKnown
	}
	if state.Rule >= 0 && int(state.Rule) < len(manager.redaction.document.Rules) {
		rule := manager.redaction.document.Rules[state.Rule]
		item.RuleID, item.RulePattern = rule.ID, string(rule.Type)+"  "+rule.Pattern
		if state.Source == redact.SourceRule {
			item.KeepWellKnown = rule.KeepWellKnown
		}
	}
	return item
}

func hasUnknownChildren(tree *scanner.Tree, node *scanner.Node) bool {
	for _, child := range node.Children {
		if tree.Node(child).TierBytes(scanner.TierUnknown) > 0 {
			return true
		}
	}
	return false
}

// RedactionDetail is the inspector panel for one item.
type RedactionDetail struct {
	Item    RedactionItem   `json:"item"`
	Preview []RedactionItem `json:"preview"`
}

const previewLength = 4

// Detail returns an item and how its largest descendants would appear in a prompt.
func (manager *Manager) Detail(sessionCode string, index scanner.NodeIndex) (
	RedactionDetail, error) {
	manager.lock.Lock()
	defer manager.lock.Unlock()
	redactor, err := manager.redactorForCode(sessionCode)
	if err != nil {
		return RedactionDetail{}, err
	}
	if !manager.validNode(index) {
		return RedactionDetail{}, errors.New("unknown item")
	}
	detail := RedactionDetail{Item: manager.redactionItem(redactor, index),
		Preview: []RedactionItem{}}
	tree := manager.completed.Result.Tree
	frontier := []scanner.NodeIndex{index}
	for len(frontier) > 0 && len(detail.Preview) < previewLength {
		current := frontier[0]
		frontier = frontier[1:]
		children := append([]scanner.NodeIndex(nil), tree.Node(current).Children...)
		sort.Slice(children, func(first, second int) bool {
			return tree.Node(children[first]).TierBytes(scanner.TierUnknown) >
				tree.Node(children[second]).TierBytes(scanner.TierUnknown)
		})
		for _, child := range children {
			if tree.Node(child).TierBytes(scanner.TierUnknown) == 0 {
				continue
			}
			if len(detail.Preview) < previewLength {
				detail.Preview = append(detail.Preview, manager.redactionItem(redactor, child))
			}
			frontier = append(frontier, child)
		}
	}
	return detail, nil
}

// OverrideRequest sets or clears a session-only treatment for one item.
type OverrideRequest struct {
	SessionCode   string            `json:"sessionCode"`
	ID            scanner.NodeIndex `json:"id"`
	Treatment     string            `json:"treatment"`
	KeepWellKnown bool              `json:"keepWellKnown"`
}

// SetOverride applies a choice from the redaction review. Treatment "default" clears it.
func (manager *Manager) SetOverride(request OverrideRequest) error {
	manager.lock.Lock()
	defer manager.lock.Unlock()
	if _, err := manager.redactorForCode(request.SessionCode); err != nil {
		return err
	}
	if !manager.validNode(request.ID) {
		return errors.New("unknown item")
	}
	switch redact.Treatment(request.Treatment) {
	case redact.TreatmentSubtree, redact.TreatmentKeepExtension, redact.TreatmentNone:
		manager.redaction.overrides[request.ID] = redact.Override{
			Treatment:     redact.Treatment(request.Treatment),
			KeepWellKnown: request.KeepWellKnown,
		}
	case "default":
		delete(manager.redaction.overrides, request.ID)
	default:
		return errors.New("treatment must be subtree, keep-extension, none or default")
	}
	manager.redaction.redactor = nil
	return nil
}

// RuleView is one row of the rules manager.
type RuleView struct {
	redact.Rule
	Matches    int      `json:"matches"`
	MatchBytes int64    `json:"matchBytes"`
	Hits       []string `json:"hits"`
	// InScan is false when no finished scan exists, so match counts are unknown.
	InScan bool `json:"inScan"`
}

const maximumHitsShown = 5

// Rules lists the rules with their matches in the current scan.
func (manager *Manager) Rules() ([]RuleView, error) {
	manager.lock.Lock()
	defer manager.lock.Unlock()
	if manager.redaction.loadError != nil {
		return nil, ErrRulesUnavailable
	}
	views := make([]RuleView, len(manager.redaction.document.Rules))
	for index, rule := range manager.redaction.document.Rules {
		views[index] = RuleView{Rule: rule, Hits: []string{}}
	}
	redactor, err := manager.currentRedactor()
	if err != nil {
		return views, nil
	}
	tree := manager.completed.Result.Tree
	for index, match := range redactor.Matches() {
		views[index].InScan = true
		views[index].Matches = len(match.Nodes)
		views[index].MatchBytes = match.Bytes
		for _, node := range match.Nodes {
			if len(views[index].Hits) == maximumHitsShown {
				break
			}
			views[index].Hits = append(views[index].Hits,
				overview.DisplayPath(tree.Path(node), manager.CurrentProfile()))
		}
	}
	return views, nil
}

// RuleRequest creates or updates a rule. With FromNode set (creating from the redaction
// review), the pattern and anchor come from that item.
type RuleRequest struct {
	ID            string             `json:"id"`
	Type          redact.RuleType    `json:"type"`
	Pattern       string             `json:"pattern"`
	Treatment     redact.Treatment   `json:"treatment"`
	KeepWellKnown bool               `json:"keepWellKnown"`
	SessionCode   string             `json:"sessionCode"`
	FromNode      *scanner.NodeIndex `json:"fromNode"`
}

// SaveRule creates a rule (empty ID) or updates an existing one.
func (manager *Manager) SaveRule(request RuleRequest) (redact.Rule, error) {
	manager.lock.Lock()
	defer manager.lock.Unlock()
	if manager.redaction.loadError != nil {
		return redact.Rule{}, ErrRulesUnavailable
	}
	rule := redact.Rule{ID: request.ID, Type: request.Type,
		Pattern: strings.TrimSpace(request.Pattern), Treatment: request.Treatment,
		KeepWellKnown: request.KeepWellKnown, Created: time.Now().UTC()}
	if request.FromNode != nil {
		if err := manager.fillRuleFromNode(&rule, request); err != nil {
			return redact.Rule{}, err
		}
	}
	if err := rule.Validate(); err != nil {
		return redact.Rule{}, err
	}
	rules := manager.redaction.document.Rules
	if rule.ID == "" {
		rule.ID = redact.NewRuleID()
		rules = append(rules, rule)
	} else {
		position := ruleIndex(rules, rule.ID)
		if position < 0 {
			return redact.Rule{}, errors.New("no rule with this id")
		}
		previous := rules[position]
		rule.Created, rule.LastMatched = previous.Created, previous.LastMatched
		if rule.Pattern == previous.Pattern && rule.Type == previous.Type {
			rule.Anchor = previous.Anchor
		}
		rules = append(append(append([]redact.Rule{}, rules[:position]...), rule),
			rules[position+1:]...)
	}
	return rule, manager.commitRules(rules)
}

func (manager *Manager) fillRuleFromNode(rule *redact.Rule, request RuleRequest) error {
	if _, err := manager.redactorForCode(request.SessionCode); err != nil {
		return err
	}
	if !manager.validNode(*request.FromNode) {
		return errors.New("unknown item")
	}
	tree := manager.completed.Result.Tree
	node := tree.Node(*request.FromNode)
	switch rule.Type {
	case redact.RuleName:
		rule.Pattern = node.Name
	case redact.RuleExact, redact.RuleSubtree, redact.RuleGlob:
		rule.Pattern = overview.DisplayPath(tree.Path(*request.FromNode),
			manager.CurrentProfile())
	}
	if (rule.Type == redact.RuleExact || rule.Type == redact.RuleSubtree) && !node.ID.IsZero() {
		anchor := node.ID
		rule.Anchor = &anchor
	}
	// The remembered rule replaces the session choice it was made from.
	delete(manager.redaction.overrides, *request.FromNode)
	return nil
}

// DeleteRule removes a rule.
func (manager *Manager) DeleteRule(id string) error {
	manager.lock.Lock()
	defer manager.lock.Unlock()
	if manager.redaction.loadError != nil {
		return ErrRulesUnavailable
	}
	rules := manager.redaction.document.Rules
	position := ruleIndex(rules, id)
	if position < 0 {
		return errors.New("no rule with this id")
	}
	return manager.commitRules(append(append([]redact.Rule{}, rules[:position]...),
		rules[position+1:]...))
}

func ruleIndex(rules []redact.Rule, id string) int {
	for index, rule := range rules {
		if rule.ID == id {
			return index
		}
	}
	return -1
}

func (manager *Manager) commitRules(rules []redact.Rule) error {
	previous := manager.redaction.document.Rules
	manager.redaction.document.Rules = rules
	if err := manager.saveRules(); err != nil {
		manager.redaction.document.Rules = previous
		return err
	}
	manager.redaction.redactor = nil
	return nil
}

// ResetUnreadableRules moves an unreadable rules file aside and starts with no rules. It
// only runs when the user explicitly confirms; it never runs automatically.
func (manager *Manager) ResetUnreadableRules() (string, error) {
	manager.lock.Lock()
	defer manager.lock.Unlock()
	if manager.redaction.loadError == nil {
		return "", errors.New("the rules file is readable; nothing to reset")
	}
	path := redact.StorePath(manager.environment.ConfigDirectory)
	aside := path + ".unreadable-" + time.Now().UTC().Format("20060102-150405")
	if err := os.Rename(path, aside); err != nil {
		return "", err
	}
	manager.loadRules()
	manager.resetRedactionForNewScan()
	return aside, manager.redaction.loadError
}
