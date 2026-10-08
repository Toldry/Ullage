// Package response parses a pasted assistant reply. It is tolerant about how the reply is
// wrapped (prose, Markdown fences, smart quotes) and strict about what it contains: only
// closed enums and IDs of the current batch survive. Nothing here can name a path or an
// action; the program builds every action itself from validated IDs (safety rule 3).
package response

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Confidence and Regenerable are the closed vocabularies a reply may use.
type Confidence string

const (
	ConfidenceHigh   Confidence = "high"
	ConfidenceMedium Confidence = "med"
	ConfidenceLow    Confidence = "low"
)

type Regenerable string

const (
	RegenerableYes       Regenerable = "yes"
	RegenerablePartially Regenerable = "partially"
	RegenerableNo        Regenerable = "no"
)

// Category is one validated row.
type Category struct {
	Name        string      `json:"name"`
	Confidence  Confidence  `json:"confidence"`
	Regenerable Regenerable `json:"regenerable"`
	Deletable   bool        `json:"deletable"`
	IDs         []int       `json:"ids"`
	Line        int         `json:"line"`
	// The explanation written below the row; required when Deletable.
	What  string   `json:"what,omitempty"`
	Lost  string   `json:"lost,omitempty"`
	Steps []string `json:"steps,omitempty"`
}

// Problem is one validation error, phrased so it can go straight into a repair prompt.
type Problem struct {
	Line    int    `json:"line"`
	Subject string `json:"subject"`
	Message string `json:"message"`
	Check   Check  `json:"check"`
}

// Check groups problems for the checklist on the Paste screen.
type Check string

const (
	CheckSessionCode Check = "session"
	CheckComplete    Check = "complete"
	CheckFormat      Check = "format"
	CheckItemIDs     Check = "ids"
	CheckDuplicates  Check = "duplicates"
	CheckExplained   Check = "explained"
)

// AllChecks is the order the checklist shows.
var AllChecks = []Check{CheckSessionCode, CheckComplete, CheckFormat, CheckItemIDs,
	CheckDuplicates, CheckExplained}

// Expectation describes the batch a reply must answer.
type Expectation struct {
	Code       string
	Batch      int
	BatchCount int
	FirstID    int
	LastID     int
}

// Result is the outcome of parsing. It is importable only when Problems is empty.
type Result struct {
	Found      bool       `json:"found"`
	FoundCode  string     `json:"foundCode"`
	Truncated  bool       `json:"truncated"`
	Categories []Category `json:"categories"`
	Problems   []Problem  `json:"problems"`
	Classified int        `json:"classified"`
}

// Valid reports whether the reply can be imported.
func (result Result) Valid() bool {
	return result.Found && len(result.Problems) == 0
}

// Failed reports whether any problem belongs to the check.
func (result Result) Failed(check Check) bool {
	for _, problem := range result.Problems {
		if problem.Check == check {
			return true
		}
	}
	return false
}

const maximumCategoryNameLength = 80

var (
	sessionMarker = regexp.MustCompile(`(?i)^@session\b\s*(\S*)`)
	endMarker     = regexp.MustCompile(`(?i)^@end\b\s*(\S*)`)
	headerRow     = regexp.MustCompile(`(?i)^cat\s*\|\s*conf\s*\|\s*regen\s*\|\s*del\s*\|\s*ids$`)
	tableRule     = regexp.MustCompile(`^[\s|:\-]+$`)
	singleID      = regexp.MustCompile(`^\d+$`)
	idRange       = regexp.MustCompile(`^(\d+)\s*-\s*(\d+)$`)
)

// Parse extracts the last @SESSION ... @END block from text and validates it.
func Parse(text string, expectation Expectation) Result {
	result := Result{Categories: []Category{}, Problems: []Problem{}}
	lines := normalizeLines(text)
	start, end := locateBlock(lines)
	if start < 0 {
		result.add(Problem{Check: CheckSessionCode, Message: fmt.Sprintf(
			"no @SESSION %s line was found", expectation.Code)})
		return result
	}
	result.Found = true
	result.FoundCode = sessionMarker.FindStringSubmatch(markerText(lines[start]))[1]
	result.checkCodes(lines, start, end, expectation)
	if end < 0 {
		result.Truncated = true
		result.add(Problem{Check: CheckComplete, Message: fmt.Sprintf(
			"the reply was cut off before @END %s", expectation.Code)})
		end = len(lines)
	}
	result.parseRows(lines, start+1, end, expectation)
	result.checkDuplicates()
	for _, category := range result.Categories {
		result.Classified += len(category.IDs)
	}
	return result
}

func (result *Result) add(problem Problem) {
	result.Problems = append(result.Problems, problem)
}

