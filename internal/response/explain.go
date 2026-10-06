package response

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Explanation is one validated round B entry. Its fields are free text written by the
// assistant: they are only ever displayed, never interpreted or executed.
type Explanation struct {
	Number int      `json:"number"`
	What   string   `json:"what"`
	Lost   string   `json:"lost"`
	Steps  []string `json:"steps"`
	Line   int      `json:"line"`
}

// ExplanationExpectation describes the round B prompt a reply must answer.
type ExplanationExpectation struct {
	Code  string
	Round int
	// Numbers are the categories the prompt asked about.
	Numbers []int
}

// ExplanationResult is the outcome of parsing a round B reply. Missing categories are
// not a problem: they stay queued for a later round.
type ExplanationResult struct {
	Found        bool          `json:"found"`
	FoundCode    string        `json:"foundCode"`
	Truncated    bool          `json:"truncated"`
	Explanations []Explanation `json:"explanations"`
	Missing      []int         `json:"missing"`
	Problems     []Problem     `json:"problems"`
}

// Valid reports whether the reply can be imported.
func (result ExplanationResult) Valid() bool {
	return result.Found && len(result.Problems) == 0
}

// The prompt asks for about 25 words per field. Assistants overshoot a little, so the
// hard limits leave room; beyond them the reply is sent back for repair.
const (
	ExplanationWordTarget   = 25
	maximumExplanationWords = 40
	maximumExplanationRunes = 320
	MaximumExplanationSteps = 5
)

var (
	categoryMarker = regexp.MustCompile(`^#+\s*#?\s*(\d+)\b`)
	fieldLine      = regexp.MustCompile(`(?i)^(what|lost|step(?:\s*\d+)?)\s*[:.)-]\s*(.*)$`)
)

// explanationBlock collects the fields of one #N entry while parsing.
type explanationBlock struct {
	explanation Explanation
	whatCount   int
	lostCount   int
}

// ParseExplanations extracts the last @SESSION ... @END block and validates it as a round B
// reply.
func ParseExplanations(text string, expectation ExplanationExpectation) ExplanationResult {
	result := ExplanationResult{Explanations: []Explanation{}, Problems: []Problem{},
		Missing: []int{}}
	lines := normalizeLines(text)
	start, end := locateBlock(lines)
	if start < 0 {
		result.Problems = append(result.Problems, Problem{Check: CheckSessionCode,
			Message: fmt.Sprintf("no @SESSION %s line was found", expectation.Code)})
		return result
	}
	result.Found = true
	codes := Result{}
	codes.FoundCode = sessionMarker.FindStringSubmatch(markerText(lines[start]))[1]
	codes.checkCodes(lines, start, end, Expectation{Code: expectation.Code})
	result.FoundCode = codes.FoundCode
	result.Problems = append(result.Problems, codes.Problems...)
	if end < 0 {
		result.Truncated = true
		result.Problems = append(result.Problems, Problem{Check: CheckComplete,
			Message: fmt.Sprintf("the reply was cut off before @END %s", expectation.Code)})
		end = len(lines)
	}
	blocks := result.collectBlocks(lines, start+1, end)
	result.validateBlocks(blocks, expectation)
	return result
}

func (result *ExplanationResult) add(lineNumber int, subject string, check Check,
	message string) {
	result.Problems = append(result.Problems, Problem{Line: lineNumber, Subject: subject,
		Check: check, Message: message})
}

