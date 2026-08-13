package logline

import (
	"context"
	"fmt"
	"io"

	"github.com/grafana/loki/v3/pkg/logline/format"
	v3 "github.com/grafana/loki/v3/pkg/logline/internal/v3"
)

// CurrentVersion is the canonical name for the current index format.
// "v3" indexes log lines, structured metadata values, and stream label values.
const CurrentVersion = "v3"

// AllVersions returns every supported format version.
//
// v4 shares v3's on-disk format byte-for-byte and differs only in n-gram
// extraction (numeric content uses 9-digit packed grams so integer queries can
// narrow, #2530). The format factories below therefore dispatch "v4" to the v3
// implementation; only ExtractorForVersion returns a different function.
func AllVersions() []string { return []string{"v3", "v4"} }

// ValidateVersion returns an error if the version string is not a supported
// index format version.
func ValidateVersion(version string) error {
	switch version {
	case "v3", "v4":
		return nil
	default:
		return fmt.Errorf("unsupported index version: %q", version)
	}
}

// Reader is the interface for querying a logline index.
type Reader interface {
	FindTerm(term string) (int, error)
	GetBitmap(termIndex int) (format.Bitmap, error)
	NewTermIterator() (format.TermIterator, error)
	Documents() []format.DocumentMetadata
	ReadHeader() format.HeaderInfo
	ClassifyRead(offset, length int64) format.ReadSection
	Close() error
}

// Writer is the interface for creating a logline index incrementally.
// Documents are provided at construction time via NewWriter.
type Writer interface {
	WriteTermBitmap(term [8]byte, bm format.Bitmap) error
	WriteTermDocIDs(term [8]byte, docIDs []uint32, cardinality int) error
	Close() error
}

// Merger is the interface for merging multiple indexes into one.
// The caller owns out and is responsible for closing and flushing it.
// Returns the HeaderInfo describing the written index so the caller can
// populate metadata without re-reading from the destination.
type Merger interface {
	Merge(ctx context.Context, readers []io.ReaderAt, sizes []int64, out io.Writer) (format.HeaderInfo, error)
}

// OpenReader opens an index reader for the given format version.
// The returned any value is opaque cached state that can be passed to
// OpenReaderCached for fast reopens without re-reading metadata.
func OpenReader(version string, r io.ReaderAt, offset, size int64, info format.HeaderInfo) (Reader, any, error) {
	switch version {
	case "v3", "v4": // v4 shares the v3 on-disk format
		reader, err := v3.OpenIndexAtWithHeader(r, offset, size, info)
		if err != nil {
			return nil, nil, err
		}
		return reader, reader.CachedState(), nil
	default:
		return nil, nil, fmt.Errorf("unsupported index version: %q", version)
	}
}

// OpenReaderCached opens an index reader using opaque cached state from a prior
// OpenReader call, avoiding metadata re-reads.
func OpenReaderCached(version string, r io.ReaderAt, offset, size int64, cached any) (Reader, error) {
	switch version {
	case "v3", "v4": // v4 shares the v3 on-disk format
		return v3.OpenIndexAtCached(r, offset, size, cached)
	default:
		return nil, fmt.Errorf("unsupported index version: %q", version)
	}
}

// NewWriter creates a streaming index writer for the given format version.
// All documents must be provided upfront; terms are then streamed via WriteTermBitmap.
// cfg may be nil for version defaults.
func NewWriter(version, path string, docs []format.DocumentMetadata, cfg *format.WriterConfig) (Writer, error) {
	switch version {
	case "v3", "v4": // v4 shares the v3 on-disk format
		return v3.NewWriter(path, docs, cfg)
	default:
		return nil, fmt.Errorf("unsupported index version: %q", version)
	}
}

// NewMerger creates a Merger for the given format version.
// cfg may be nil for version defaults.
func NewMerger(version string, cfg *format.WriterConfig) (Merger, error) {
	switch version {
	case "v3", "v4": // v4 shares the v3 on-disk format
		return mergerFunc(func(ctx context.Context, readers []io.ReaderAt, sizes []int64, out io.Writer) (format.HeaderInfo, error) {
			return v3.Merge(ctx, readers, sizes, out, cfg)
		}), nil
	default:
		return nil, fmt.Errorf("unsupported index version: %q", version)
	}
}

// mergerFunc adapts a function to the Merger interface.
type mergerFunc func(ctx context.Context, readers []io.ReaderAt, sizes []int64, out io.Writer) (format.HeaderInfo, error)

func (f mergerFunc) Merge(ctx context.Context, readers []io.ReaderAt, sizes []int64, out io.Writer) (format.HeaderInfo, error) {
	return f(ctx, readers, sizes, out)
}

// OpenReaderAt opens an index reader from an io.ReaderAt, auto-detecting the
// format version. Probes the v3 footer-based format (on-disk version 4).
// The returned string is the detected version and the any value is opaque
// cached state for OpenReaderCached.
//
// A v4 file is byte-identical to a v3 file, so footer probing cannot tell them
// apart and this reports "v3" for both. That is safe for the callers of this
// function, which are format-only tools (dump, convert, identity) that never
// re-extract n-grams. The query path never auto-detects: it takes the version
// from meta.json, so a v4 index is always read with the v4 extractor.
func OpenReaderAt(r io.ReaderAt, offset, size int64) (Reader, string, any, error) {
	if size >= int64(v3.IndexFooterSize) {
		if reader, err := v3.OpenIndexAt(r, offset, size); err == nil {
			return reader, "v3", reader.CachedState(), nil
		}
	}
	return nil, "", nil, fmt.Errorf("unsupported index format: no v3 footer detected")
}

// ReadHeaderAt reads the header summary of the index in r, auto-detecting the
// format version. It satisfies store.HeaderReaderFunc so callers can hand it
// straight to store.PopulateFileInfo.
//
// Reads go through io.ReaderAt only, so the caller's file offset is untouched.
func ReadHeaderAt(r io.ReaderAt, size int64) (format.HeaderInfo, error) {
	reader, _, _, err := OpenReaderAt(r, 0, size)
	if err != nil {
		return format.HeaderInfo{}, err
	}
	defer reader.Close()
	return reader.ReadHeader(), nil
}

// OpenFile opens an index file and auto-detects its format version.
// The returned Reader owns the underlying file; Close releases it.
func OpenFile(path string) (Reader, string, error) {
	if reader, err := v3.OpenIndexFile(path); err == nil {
		return reader, "v3", nil
	}
	return nil, "", fmt.Errorf("open index %s: unsupported format (no v3 footer detected)", path)
}
