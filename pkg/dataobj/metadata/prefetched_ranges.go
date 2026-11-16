package metadata

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
)

// prefetcherKeyPrefix namespaces the cache keys. Change the version when the keys change, so builds
// of both versions do not read each other's cache items.
const prefetcherKeyPrefix = "dataobj-prefetcher/v1/"

// prefetchedRangesKind says what a group of prefetched ranges holds.
type prefetchedRangesKind uint8

const (
	// kindFileMetadata holds the header and the file metadata.
	kindFileMetadata prefetchedRangesKind = iota

	// kindCore holds the metadata of every non-logs section, and from every logs section the section
	// metadata and the column metadata of the stream ID and timestamp columns.
	kindCore

	// kindMessage holds the column metadata of the message column of the logs sections.
	kindMessage

	// kindColumn holds the column metadata of one structured metadata column.
	kindColumn

	// kindRegion holds the whole region.
	kindRegion
)

// prefetchedRangesID names a group of prefetched ranges that the cache holds together: its kind and,
// for kindColumn, the column name.
type prefetchedRangesID struct {
	Kind       prefetchedRangesKind
	ColumnName string
}

// cacheKey returns the unhashed and the hashed cache key of the group of the object at path.
//
// The unhashed key names the group exactly, so two different groups never have the same unhashed
// key. The length of the path comes first, so a # in a path or in a column name cannot make two keys
// equal. The unhashed key has no length limit.
//
// The hashed key is the key under which the cache stores the group. It is the prefix and the SHA-256
// of the unhashed key, so it has a fixed length that fits the key limit of memcached. Two groups can
// still have the same hashed key. The cache item therefore holds the unhashed key, and a reader
// compares it to the key it asked for.
func (id prefetchedRangesID) cacheKey(path string) (unhashed, hashed string) {
	var suffix string
	switch id.Kind {
	case kindFileMetadata:
		suffix = "fm"
	case kindCore:
		suffix = "core"
	case kindMessage:
		suffix = "msg"
	case kindRegion:
		suffix = "region"
	case kindColumn:
		suffix = "col:" + id.ColumnName
	}

	unhashed = prefetcherKeyPrefix + strconv.Itoa(len(path)) + ":" + path + "#" + suffix
	sum := sha256.Sum256([]byte(unhashed))
	return unhashed, prefetcherKeyPrefix + hex.EncodeToString(sum[:])
}
