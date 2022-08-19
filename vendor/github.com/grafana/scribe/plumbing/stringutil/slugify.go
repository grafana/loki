package stringutil

import "strings"

// Slugify removes typically illegal characters for use in identifiers
func Slugify(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, ".", "")
	s = strings.ReplaceAll(s, ",", "")
	s = strings.ReplaceAll(s, "-", "_")

	return s
}
