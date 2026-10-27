package v5

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math/bits"
	"os"

	"github.com/grafana/loki/v3/pkg/logline/format"
)

// incrementalEncoder is the package-private streaming contract that
// FastPostingsEncoder satisfies.
type incrementalEncoder interface {
	StreamingPostingsEncoder
	beginStream(w io.Writer) error
	writeTerm(term [8]byte, bm format.Bitmap) error
	writeTermDocIDs(term [8]byte, docIDs []uint32, matchesAll bool) error
	endStream() (int64, []postingsBlockDirEntry, error)
}

// Compile-time assertion: FastPostingsEncoder must satisfy incrementalEncoder.
var _ incrementalEncoder = (*FastPostingsEncoder)(nil)

// StreamingIndexWriter writes an index file incrementally, without accumulating
// all postings in memory. Document IDs are cells of the configured day and
// must be below its cell count; the writer keeps no document table and counts
// the cells its postings reference.
//
// When constructed from a path (NewStreamingIndexWriter), the writer owns the
// underlying file: Close closes it and removes it on error. When constructed
// from an io.Writer (newStreamingIndexWriterTo, used by the streaming merge
// path), Close only finalises the index bytes and leaves the writer to the
// caller.
type StreamingIndexWriter struct {
	config  IndexWriteConfig
	encoder incrementalEncoder
	w       io.Writer
	f       *os.File // non-nil when the writer owns a backing file
	path    string   // set alongside f; used for cleanup on error
	layout  documentLayout
	// cells is a bitset of the cells referenced by any posting, sized to the
	// day's cell count. Its popcount is the footer's DocumentCount.
	cells       []uint64
	cellsPerDay uint64
	terms       [][NgramLength]byte
	lastTerm    [NgramLength]byte
	started     bool
	closed      bool
	err         error
	footer      IndexFooter
}

// newStreamingIndexWriter opens path for writing and returns a writer that
// finalises an index file at that path. Close closes the file and removes
// it on error.
func newStreamingIndexWriter(path string, cfg IndexWriteConfig) (*StreamingIndexWriter, error) {
	if _, err := cfg.documentLayout(); err != nil {
		return nil, err
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("create index file: %w", err)
	}
	sw, err := newStreamingIndexWriterTo(f, cfg)
	if err != nil {
		f.Close()
		_ = os.Remove(path)
		return nil, err
	}
	sw.f = f
	sw.path = path
	return sw, nil
}

// newStreamingIndexWriterTo constructs a writer that appends bytes to w.
// The caller owns w; the writer never closes it and never attempts any
// cleanup on error. Used by the streaming merge path, which feeds bytes
// into an io.Pipe rather than a file.
func newStreamingIndexWriterTo(w io.Writer, cfg IndexWriteConfig) (*StreamingIndexWriter, error) {
	layout, err := cfg.documentLayout()
	if err != nil {
		return nil, err
	}
	enc, err := buildPostingsEncoder(cfg)
	if err != nil {
		return nil, err
	}
	ie, ok := enc.(incrementalEncoder)
	if !ok {
		return nil, fmt.Errorf("postings encoder type %T does not implement incrementalEncoder", enc)
	}

	cellsPerDay := layout.public().CellsPerDay()
	return &StreamingIndexWriter{
		config:      cfg,
		encoder:     ie,
		w:           w,
		layout:      layout,
		cells:       make([]uint64, (cellsPerDay+63)/64),
		cellsPerDay: cellsPerDay,
		terms:       make([][NgramLength]byte, 0, 256),
	}, nil
}

// recordCell notes that a posting references cell, or returns an error if
// cell is not a cell of the day.
func (w *StreamingIndexWriter) recordCell(cell uint32) error {
	if uint64(cell) >= w.cellsPerDay {
		return fmt.Errorf("document ID %d is not a cell of the day: the layout has %d cells per day", cell, w.cellsPerDay)
	}
	w.cells[cell>>6] |= 1 << (cell & 63)
	return nil
}

