package utils

import (
	"path/filepath"
	"strings"
)

func ReplaceWindowsDriveWithLinuxPath(path string) string {
	if len(path) > 1 && path[1] == ':' {
		path = strings.ReplaceAll(path, "\\", "/")
		return path[2:]
	}
	return strings.ReplaceAll(path, "\\", "/")
}

// CheckPathOverlap joins pathA and pathB while avoiding duplicated overlapping segments.
// It tolerates mixed separators in the inputs and uses OS-specific path comparison rules.
// It also handles leading ".." segments in pathB by first resolving them against pathA
// before checking for overlap, preventing path doubling issues.
func CheckPathOverlap(pathA, pathB, sep string) string {
	if sep == "" {
		sep = string(filepath.Separator)
	}
	if pathB != "" {
		if pathB[0] == '/' || pathB[0] == '\\' || (len(pathB) > 1 && pathB[1] == ':') {
			return filepath.Clean(pathB)
		}
	}

	// Split on both separators so mixed-path inputs are handled safely.
	split := func(p string) []string {
		return strings.FieldsFunc(p, func(r rune) bool {
			return r == '/' || r == '\\'
		})
	}

	// preserve path prefix (absolute path marker or Windows drive letter)
	prefix := ""
	if len(pathA) > 0 && (pathA[0] == '/' || pathA[0] == '\\') {
		prefix = string(pathA[0])
	}
	if len(pathA) > 2 && pathA[1] == ':' {
		prefix = pathA[:2] + sep
	}

	aParts := split(pathA)
	bParts := split(pathB)

	// If pathA has a Windows drive letter, drop it from aParts
	// to avoid duplicating it when rebuilding with prefix.
	if len(pathA) > 2 && pathA[1] == ':' {
		if len(aParts) > 0 && strings.EqualFold(aParts[0], pathA[:2]) {
			aParts = aParts[1:]
		}
	}

	// Handle leading ".." segments in pathB by going up pathA.
	// This prevents path doubling when resolving refs like "../components/schemas/X.yaml"
	// from a base path like "/path/to/components/schemas".
	dotDotResolved := false
	for len(bParts) > 0 && bParts[0] == ".." && len(aParts) > 0 {
		aParts = aParts[:len(aParts)-1] // Go up one directory
		bParts = bParts[1:]             // Remove the ".."
		dotDotResolved = true
	}

	// rebuild pathA if we resolved any ".." segments
	if dotDotResolved {
		if len(aParts) == 0 {
			pathA = prefix
		} else {
			// Use the OS separator to join parts, then add a prefix
			pathA = prefix + strings.Join(aParts, string(filepath.Separator))
		}
	}

	// find the longest suffix of aParts that matches the prefix of bParts.
	overlap := 0
	maxCheck := len(aParts)
	if len(bParts) < maxCheck {
		maxCheck = len(bParts)
	}
	for i := maxCheck; i > 0; i-- {
		start := len(aParts) - i
		match := true
		for j := 0; j < i; j++ {
			if !pathPartEqual(aParts[start+j], bParts[j]) {
				match = false
				break
			}
		}
		if match {
			overlap = i
			break
		}
	}

	if overlap > 0 {
		bParts = bParts[overlap:]
	}

	// sep is used to build the tail; filepath.Join normalizes separators for the OS.
	tail := strings.Join(bParts, sep)
	if tail == "" {
		if pathA == "" {
			return ""
		}
		return filepath.Clean(pathA)
	}

	return filepath.Join(pathA, tail)
}
