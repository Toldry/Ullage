package tiers

import (
	"strings"

	"github.com/toldry/ullage/internal/pathpattern"
	"github.com/toldry/ullage/internal/platform"
)

// PathMatcher tests full paths against user-written patterns such as "~/vault/**",
// "D:/vm/**" or a bare folder name like "secrets", which matches at any depth.
type PathMatcher struct {
	patterns *pathpattern.Set
}

// NewPathMatcher compiles user patterns. "~" stands for the current user's profile.
func NewPathMatcher(patterns []string, locations platform.KnownLocations) *PathMatcher {
	matcher := &PathMatcher{patterns: pathpattern.NewSet(locations.CaseInsensitive)}
	profiles := locations.Variables["CurrentUserProfile"]
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		if !strings.ContainsAny(pattern, `/\`) {
			pattern = "**/" + pattern
		}
		expanded := []string{pattern}
		if pattern == "~" || strings.HasPrefix(pattern, "~/") || strings.HasPrefix(pattern, `~\`) {
			expanded = nil
			for _, profile := range profiles {
				expanded = append(expanded, profile+pattern[1:])
			}
		}
		for _, path := range expanded {
			matcher.patterns.Add(path, false, 0)
		}
	}
	return matcher
}

// Matches reports whether path matches any pattern.
func (matcher *PathMatcher) Matches(path string) bool {
	return len(matcher.patterns.MatchesPath(path)) > 0
}