func (w *StreamingIndexWriter) documentCount() uint32 {
	var n int
	for _, word := range w.cells {
		n += bits.OnesCount64(word)
	}
	return uint32(n)
}

// WriteTerm writes a single term's bitmap into the postings stream.
// Terms must arrive in strictly ascending order (same invariant as the
// k-way merge loop in StreamingMergeIndexReaders). Violating order corrupts
// the index: the term dictionary and postings section must agree on term IDs.
//
// The caller must not retain bm after WriteTerm returns — the bitmap is
// serialized or consumed within this call.
func (w *StreamingIndexWriter) WriteTermBitmap(term [8]byte, bm format.Bitmap) error {
	if w.err != nil {
		return w.err
	}
	if !w.started {
		// Open the postings stream on the first term.
		if err := w.encoder.beginStream(w.w); err != nil {
			w.err = fmt.Errorf("begin postings stream: %w", err)
			return w.err
		}
		w.started = true
	}

	var key [NgramLength]byte
	copy(key[:], term[:NgramLength])

	// Terms must arrive in strictly ascending order so that the term dictionary
	// and postings section agree on term IDs. Enforce this always-on so that
	// merge-loop bugs surface immediately rather than corrupting the index silently.
	if len(w.terms) > 0 && bytes.Compare(key[:], w.lastTerm[:]) <= 0 {
		w.err = fmt.Errorf("WriteTerm: out-of-order term %x after %x", key, w.lastTerm)
		return w.err
	}

	if !bm.MatchesAll && bm.Roaring != nil {
		var cellErr error
		bm.Roaring.Iterate(func(cell uint32) bool {
			cellErr = w.recordCell(cell)
			return cellErr == nil
		})
		if cellErr != nil {
			w.err = cellErr
			return w.err
		}
	}

	w.terms = append(w.terms, key)

	// Density filter: terms covering more than the threshold fraction of a full
	// day's documents are stored as sentinels. The cutoff is computed from the
	// document layout (see sentinelCutoff), not from this index's document
	// count. Callers may also set MatchesAll directly.
	if cutoff, ok := w.config.sentinelCutoff(); ok && !bm.MatchesAll && bm.Roaring != nil {
		if bm.Roaring.GetCardinality() > cutoff {
			bm = format.Bitmap{MatchesAll: true}
		}
	}

	if err := w.encoder.writeTerm(term, bm); err != nil {
		w.err = fmt.Errorf("write term postings: %w", err)
		return w.err
	}
	w.lastTerm = key
	return nil
}

// WriteTermDocIDs accepts pre-extracted sorted docIDs directly, bypassing
// roaring bitmap construction.
func (w *StreamingIndexWriter) WriteTermDocIDs(term [8]byte, docIDs []uint32, cardinality int) error {
	if w.err != nil {
		return w.err
	}
	if !w.started {
		if err := w.encoder.beginStream(w.w); err != nil {
			w.err = fmt.Errorf("begin postings stream: %w", err)
			return w.err
		}
		w.started = true
	}

	var key [NgramLength]byte
	copy(key[:], term[:NgramLength])

	if len(w.terms) > 0 && bytes.Compare(key[:], w.lastTerm[:]) <= 0 {
		w.err = fmt.Errorf("WriteTermDocIDs: out-of-order term %x after %x", key, w.lastTerm)
		return w.err
	}

	for _, cell := range docIDs {
		if err := w.recordCell(cell); err != nil {
			w.err = err
			return w.err
		}
	}

	w.terms = append(w.terms, key)

	// Density filter: same logic as WriteTermBitmap.
	cutoff, ok := w.config.sentinelCutoff()
	matchesAll := ok && uint64(cardinality) > cutoff

	if err := w.encoder.writeTermDocIDs(term, docIDs, matchesAll); err != nil {
		w.err = fmt.Errorf("write term postings: %w", err)
		return w.err
	}
	w.lastTerm = key
	return nil
}

