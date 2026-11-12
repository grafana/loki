package metadata

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/filemd"
)

// errInvalidFileMetadata means the ranges are not the file metadata group.
var errInvalidFileMetadata = errors.New("the ranges are not valid file metadata")

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

// PrefetchedRange is a run of object bytes held in memory at an absolute offset.
type PrefetchedRange struct {
	Offset int64
	Data   []byte
}

// PrefetchedRanges is a list of PrefetchedRange.
type PrefetchedRanges []PrefetchedRange

// Valid reports whether the ranges are sorted by offset and do not overlap. It also rejects a
// negative offset and an end that overflows int64.
func (rs PrefetchedRanges) Valid() bool {
	end := int64(0)
	for _, r := range rs {
		if r.Offset < end || r.Offset+int64(len(r.Data)) < r.Offset {
			return false
		}
		end = r.Offset + int64(len(r.Data))
	}
	return true
}

// size returns the number of bytes that the ranges hold.
func (rs PrefetchedRanges) size() int64 {
	var n int64
	for _, r := range rs {
		n += int64(len(r.Data))
	}
	return n
}

// parseFileMetadata decodes the ranges of the file metadata group: one range at offset 0 that holds
// the header and the file metadata, and nothing else. It returns the file metadata and the offset
// where its sections start. The ranges of another group are not valid file metadata.
func (rs PrefetchedRanges) parseFileMetadata() (*filemd.Metadata, int64, error) {
	if len(rs) != 1 || rs[0].Offset != 0 {
		return nil, 0, errInvalidFileMetadata
	}
	md, startOffset, err := decodeHeaderAndFileMetadata(rs[0].Data)
	if err != nil {
		return nil, 0, err
	}
	if startOffset != int64(len(rs[0].Data)) {
		return nil, 0, errInvalidFileMetadata
	}
	return md, startOffset, nil
}

// mergeAdjacent joins the ranges that touch each other. The ranges must be sorted by offset and their
// Data must alias the input region slice, which starts at offset 0, so a joined range aliases region too.
// It joins rs in-place.
func (rs PrefetchedRanges) mergeAdjacent(region []byte) PrefetchedRanges {
	merged := rs[:0]
	for _, r := range rs {
		if n := len(merged); n > 0 && merged[n-1].Offset+int64(len(merged[n-1].Data)) == r.Offset {
			end := r.Offset + int64(len(r.Data))
			merged[n-1].Data = region[merged[n-1].Offset:end:end]
			continue
		}
		merged = append(merged, r)
	}
	return merged
}

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

// prefetchedRangesGroups holds the groups of prefetched ranges of one object.
type prefetchedRangesGroups struct {
	Groups map[prefetchedRangesID]PrefetchedRanges

	// ColumnNames lists every structured metadata column of the object, and each has a group. A column
	// that is not in ColumnNames is a column the object does not have.
	ColumnNames map[string]struct{}
}
