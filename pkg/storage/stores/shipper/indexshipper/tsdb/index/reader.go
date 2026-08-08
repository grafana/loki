package index

import (
	"io"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
)

// Reader is the read-side interface implemented by every on-disk TSDB index reader.
type Reader interface {
	// Version returns the on-disk index format version.
	Version() int

	// RawFileReader exposes the underlying index file bytes as an
	// io.ReadSeekCloser so the indexshipper can upload the raw file.
	// The caller owns the returned reader and must Close it.
	RawFileReader() (io.ReadSeekCloser, error)

	// PostingsRanges returns the byte range in the underlying index file for
	// every posting list.
	PostingsRanges() (map[labels.Label]Range, error)

	// Bounds returns the min/max time range covered by the index.
	Bounds() (int64, int64)

	// Checksum returns the CRC32 checksum recorded in the index TOC.
	Checksum() uint32

	// LabelValues returns the possible label values for the given label name.
	LabelValues(name string, matchers ...*labels.Matcher) ([]string, error)

	// LabelNames returns the unique label names present in the index in
	// sorted order.
	LabelNames(matchers ...*labels.Matcher) ([]string, error)

	// LabelValueFor returns the value of the given label name for the series
	// referred to by id.
	LabelValueFor(id storage.SeriesRef, label string) (string, error)

	// LabelNamesFor returns the sorted label names for the series referred to
	// by ids.
	LabelNamesFor(ids ...storage.SeriesRef) ([]string, error)

	// Series populates lbls and chks for the series identified by id.
	Series(id storage.SeriesRef, from int64, through int64, lbls *labels.Labels, chks *[]ChunkMeta) (uint64, error)

	// ChunkStats returns aggregated chunk statistics for the series
	// identified by id.
	ChunkStats(id storage.SeriesRef, from, through int64, lbls *labels.Labels, by map[string]struct{}) (uint64, ChunkStats, error)

	// Postings returns a postings iterator over the series matching the
	// (name, value) pairs.
	Postings(name string, fpFilter FingerprintFilter, values ...string) (Postings, error)

	// Size returns the size of the underlying index in bytes.
	Size() int64

	// Close releases the underlying resources of the reader.
	Close() error
}