func (result *ExplanationResult) collectBlocks(lines []string, from, to int) []*explanationBlock {
	var blocks []*explanationBlock
	var current *explanationBlock
	for index := from; index < to; index++ {
		line := strings.TrimSpace(strings.ReplaceAll(cleanRow(lines[index]), "**", ""))
		if line == "" {
			continue
		}
		if match := categoryMarker.FindStringSubmatch(line); match != nil {
			number, _ := strconv.Atoi(match[1])
			current = &explanationBlock{explanation: Explanation{Number: number,
				Line: index + 1, Steps: []string{}}}
			blocks = append(blocks, current)
			continue
		}
		match := fieldLine.FindStringSubmatch(line)
		if match == nil {
			result.add(index+1, shorten(line), CheckFormat,
				"expected #number, what:, lost: or step:")
			continue
		}
		if current == nil {
			result.add(index+1, shorten(line), CheckFormat,
				"this line comes before the first #number line")
			continue
		}
		subject := "#" + strconv.Itoa(current.explanation.Number)
		value, problem := cleanExplanationText(match[2])
		if problem != "" {
			result.add(index+1, subject, CheckFormat,
				strings.ToLower(fieldName(match[1]))+": "+problem)
		}
		switch strings.ToLower(fieldName(match[1])) {
		case "what":
			current.whatCount++
			current.explanation.What = value
		case "lost":
			current.lostCount++
			current.explanation.Lost = value
		default:
			current.explanation.Steps = append(current.explanation.Steps, value)
		}
	}
	return blocks
}

func fieldName(key string) string {
	if strings.HasPrefix(strings.ToLower(key), "step") {
		return "step"
	}
	return key
}

func (result *ExplanationResult) validateBlocks(blocks []*explanationBlock,
	expectation ExplanationExpectation) {
	expected := map[int]bool{}
	for _, number := range expectation.Numbers {
		expected[number] = true
	}
	seen := map[int]bool{}
	problemsBefore := len(result.Problems)
	for _, block := range blocks {
		explanation := block.explanation
		subject := "#" + strconv.Itoa(explanation.Number)
		blockProblems := len(result.Problems)
		switch {
		case !expected[explanation.Number]:
			result.add(explanation.Line, subject, CheckItemIDs, fmt.Sprintf(
				"category #%d was not asked about; the categories are %s", explanation.Number,
				listNumbers(expectation.Numbers)))
		case seen[explanation.Number]:
			result.add(explanation.Line, subject, CheckDuplicates, fmt.Sprintf(
				"category #%d is explained twice", explanation.Number))
		}
		seen[explanation.Number] = true
		if block.whatCount != 1 {
			result.add(explanation.Line, subject, CheckFormat, "needs exactly one what: line")
		}
		if block.lostCount != 1 {
			result.add(explanation.Line, subject, CheckFormat, "needs exactly one lost: line")
		}
		if len(explanation.Steps) == 0 || len(explanation.Steps) > MaximumExplanationSteps {
			result.add(explanation.Line, subject, CheckFormat, fmt.Sprintf(
				"needs 1 to %d step: lines", MaximumExplanationSteps))
		}
		if len(result.Problems) == blockProblems {
			result.Explanations = append(result.Explanations, explanation)
		}
	}
	if len(blocks) == 0 && len(result.Problems) == problemsBefore && !result.Truncated {
		result.add(0, "", CheckFormat, "the reply explains no category")
	}
	for _, number := range expectation.Numbers {
		if !seen[number] {
			result.Missing = append(result.Missing, number)
		}
	}
}

// cleanExplanationText removes control and format characters (which could reorder how
// the text is displayed) and checks the length limits.
func cleanExplanationText(text string) (string, string) {
	cleaned := strings.Map(func(character rune) rune {
		if unicode.IsControl(character) || unicode.Is(unicode.Cf, character) {
			return -1
		}
		return character
	}, text)
	cleaned = strings.Join(strings.Fields(cleaned), " ")
	switch {
	case cleaned == "":
		return "", "is empty"
	case len(strings.Fields(cleaned)) > maximumExplanationWords:
		return cleaned, fmt.Sprintf("is longer than %d words; keep it under %d",
			maximumExplanationWords, ExplanationWordTarget)
	case len([]rune(cleaned)) > maximumExplanationRunes:
		return cleaned, fmt.Sprintf("is longer than %d characters", maximumExplanationRunes)
	}
	return cleaned, ""
}

func listNumbers(numbers []int) string {
	parts := make([]string, len(numbers))
	for index, number := range numbers {
		parts[index] = "#" + strconv.Itoa(number)
	}
	return strings.Join(parts, ", ")
}
