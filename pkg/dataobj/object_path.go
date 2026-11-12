package dataobj

import "strings"

// compactedDirectory is the path segment below objects/ or indexes/ that holds compacted objects. A
// builder never writes there, because its keys continue with a hexadecimal hash prefix and "tenants"
// is not hexadecimal.
const compactedDirectory = "tenants/"

// IsCompactedObjectPath reports whether key names a compacted logs or index object.
func IsCompactedObjectPath(key string) bool {
	_, rest, found := strings.Cut(key, "/")
	return found && strings.HasPrefix(rest, compactedDirectory)
}
