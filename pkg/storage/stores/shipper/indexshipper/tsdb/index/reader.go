package index

import (
	"io"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
)

// ReaderOptions selects and configures one of the Reader implementations in this package.
type ReaderOptions interface {
	// OpenReader constructs the reader described by these options against the
	// selected index file.
	// The caller owns the returned Reader and must Close it.
	OpenReader(path string) (Reader, error)
}

// Reader is the read-side interface implemented by every on-disk TSDB index reader.
type Reader interface {
	// Version returns the on-disk index format version.
	Version() int

	// RawFileReader exposes the underlying index file bytes as an
	// io.ReadSeekCloser so the indexshipper can upload the raw file.
	// The caller owns the returned reader and must Close it.
	RawFileReader() (io.ReadSeekCloser, error)

	// Bounds returns the min/max time range covered by the index.
	Bounds() (int64, int64)

	// Checksum returns the CRC32 checksum recorded in the index TOC.
	Checksum() uint32

	// LabelValues returns the possible label values for the given label name.
	LabelValues(name string, matchers ...*labels.Matcher) ([]string, error)

	// LabelNames returns the unique label names present in the index in
	// sorted order.
	LabelNames(matchers ...*labels.Matcher) ([]string, error)

	// Postings returns a postings iterator over the series matching the
	// (name, value) pairs.
	Postings(name string, fpFilter FingerprintFilter, values ...string) (Postings, error)

	// NewSeriesScan returns a scan over the series records of one pass over a
	// postings list.
	// The caller owns the returned scan and must Close it.
	NewSeriesScan() SeriesScan

	// Size returns the size of the underlying index in bytes.
	Size() int64

	// Close releases the underlying resources of the reader.
	Close() error
}

// SeriesScan reads series records for a single pass over a postings list.
// We generally scan through the series section of an index in ascending order
// of SeriesRef, which is the order in which they're stored in the index file.
// This makes it much more efficient to keep a reader open scanning forwards
// through the file rather than opening a new one for each series that needs
// to be read.
//
// A scan is not safe for concurrent use.
// Refs are expected, but not required to arrive in ascending order. If they
// arrive out of order then a performance cost may be paid.
type SeriesScan interface {
	// Series populates lbls and chks for the series identified by id.
	Series(id storage.SeriesRef, from int64, through int64, lbls *labels.Labels, chks *[]ChunkMeta) (uint64, error)

	// ChunkStats returns aggregated chunk statistics for the series
	// identified by id.
	ChunkStats(id storage.SeriesRef, from, through int64, lbls *labels.Labels, by map[string]struct{}) (uint64, ChunkStats, error)

	// LabelValueFor returns the value of the given label name for the series
	// referred to by id.
	LabelValueFor(id storage.SeriesRef, label string) (string, error)

	// LabelNamesFor returns the sorted label names for the series referred to
	// by ids.
	LabelNamesFor(ids ...storage.SeriesRef) ([]string, error)

	// Close releases the resources held for the duration of the scan.
	Close() error
}
