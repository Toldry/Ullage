package response

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// Each category row is followed by its explanation: what the data is, what deleting it
// loses, and the safest way to free the space. The prompt asks for about 25 words per
// line; assistants overshoot a little, so the hard limits leave room, and beyond them the
// reply goes back for repair. The text is only ever displayed, never interpreted.
const (
	ExplanationWordTarget   = 25
	maximumExplanationWords = 40
	maximumExplanationRunes = 320
	MaximumExplanationSteps = 5
)

// fieldLine needs a colon, or a numbered step ("Step 2." or "step 2)"), so a category
// row such as "Lost-and-found files|..." is never taken for an explanation line.
var fieldLine = regexp.MustCompile(`(?i)^(what|lost|step)(?:\s*:|\s*\d+\s*[:.)])\s*(.*)$`)

// field is one what:, lost: or step: line.
type field struct {
	name  string
	value string
}

// parseField recognizes an explanation line; ok is false for anything else.
func parseField(line string) (field, string, bool) {
	match := fieldLine.FindStringSubmatch(strings.ReplaceAll(line, "**", ""))
	if match == nil {
		return field{}, "", false
	}
	name := strings.ToLower(match[1])
	value, problem := cleanExplanationText(match[2])
	return field{name: name, value: value}, problem, true
}

// addField attaches a field to a category, reporting repeated what: or lost: lines.
func (category *Category) addField(item field) string {
	switch item.name {
	case "what":
		if category.What != "" {
			return "has more than one what: line"
		}
		category.What = item.value
	case "lost":
		if category.Lost != "" {
			return "has more than one lost: line"
		}
		category.Lost = item.value
	default:
		category.Steps = append(category.Steps, item.value)
	}
	return ""
}

// explanationProblem checks that a deletable category is explained. Categories to keep
// may carry an explanation but need none.
func (category Category) explanationProblem() string {
	if len(category.Steps) > MaximumExplanationSteps {
		return fmt.Sprintf("has more than %d step: lines", MaximumExplanationSteps)
	}
	if !category.Deletable {
		return ""
	}
	var missing []string
	if category.What == "" {
		missing = append(missing, "what:")
	}
	if category.Lost == "" {
		missing = append(missing, "lost:")
	}
	if len(category.Steps) == 0 {
		missing = append(missing, "step:")
	}
	if len(missing) > 0 {
		return "has del 1 but no " + strings.Join(missing, ", ") + " line below it"
	}
	return ""
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
