package metadata

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/dustin/go-humanize"
)

// ErrNotPrefetchable means the caller must read the metadata of an object directly. The layout is
// invalid, the metadata region is too large, or the cache failed and cannot answer.
var ErrNotPrefetchable = errors.New("data object metadata cannot be prefetched")

// DataobjSourceReader reads byte ranges of one object from object storage.
type DataobjSourceReader interface {
	// ReadRange returns up to length bytes starting at offset. It returns fewer bytes when the
	// object ends first. The caller closes the reader.
	ReadRange(ctx context.Context, offset, length int64) (io.ReadCloser, error)
}

// PrefetchSet says which metadata a query reads. The zero value reads the stream ID and the timestamp.
type PrefetchSet struct {
	// Message is true when the query reads the message column. It is ignored when AllMetadata is true.
	Message bool

	// AllMetadata is true when the query reads every structured metadata column. The prefetcher then
	// serves the whole region, so Message and MetadataNames have no effect.
	AllMetadata bool

	// MetadataNames lists the structured metadata columns the query reads by name. It is ignored when
	// AllMetadata is true.
	MetadataNames []string
}

// prefetchedRangesIDs returns the IDs of the groups of prefetched ranges that the prefetch set reads,
// the file metadata group first. A prefetch set that reads every column reads the region group and no
// other.
func (s PrefetchSet) prefetchedRangesIDs() []prefetchedRangesID {
	ids := []prefetchedRangesID{{Kind: kindFileMetadata}}
	if s.AllMetadata {
		return append(ids, prefetchedRangesID{Kind: kindRegion})
	}

	ids = append(ids, prefetchedRangesID{Kind: kindCore})
	if s.Message {
		ids = append(ids, prefetchedRangesID{Kind: kindMessage})
	}
	for _, name := range s.MetadataNames {
		id := prefetchedRangesID{Kind: kindColumn, ColumnName: name}
		if !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	return ids
}

// prefetchedRangesFor returns the ranges that the prefetch set reads, sorted by offset. groups holds
// the groups of the object. region is the whole region and is used when the prefetch set reads every
// column.
func (s PrefetchSet) prefetchedRangesFor(groups map[prefetchedRangesID]PrefetchedRanges, region []byte) PrefetchedRanges {
	if s.AllMetadata {
		return PrefetchedRanges{{Offset: 0, Data: region}}
	}

	var out PrefetchedRanges
	for _, id := range s.prefetchedRangesIDs() {
		out = append(out, groups[id]...)
	}
	slices.SortFunc(out, func(a, b PrefetchedRange) int {
		switch {
		case a.Offset < b.Offset:
			return -1
		case a.Offset > b.Offset:
			return 1
		}
		return 0
	})
	return out
}

// PrefetchRequest asks a Prefetcher for the metadata of one object.
type PrefetchRequest struct {
	// Path is the object key, unique within the bucket.
	Path        string
	PrefetchSet PrefetchSet
	Source      DataobjSourceReader
}

// PrefetchResponse holds the prefetched metadata of one object.
type PrefetchResponse struct {
	// PrefetchedRanges are sorted by offset and do not overlap. The first range starts at offset 0 and
	// holds the header and the whole file metadata. The ranges are shared, so the caller must not
	// modify their data.
	PrefetchedRanges PrefetchedRanges
}

// A Prefetcher returns the metadata of an object that a query reads. The response can omit metadata
// that the prefetch set names, and a reader then reads the omitted metadata from object storage. A Prefetcher
// is safe for concurrent use.
type Prefetcher interface {
	// Prefetch returns an error that wraps ErrNotPrefetchable when the caller must read the metadata
	// directly. Any other error is final.
	Prefetch(ctx context.Context, req PrefetchRequest) (PrefetchResponse, error)
}

// PrefetcherConfig configures a CachedPrefetcher.
type PrefetcherConfig struct {
	Enabled bool `yaml:"enabled" category:"experimental"`

	Compression         string        `yaml:"compression"`
	MinCompressionRatio float64       `yaml:"min_compression_ratio"`
	HeadBytes           int           `yaml:"head_bytes"`
	MaxRegionBytes      int           `yaml:"max_region_bytes"`
	LoadTimeout         time.Duration `yaml:"load_timeout"`
}

const (
	compressionZstd = "zstd"
	compressionNone = "none"

	// minHeadBytes is the smallest first read of a cold open.
	minHeadBytes = 16 * 1024
)

// RegisterFlagsWithPrefix registers the flags of the config under prefix.
func (c *PrefetcherConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.BoolVar(&c.Enabled, prefix+"enabled", false, "Experimental: Serve the metadata of log objects from separate cache items, so a query reads only the metadata it needs.")
	f.StringVar(&c.Compression, prefix+"compression", compressionZstd, "The codec of the cache items. Supported values are: zstd, none.")
	f.Float64Var(&c.MinCompressionRatio, prefix+"min-compression-ratio", 1.5, "A cache item stays uncompressed when compression shrinks it by less than this ratio.")
	f.IntVar(&c.HeadBytes, prefix+"head-bytes", 256*1024, "Size in bytes of the first object storage read of an object whose metadata is not cached. The header and the file metadata of the object must fit in it, or the prefetcher skips the object. The minimum is 16 KiB.")
	f.IntVar(&c.MaxRegionBytes, prefix+"max-region-bytes", 4*1024*1024, fmt.Sprintf("The largest metadata region, in bytes, that the prefetcher handles. The metadata region of an object is the part of the object from its start to the end of the metadata of all sections. The prefetcher skips an object with a larger region, and a query reads the metadata of that object directly. When the metadata cache has a maximum item size, this value plus %s must not exceed it.", humanize.IBytes(cacheItemOverheadBytes)))
	f.DurationVar(&c.LoadTimeout, prefix+"load-timeout", 5*time.Second, "The time limit of the load that follows a cache miss.")
}

// Validate returns an error when an enabled config has an invalid value.
func (c *PrefetcherConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	if c.Compression != compressionZstd && c.Compression != compressionNone {
		return fmt.Errorf("unsupported metadata prefetcher compression %q", c.Compression)
	}
	if c.MinCompressionRatio < 1 {
		return errors.New("the metadata prefetcher minimum compression ratio must be at least 1")
	}
	if c.HeadBytes < minHeadBytes {
		return fmt.Errorf("the metadata prefetcher head bytes must be at least %d", minHeadBytes)
	}
	if c.MaxRegionBytes <= 0 {
		return errors.New("the metadata prefetcher max region bytes must be positive")
	}
	if c.LoadTimeout <= 0 {
		return errors.New("the metadata prefetcher load timeout must be positive")
	}
	return nil
}
