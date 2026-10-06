package response

import "strings"

// characterReplacements undoes what chat interfaces and word processors do to text:
// curly quotes, typographic dashes, non-breaking spaces, and look-alike pipes.
var characterReplacements = strings.NewReplacer(
	"\u201C", `"`, "\u201D", `"`, "\u201E", `"`, "\u201F", `"`, "\u2033", `"`,
	"\u2018", "'", "\u2019", "'", "\u201A", "'", "\u201B", "'", "\u2032", "'",
	"\u2010", "-", "\u2011", "-", "\u2012", "-", "\u2013", "-", "\u2014", "-", "\u2212", "-",
	"\u00A0", " ", "\u2007", " ", "\u2009", " ", "\u200A", " ", "\u202F", " ", "\u3000", " ",
	"\uFF5C", "|", "\u2502", "|",
	// Zero-width characters are removed outright.
	"\u200B", "", "\u200C", "", "\u200D", "", "\u2060", "", "\uFEFF", "",
	"\r\n", "\n", "\r", "\n",
)

// normalizeLines returns trimmed lines with blockquote markers removed, as pasted text
// often arrives quoted ("> ") from an email or a chat export.
func normalizeLines(text string) []string {
	lines := strings.Split(characterReplacements.Replace(text), "\n")
	for index, line := range lines {
		line = strings.TrimSpace(line)
		for strings.HasPrefix(line, ">") {
			line = strings.TrimSpace(line[1:])
		}
		lines[index] = line
	}
	return lines
}