// locateBlock returns the last @END and the last @SESSION before it, so a reply pasted
// together with the prompt (which contains an example block) still resolves to the reply.
// end is -1 when no @END follows the @SESSION line.
func locateBlock(lines []string) (int, int) {
	lastEnd := -1
	for index := len(lines) - 1; index >= 0; index-- {
		if endMarker.MatchString(markerText(lines[index])) {
			lastEnd = index
			break
		}
	}
	searchFrom := len(lines) - 1
	if lastEnd >= 0 {
		searchFrom = lastEnd - 1
	}
	for index := searchFrom; index >= 0; index-- {
		if sessionMarker.MatchString(markerText(lines[index])) {
			if lastEnd < 0 {
				return index, -1
			}
			return index, lastEnd
		}
	}
	// No @SESSION at all, or only an @END: the start of the block was not copied.
	return -1, -1
}

// markerText strips Markdown emphasis that assistants sometimes put around markers.
func markerText(line string) string {
	return strings.Trim(line, "*_`# ")
}

func (result *Result) checkCodes(lines []string, start, end int, expectation Expectation) {
	if result.FoundCode == "" {
		result.add(Problem{Line: start + 1, Check: CheckSessionCode, Message: fmt.Sprintf(
			"the @SESSION line has no code; it must be @SESSION %s", expectation.Code)})
	} else if !strings.EqualFold(result.FoundCode, expectation.Code) {
		result.add(Problem{Line: start + 1, Check: CheckSessionCode, Message: fmt.Sprintf(
			"the session code is %s but must be %s", result.FoundCode, expectation.Code)})
	}
	if end >= 0 {
		endCode := endMarker.FindStringSubmatch(markerText(lines[end]))[1]
		if endCode != "" && !strings.EqualFold(endCode, expectation.Code) {
			result.add(Problem{Line: end + 1, Check: CheckSessionCode, Message: fmt.Sprintf(
				"the @END code is %s but must be %s", endCode, expectation.Code)})
		}
	}
}

// parseRows reads category rows and the explanation lines below each. Explanation lines
// are recognized by their prefix first, because their text may contain '|'.
func (result *Result) parseRows(lines []string, from, to int, expectation Expectation) {
	// current is the row the next explanation lines belong to; nil before the first row.
	// A rejected row still owns its lines, so they do not add problems of their own.
	var current *Category
	rejected := false
	for index := from; index < to; index++ {
		line := cleanRow(lines[index])
		if line == "" || headerRow.MatchString(line) || tableRule.MatchString(line) {
			continue
		}
		if item, problem, ok := parseField(line); ok {
			switch {
			case current == nil && !rejected:
				result.add(Problem{Line: index + 1, Subject: shorten(line), Check: CheckFormat,
					Message: "this explanation line comes before the first category row"})
			case current != nil:
				if problem == "" {
					problem = current.addField(item)
				} else {
					problem = item.name + ": " + problem
				}
				if problem != "" {
					result.add(Problem{Line: index + 1, Subject: shorten(current.Name),
						Check: CheckExplained, Message: problem})
				}
			}
			continue
		}
		result.flushExplanation(current)
		current, rejected = nil, false
		if category, ok := result.parseRow(line, index+1, expectation); ok {
			result.Categories = append(result.Categories, category)
			current = &result.Categories[len(result.Categories)-1]
		} else {
			rejected = true
		}
	}
	result.flushExplanation(current)
}

// flushExplanation checks the explanation of the row just finished.
func (result *Result) flushExplanation(category *Category) {
	if category == nil {
		return
	}
	if problem := category.explanationProblem(); problem != "" {
		result.add(Problem{Line: category.Line, Subject: shorten(category.Name),
			Check: CheckExplained, Message: problem})
	}
}

// cleanRow removes list bullets and the outer pipes of Markdown table rows.
func cleanRow(line string) string {
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "```") {
		return ""
	}
	for _, bullet := range []string{"- ", "* ", "• "} {
		line = strings.TrimPrefix(line, bullet)
	}
	if strings.HasPrefix(line, "|") && strings.HasSuffix(line, "|") && len(line) > 1 {
		line = strings.TrimSpace(line[1 : len(line)-1])
	}
	return line
}

// parseRow splits from the right, at most four times, because category names come from
// the assistant and may contain '|' (file names cannot).
func (result *Result) parseRow(line string, lineNumber int, expectation Expectation) (
	Category, bool) {
	parts := splitFromRight(line, "|", 5)
	if len(parts) < 5 {
		result.add(Problem{Line: lineNumber, Subject: shorten(line), Check: CheckFormat,
			Message: "expected 5 columns cat|conf|regen|del|ids"})
		return Category{}, false
	}
	name := strings.Trim(strings.TrimSpace(parts[0]), "\"'*`")
	category := Category{Name: name, Line: lineNumber}
	problemsBefore := len(result.Problems)
	formatProblem := func(message string) {
		result.add(Problem{Line: lineNumber, Subject: shorten(name), Check: CheckFormat,
			Message: message})
	}
	switch {
	case name == "":
		formatProblem("the category name is empty")
	case len([]rune(name)) > maximumCategoryNameLength:
		formatProblem(fmt.Sprintf("the category name is longer than %d characters",
			maximumCategoryNameLength))
	}
	confidence := strings.ToLower(strings.TrimSpace(parts[1]))
	switch Confidence(confidence) {
	case ConfidenceHigh, ConfidenceMedium, ConfidenceLow:
		category.Confidence = Confidence(confidence)
	default:
		formatProblem(fmt.Sprintf("conf must be high, med or low, not %q", parts[1]))
	}
	regenerable := strings.ToLower(strings.TrimSpace(parts[2]))
	switch Regenerable(regenerable) {
	case RegenerableYes, RegenerablePartially, RegenerableNo:
		category.Regenerable = Regenerable(regenerable)
	default:
		formatProblem(fmt.Sprintf("regen must be yes, partially or no, not %q", parts[2]))
	}
	switch strings.TrimSpace(parts[3]) {
	case "1":
		category.Deletable = true
	case "0":
	default:
		formatProblem(fmt.Sprintf("del must be 1 or 0, not %q", parts[3]))
	}
	ids, idProblems := parseIDs(parts[4], expectation)
	for _, message := range idProblems {
		result.add(Problem{Line: lineNumber, Subject: shorten(name), Check: CheckItemIDs,
			Message: message})
	}
	category.IDs = ids
	return category, len(result.Problems) == problemsBefore
}

