package scanner

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/toldry/ullage/internal/platform"
)

// ExtensionShare is the total apparent size of one file type inside a directory.
type ExtensionShare struct {
	Extension string
	Bytes     int64
}

const (
	maximumExtensionsPerDirectory = 3
	maximumExtensionLength        = 10
)

// NormalizeExtension returns a lowercase extension without the dot, or "" when the name
// has none or the extension looks like part of a name (too long, or not alphanumeric).
func NormalizeExtension(name string) string {
	extension := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
	// A leading dot marks a hidden file such as .bashrc, not an extension.
	isDotfile := strings.LastIndex(name, ".") == 0
	if extension == "" || len(extension) > maximumExtensionLength || isDotfile {
		return ""
	}
	for _, character := range extension {
		isLetter := character >= 'a' && character <= 'z'
		isDigit := character >= '0' && character <= '9'
		if !isLetter && !isDigit {
			return ""
		}
	}
	return extension
}

// dominantExtensions summarizes the files directly in one directory.
func dominantExtensions(entries []platform.DirectoryEntry) []ExtensionShare {
	byExtension := map[string]int64{}
	for index := range entries {
		entry := &entries[index]
		if entry.Kind != platform.EntryFile {
			continue
		}
		if extension := NormalizeExtension(entry.Name); extension != "" {
			byExtension[extension] += max(entry.LogicalBytes, entry.AllocatedBytes)
		}
	}
	return topExtensions(byExtension, maximumExtensionsPerDirectory)
}

func topExtensions(byExtension map[string]int64, limit int) []ExtensionShare {
	if len(byExtension) == 0 {
		return nil
	}
	shares := make([]ExtensionShare, 0, len(byExtension))
	for extension, bytes := range byExtension {
		shares = append(shares, ExtensionShare{Extension: extension, Bytes: bytes})
	}
	sort.Slice(shares, func(first, second int) bool {
		if shares[first].Bytes != shares[second].Bytes {
			return shares[first].Bytes > shares[second].Bytes
		}
		return shares[first].Extension < shares[second].Extension
	})
	if len(shares) > limit {
		shares = shares[:limit]
	}
	return shares
}

// MergeExtensions combines summaries, for example of a folder and its subfolders. The
// inputs are already truncated, so the result is an approximation of the true mix.
func MergeExtensions(summaries [][]ExtensionShare, limit int) []ExtensionShare {
	byExtension := map[string]int64{}
	for _, summary := range summaries {
		for _, share := range summary {
			byExtension[share.Extension] += share.Bytes
		}
	}
	return topExtensions(byExtension, limit)
}
