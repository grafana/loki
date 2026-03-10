package helpers

import (
	"strings"
)

// VersionToFloat converts a version string to a float32 for easier comparison.
func VersionToFloat(version string) float32 {
	switch {
	case strings.HasPrefix(version, "3.0"):
		return 3.0
	default:
		return 3.1
	}
}
