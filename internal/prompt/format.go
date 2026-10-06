package prompt

import (
	"strconv"
	"strings"
	"time"

	"github.com/toldry/ullage/internal/scanner"
)

// formatSize rounds to two significant digits with a one-letter unit: 41G, 6.1G, 820M.
// Exact values stay in the program; the AI only needs magnitudes.
func formatSize(bytes int64) string {
	units := []string{"B", "K", "M", "G", "T", "P"}
	value := float64(bytes)
	unit := 0
	for value >= 1000 && unit < len(units)-1 {
		value /= 1024
		unit++
	}
	if value >= 10 || unit == 0 {
		return strconv.Itoa(int(value+0.5)) + units[unit]
	}
	text := strconv.FormatFloat(value, 'f', 1, 64)
	return strings.TrimSuffix(text, ".0") + units[unit]
}

// formatCount shortens large counts: 9412 → 9.4k, 21095 → 21k.
func formatCount(count int64) string {
	switch {
	case count < 1000:
		return strconv.FormatInt(count, 10)
	case count < 10_000:
		return strings.TrimSuffix(strconv.FormatFloat(float64(count)/1000, 'f', 1, 64),
			".0") + "k"
	case count < 1_000_000:
		return strconv.FormatInt((count+500)/1000, 10) + "k"
	default:
		return strings.TrimSuffix(strconv.FormatFloat(float64(count)/1e6, 'f', 1, 64),
			".0") + "M"
	}
}

// formatMonth renders a timestamp as YYYY-MM (amendment A-3); day resolution does not
// change staleness judgments and costs tokens.
func formatMonth(unixNanos int64) string {
	if unixNanos <= 0 {
		return ""
	}
	return time.Unix(0, unixNanos).UTC().Format("2006-01")
}

// minimumTypeShare drops file types that are a small part of an item.
const minimumTypeShare = 0.1

func formatTypes(shares []scanner.ExtensionShare) string {
	var total int64
	for _, share := range shares {
		total += share.Bytes
	}
	var kept []string
	for _, share := range shares {
		if total > 0 && float64(share.Bytes) >= minimumTypeShare*float64(total) {
			kept = append(kept, share.Extension)
		}
	}
	if len(kept) == 0 {
		return ""
	}
	return "{" + strings.Join(kept, ",") + "}"
}

// details renders the optional parts after the path: [N files] m.. c.. {types}.
func details(files int64, modified, created int64, types []scanner.ExtensionShare,
	nameShown bool) string {
	if !nameShown {
		// Redacted items carry their size only (decision D-030).
		return ""
	}
	var parts []string
	if files > 1 {
		parts = append(parts, "["+formatCount(files)+" files]")
	}
	modifiedMonth, createdMonth := formatMonth(modified), formatMonth(created)
	if modifiedMonth != "" {
		parts = append(parts, "m"+modifiedMonth)
	}
	if createdMonth != "" && createdMonth != modifiedMonth {
		parts = append(parts, "c"+createdMonth)
	}
	if typeText := formatTypes(types); typeText != "" {
		parts = append(parts, typeText)
	}
	if len(parts) == 0 {
		return ""
	}
	return " " + strings.Join(parts, " ")
}
