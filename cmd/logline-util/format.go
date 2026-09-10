package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/RoaringBitmap/roaring"
)

// formatBytes formats byte count into human-readable format.
// Returns sizes like "1.5 KB", "8.0 MB", "10.0 GB".
func formatBytes(bytes uint64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)

	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// formatTerm formats an 8-byte term as a printable string.
// Non-printable characters are escaped as \xHH.
// Trailing null bytes (padding) are trimmed.
func formatTerm(term [8]byte) string {
	var sb strings.Builder
	sb.Grow(16) // Pre-allocate for efficiency

	for _, b := range term {
		// Printable ASCII (space through ~)
		if b >= 32 && b <= 126 {
			sb.WriteByte(b)
		} else if b == 0 {
			// Stop at first null byte only if we've written something
			// This handles padding nulls at the end
			if sb.Len() > 0 {
				break
			}
			// If null is first byte or among non-printables, escape it
			fmt.Fprintf(&sb, "\\x%02x", b)
		} else {
			// Escape other non-printable
			fmt.Fprintf(&sb, "\\x%02x", b)
		}
	}

	return sb.String()
}

// formatTime formats Unix millisecond timestamp as RFC3339 string with milliseconds in UTC.
// Input is milliseconds since Unix epoch.
func formatTime(unixMilli int64) string {
	t := time.UnixMilli(unixMilli).UTC()
	// Format: 2006-01-02T15:04:05.000Z
	return t.Format("2006-01-02T15:04:05.000Z07:00")
}

// formatBitmap formats a roaring bitmap as either summary or full list.
// If showAll is false, returns "N docs" (e.g., "5 docs" or "1 doc").
// If showAll is true, returns document ID array (e.g., "[0, 1, 2]").
func formatBitmap(bm *roaring.Bitmap, showAll bool) string {
	count := bm.GetCardinality()

	if showAll {
		// Show full list of document IDs
		docIDs := bm.ToArray()
		if len(docIDs) == 0 {
			return "[]"
		}

		var sb strings.Builder
		sb.WriteByte('[')
		for i, docID := range docIDs {
			if i > 0 {
				sb.WriteString(", ")
			}
			fmt.Fprintf(&sb, "%d", docID)
		}
		sb.WriteByte(']')
		return sb.String()
	}

	// Show summary
	if count == 1 {
		return "1 doc"
	}
	return fmt.Sprintf("%d docs", count)
}

// formatNumber formats a number with thousands separators.
// Examples: 1234 → "1,234", 1000000 → "1,000,000"
func formatNumber(n uint64) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}

	// Convert to string and add commas
	str := fmt.Sprintf("%d", n)
	var result strings.Builder
	result.Grow(len(str) + (len(str)-1)/3)

	// Process from right to left
	for i := len(str) - 1; i >= 0; i-- {
		if i < len(str)-1 && (len(str)-1-i)%3 == 0 {
			result.WriteByte(',')
		}
		result.WriteByte(str[i])
	}

	// Reverse the string
	resultStr := result.String()
	runes := []rune(resultStr)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}

	return string(runes)
}
