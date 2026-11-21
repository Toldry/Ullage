package redact

import (
	"regexp"
	"strings"
	"unicode"
)

// shortNameLength is the length below which a sensitive name only matches as a whole word.
// A user named "Al" must not redact AppData/Local (decision D-028).
const shortNameLength = 4

// containsSensitive reports whether name contains the sensitive word: as a substring for
// words of 4 or more characters, as a whole word otherwise.
func containsSensitive(name, sensitive string) bool {
	if sensitive == "" {
		return false
	}
	lowerName, lowerSensitive := strings.ToLower(name), strings.ToLower(sensitive)
	if len([]rune(sensitive)) >= shortNameLength {
		return strings.Contains(lowerName, lowerSensitive)
	}
	for _, word := range splitWords(name) {
		if strings.ToLower(word) == lowerSensitive {
			return true
		}
	}
	return false
}

// splitWords splits at non-alphanumeric characters and at lower-to-upper case changes,
// so "maraNotes", "mara-notes" and "mara_notes" all contain the word "mara".
func splitWords(name string) []string {
	var words []string
	var current []rune
	var previous rune
	flush := func() {
		if len(current) > 0 {
			words = append(words, string(current))
			current = current[:0]
		}
	}
	for _, character := range name {
		isWordCharacter := unicode.IsLetter(character) || unicode.IsDigit(character)
		switch {
		case !isWordCharacter:
			flush()
		case unicode.IsUpper(character) && unicode.IsLower(previous):
			flush()
			current = append(current, character)
		default:
			current = append(current, character)
		}
		previous = character
	}
	flush()
	return words
}

// windowsSecurityIdentifier matches SIDs such as S-1-5-21-3623811015-3361044348-30300820-1013
// that appear in folder names like $Recycle.Bin\S-1-5-21-....
var windowsSecurityIdentifier = regexp.MustCompile(`(?i)s-1-\d+(-\d+){2,}`)

// wellKnownNames are generic folder names that identify a kind of content but nothing
// about the user. Inside a redacted subtree they may stay readable so the AI can still
// recognize them (decision D-026).
var wellKnownNames = map[string]bool{
	"build": true, "dist": true, "out": true, "bin": true, "obj": true, "target": true,
	"node_modules": true, ".venv": true, "venv": true, "__pycache__": true, ".git": true,
	".cache": true, "cache": true, "caches": true, "tmp": true, "temp": true, "logs": true,
	"log": true, "backup": true, "backups": true, "packages": true, ".gradle": true,
	".next": true, "coverage": true, "thumbnails": true, "proxies": true, "renders": true,
	// The standard layout of a user profile, the same on every PC. Without these a hidden
	// home folder could not be split at all, and became one line with only a size
	// (decision D-075).
	"appdata": true, "local": true, "locallow": true, "roaming": true, "desktop": true,
	"documents": true, "downloads": true, "music": true, "pictures": true, "videos": true,
	"movies": true, "saved games": true, "favorites": true, "onedrive": true,
	"microsoft": true, "windows": true, "programs": true, "library": true,
	"application support": true, "containers": true, ".local": true, "share": true,
	".config": true, "trash": true, ".trash": true, "snap": true, ".npm": true,
	".cargo": true, ".rustup": true, ".m2": true, ".nuget": true, ".docker": true,
	".android": true, ".vscode": true, ".conda": true,
}

func isWellKnown(name string) bool {
	return wellKnownNames[strings.ToLower(name)]
}

// sanitize makes a visible name safe to embed: it cannot imitate a placeholder or break
// the one-line-per-item format. '<', '>' and '|' never occur in Windows names but can on
// Linux; control characters can occur on both. Invisible format characters (bidi
// overrides such as U+202E, zero-width spaces) are replaced too, because they can make a
// name display differently from what it is.
func sanitize(name string) string {
	return strings.Map(func(character rune) rune {
		switch {
		case character == '<' || character == '>' || character == '|':
			return '_'
		case unicode.IsControl(character) || unicode.Is(unicode.Cf, character):
			return '_'
		default:
			return character
		}
	}, name)
}