func splitFromRight(text, separator string, parts int) []string {
	var result []string
	remaining := text
	for len(result) < parts-1 {
		index := strings.LastIndex(remaining, separator)
		if index < 0 {
			break
		}
		result = append([]string{remaining[index+len(separator):]}, result...)
		remaining = remaining[:index]
	}
	return append([]string{remaining}, result...)
}

// parseIDs accepts "3", "3-33" and comma-separated lists of both. It never expands a
// range before checking its bounds, so "1-999999999" cannot exhaust memory.
func parseIDs(text string, expectation Expectation) ([]int, []string) {
	var ids []int
	var problems []string
	seen := map[int]bool{}
	var outside []string
	for _, token := range strings.Split(text, ",") {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		first, last, ok := parseIDToken(token)
		if !ok {
			problems = append(problems, fmt.Sprintf("%q is not an ID or a range like 3-33",
				token))
			continue
		}
		if first < expectation.FirstID || last > expectation.LastID {
			outside = append(outside, token)
			continue
		}
		for id := first; id <= last; id++ {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	if len(outside) > 0 {
		problems = append(problems, fmt.Sprintf("%s not in this batch, which has IDs %d-%d",
			describeTokens(outside), expectation.FirstID, expectation.LastID))
	}
	if len(ids) == 0 && len(problems) == 0 {
		problems = append(problems, "the row lists no IDs")
	}
	return ids, problems
}

func parseIDToken(token string) (int, int, bool) {
	if singleID.MatchString(token) {
		value, err := strconv.Atoi(token)
		return value, value, err == nil
	}
	if match := idRange.FindStringSubmatch(token); match != nil {
		first, firstErr := strconv.Atoi(match[1])
		last, lastErr := strconv.Atoi(match[2])
		if firstErr == nil && lastErr == nil && first <= last {
			return first, last, true
		}
	}
	return 0, 0, false
}

func describeTokens(tokens []string) string {
	const shown = 5
	text := strings.Join(tokens[:min(len(tokens), shown)], ", ")
	if len(tokens) > shown {
		text += fmt.Sprintf(" and %d more", len(tokens)-shown)
	}
	if len(tokens) == 1 && !strings.Contains(tokens[0], "-") {
		return "ID " + text + " is"
	}
	return "IDs " + text + " are"
}

// checkDuplicates reports IDs claimed by more than one category, grouped per pair of
// rows so the repair prompt stays short.
func (result *Result) checkDuplicates() {
	owner := map[int]int{}
	type pair struct{ first, second int }
	conflicts := map[pair][]int{}
	var order []pair
	for index, category := range result.Categories {
		for _, id := range category.IDs {
			previous, taken := owner[id]
			if !taken {
				owner[id] = index
				continue
			}
			key := pair{previous, index}
			if _, known := conflicts[key]; !known {
				order = append(order, key)
			}
			conflicts[key] = append(conflicts[key], id)
		}
	}
	for _, key := range order {
		first, second := result.Categories[key.first], result.Categories[key.second]
		result.add(Problem{Line: second.Line, Subject: shorten(second.Name),
			Check: CheckDuplicates, Message: fmt.Sprintf("%s also in %q; each ID belongs "+
				"in one category", compressIDs(conflicts[key]), first.Name)})
	}
}

// compressIDs renders sorted runs as ranges: [3 4 5 9] → "IDs 3-5, 9".
func compressIDs(ids []int) string {
	var parts []string
	for index := 0; index < len(ids); {
		end := index
		for end+1 < len(ids) && ids[end+1] == ids[end]+1 {
			end++
		}
		if end > index {
			parts = append(parts, fmt.Sprintf("%d-%d", ids[index], ids[end]))
		} else {
			parts = append(parts, strconv.Itoa(ids[index]))
		}
		index = end + 1
	}
	if len(ids) == 1 {
		return "ID " + parts[0] + " is"
	}
	return "IDs " + strings.Join(parts, ", ") + " are"
}

func shorten(text string) string {
	runes := []rune(text)
	if len(runes) <= 40 {
		return text
	}
	return string(runes[:39]) + "…"
}
