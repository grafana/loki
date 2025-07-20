package util

import "time"

// FormatTimeRFC3339Nano formats the given time in RFC3339Nano format in UTC.
// Use this everywhere in the engine for consistent timestamp formatting.
func FormatTimeRFC3339Nano(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}