// Close finalises the index by appending the term dictionary, directory
// sections, and the 256-byte footer. If this writer
// owns a backing file (opened via NewStreamingIndexWriter), Close also
// closes the file and removes it on error. Otherwise the caller remains
// responsible for closing and cleaning up the destination.
//
// After a successful Close, Info() returns the HeaderInfo describing the
// finalised index.
func (w *StreamingIndexWriter) Close() (retErr error) {
	// If we own a file, handle close and cleanup-on-error here.
	if w.f != nil {
		defer func() {
			closeErr := w.f.Close()
			w.f = nil
			if retErr == nil && closeErr != nil {
				retErr = fmt.Errorf("close index file: %w", closeErr)
			}
			if retErr != nil {
				if removeErr := os.Remove(w.path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
					retErr = fmt.Errorf("%w; cleanup partial index file failed: %v", retErr, removeErr)
				}
			}
		}()
	}

	if w.err != nil {
		if w.started {
			// Clean up encoder resources (e.g. zstd encoder) even on error.
			_, _, _ = w.encoder.endStream()
		}
		return w.err
	}

	// An index with no terms has no postings section.
	var (
		postingsDataSize int64
		postingsDir      []postingsBlockDirEntry
		err              error
	)
	if w.started {
		postingsDataSize, postingsDir, err = w.encoder.endStream()
		if err != nil {
			return fmt.Errorf("end postings stream: %w", err)
		}
		if postingsDataSize < 0 {
			return fmt.Errorf("invalid postings data size: %d", postingsDataSize)
		}
	}

	// Terms arrive pre-sorted from the k-way merge. Do NOT call normalizeTermBitmaps:
	// sorting would reassign term IDs and corrupt the postings section, which was
	// already flushed in sorted order.
	termData, termDir, err := buildTermData(w.terms)
	if err != nil {
		return fmt.Errorf("build term data: %w", err)
	}

	termDirSection, err := encodeTermBlockDirEntries(termDir)
	if err != nil {
		return fmt.Errorf("encode term directory: %w", err)
	}

	postingsDirSection, err := encodePostingsBlockDirEntries(postingsDir)
	if err != nil {
		return fmt.Errorf("encode postings directory: %w", err)
	}

	if _, err := w.w.Write(termData); err != nil {
		return fmt.Errorf("write term data: %w", err)
	}
	if _, err := w.w.Write(termDirSection); err != nil {
		return fmt.Errorf("write term directory: %w", err)
	}
	if _, err := w.w.Write(postingsDirSection); err != nil {
		return fmt.Errorf("write postings directory: %w", err)
	}

	w.footer = IndexFooter{
		Magic:                IndexMagic,
		Version:              IndexVersion,
		Flags:                composeIndexFlags(w.encoder.Encoding(), w.encoder.PackingFactor()),
		DocumentCount:        w.documentCount(),
		TermBlockCount:       uint32(len(termDir)),
		PostingsBlockCount:   uint32(len(postingsDir)),
		PostingsCompression:  w.encoder.CompressionType(),
		TermCount:            uint64(len(w.terms)),
		PostingsDataSize:     uint64(postingsDataSize),
		TermDataSize:         uint64(len(termData)),
		TermBlockDirSize:     uint64(len(termDirSection)),
		PostingsBlockDirSize: uint64(len(postingsDirSection)),
	}
	w.footer.setDocumentLayout(w.layout)

	// v2: append footer at EOF (no seek needed).
	if err := writeIndexHeader(w.w, w.footer); err != nil {
		return fmt.Errorf("write index footer: %w", err)
	}
	w.closed = true
	return nil
}

// Info returns the HeaderInfo describing the finalised index.
// Only valid after a successful Close(); returns the zero value otherwise.
func (w *StreamingIndexWriter) Info() format.HeaderInfo {
	if !w.closed {
		return format.HeaderInfo{}
	}
	return w.footer.Info()
}
