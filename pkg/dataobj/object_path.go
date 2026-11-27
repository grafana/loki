package dataobj

import "strings"

// compactedDirectory is the path segment below objects/ or indexes/ that holds compacted objects. A
// builder never writes there, because its keys continue with a hexadecimal hash prefix and "tenants"
// is not hexadecimal.
const compactedDirectory = "tenants/"

// IndexDirectory is the directory of the index storage that holds the index objects. The index
// storage is the part of the bucket below metastore.IndexStoragePrefix.
const IndexDirectory = "indexes/"

// IsCompactedObjectPath reports whether key names a compacted logs or index object.
func IsCompactedObjectPath(key string) bool {
	_, rest, found := strings.Cut(key, "/")
	return found && strings.HasPrefix(rest, compactedDirectory)
}
