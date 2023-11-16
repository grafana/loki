package queryutil

import "hash/fnv"

// HashedQuery returns a unique hash value for the given `query`.
func HashedQuery(query string) uint32 {
	h := fnv.New32()
	_, _ = h.Write([]byte(query))
	return h.Sum32()
}
