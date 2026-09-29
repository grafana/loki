package v3

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/grafana/loki/v3/pkg/logline/format"
)

// incrementalEncoder is the package-private streaming contract that
// FastPostingsEncoder satisfies.
type incrementalEncoder interface {
	StreamingPostingsEncoder
	beginStream(w io.Writer, documentCount uint32) error
	writeTerm(term [8]byte, bm format.Bitmap) error
	writeTermDocIDs(term [8]byte, docIDs []uint32, matchesAll bool) error
	endStream() (int64, []postingsBlockDirEntry, error)
}

// Compile-time assertion: FastPostingsEncoder must satisfy incrementalEncoder.
var _ incrementalEncoder = (*FastPostingsEncoder)(nil)

// StreamingIndexWriter writes an index file incrementally, without accumulating
// all postings in memory. Documents must be registered via AddDocument before
// the first WriteTerm call, because the total document count must be known
// before the postings stream is opened.
//
// When constructed from a path (NewStreamingIndexWriter), the writer owns the
// underlying file: Close closes it and removes it on error. When constructed
// from an io.Writer (newStreamingIndexWriterTo, used by the streaming merge
// path), Close only finalises the index bytes and leaves the writer to the
// caller.
type StreamingIndexWriter struct {
	config   IndexWriteConfig
	encoder  incrementalEncoder
	w        io.Writer
	f        *os.File // non-nil when the writer owns a backing file
	path     string   // set alongside f; used for cleanup on error
	docs     []format.DocumentMetadata
	terms    [][NgramLength]byte
	lastTerm [NgramLength]byte
	started  bool
	closed   bool
	err      error
	docCount uint32
	footer   IndexFooter
}

// newStreamingIndexWriter opens path for writing and returns a writer that
// finalises an index file at that path. Close closes the file and removes
// it on error.
func newStreamingIndexWriter(path string, cfg IndexWriteConfig, documentCount uint32) (*StreamingIndexWriter, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("create index file: %w", err)
	}
	sw, err := newStreamingIndexWriterTo(f, cfg, documentCount)
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
func newStreamingIndexWriterTo(w io.Writer, cfg IndexWriteConfig, documentCount uint32) (*StreamingIndexWriter, error) {
	enc, err := buildPostingsEncoder(cfg)
	if err != nil {
		return nil, err
	}
	ie, ok := enc.(incrementalEncoder)
	if !ok {
		return nil, fmt.Errorf("postings encoder type %T does not implement incrementalEncoder", enc)
	}

	return &StreamingIndexWriter{
		config:   cfg,
		encoder:  ie,
		w:        w,
		docs:     make([]format.DocumentMetadata, 0, int(documentCount)),
		terms:    make([][NgramLength]byte, 0, 256),
		docCount: documentCount,
	}, nil
}

// AddDocument appends a document to the index. Must be called before WriteTerm.
func (w *StreamingIndexWriter) AddDocument(doc format.DocumentMetadata) {
	w.docs = append(w.docs, doc)
}

// AddDocuments appends multiple documents. Must be called before WriteTerm.
func (w *StreamingIndexWriter) AddDocuments(docs []format.DocumentMetadata) {
	w.docs = append(w.docs, docs...)
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
		if err := w.encoder.beginStream(w.w, w.docCount); err != nil {
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

	w.terms = append(w.terms, key)

	// Density filter: terms covering more than the threshold fraction of a full
	// day's documents are stored as sentinels. The cutoff is computed from the
	// document interval (24h / interval * threshold), not from this index's
	// document count. Callers may also set MatchesAll directly.
	if !bm.MatchesAll && bm.Roaring != nil && w.config.DensityThreshold > 0 && w.config.DocumentInterval > 0 {
		docsPerDay := uint64(24 * time.Hour / w.config.DocumentInterval)
		threshold := uint64(float32(docsPerDay) * w.config.DensityThreshold)
		if bm.Roaring.GetCardinality() > threshold {
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
		if err := w.encoder.beginStream(w.w, w.docCount); err != nil {
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

	w.terms = append(w.terms, key)

	// Density filter: same logic as WriteTermBitmap.
	matchesAll := false
	if w.config.DensityThreshold > 0 && w.config.DocumentInterval > 0 {
		docsPerDay := uint64(24 * time.Hour / w.config.DocumentInterval)
		threshold := uint64(float32(docsPerDay) * w.config.DensityThreshold)
		if uint64(cardinality) > threshold {
			matchesAll = true
		}
	}

	if err := w.encoder.writeTermDocIDs(term, docIDs, matchesAll); err != nil {
		w.err = fmt.Errorf("write term postings: %w", err)
		return w.err
	}
	w.lastTerm = key
	return nil
}

// Close finalises the index by appending the term dictionary, document
// metadata, directory sections, and the 256-byte footer. If this writer
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

	// Ensure the encoder stream is opened even when WriteTerm was never called,
	// so the postings section is written with valid (empty) state.
	if !w.started {
		if w.docCount > 0 {
			if err := w.encoder.beginStream(w.w, w.docCount); err != nil {
				return fmt.Errorf("begin postings stream: %w", err)
			}
		}
		w.started = true
	}

	var (
		postingsDataSize int64
		postingsDir      []postingsBlockDirEntry
		err              error
	)

	if w.docCount > 0 || len(w.terms) > 0 {
		postingsDataSize, postingsDir, err = w.encoder.endStream()
		if err != nil {
			return fmt.Errorf("end postings stream: %w", err)
		}
		if postingsDataSize < 0 {
			return fmt.Errorf("invalid postings data size: %d", postingsDataSize)
		}
	}

	// Validate document count matches what was promised at construction.
	if uint32(len(w.docs)) != w.docCount {
		return fmt.Errorf("document count mismatch: added %d documents but declared %d", len(w.docs), w.docCount)
	}

	// Terms arrive pre-sorted from the k-way merge. Do NOT call normalizeTermBitmaps:
	// sorting would reassign term IDs and corrupt the postings section, which was
	// already flushed in sorted order.
	termData, termDir, err := buildTermData(w.terms)
	if err != nil {
		return fmt.Errorf("build term data: %w", err)
	}

	docsSection := encodeDocumentMetadata(w.docs)

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
	if _, err := w.w.Write(docsSection); err != nil {
		return fmt.Errorf("write document metadata: %w", err)
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
		DocumentCount:        uint32(len(w.docs)),
		TermBlockCount:       uint32(len(termDir)),
		PostingsBlockCount:   uint32(len(postingsDir)),
		PostingsCompression:  w.encoder.CompressionType(),
		TermCount:            uint64(len(w.terms)),
		PostingsDataSize:     uint64(postingsDataSize),
		TermDataSize:         uint64(len(termData)),
		DocMetadataSize:      uint64(len(docsSection)),
		TermBlockDirSize:     uint64(len(termDirSection)),
		PostingsBlockDirSize: uint64(len(postingsDirSection)),
	}

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
