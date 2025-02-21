package chunkenc

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/Workiva/go-datastructures/rangetree"
	"github.com/cespare/xxhash/v2"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/compression"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
)

type organisedHeadBlock struct {
	unorderedHeadBlock
}

func newOrganisedHeadBlock(fmt HeadBlockFmt, symbolizer *symbolizer) *organisedHeadBlock {
	return &organisedHeadBlock{
		unorderedHeadBlock: unorderedHeadBlock{
			format:     fmt,
			symbolizer: symbolizer,
			rt:         rangetree.New(1),
		},
	}
}

// Serialise is used in creating an ordered, compressed block from an organisedHeadBlock
func (b *organisedHeadBlock) SerialiseBlock(pool compression.WriterPool) (ts []byte, lines []byte, sm []byte, err error) {
	linesInBuf := serializeBytesBufferPool.Get().(*bytes.Buffer)
	symbolsSectionBuf := serializeBytesBufferPool.Get().(*bytes.Buffer)

	defer func() {
		symbolsSectionBuf.Reset()
		serializeBytesBufferPool.Put(symbolsSectionBuf)

		linesInBuf.Reset()
		serializeBytesBufferPool.Put(linesInBuf)
	}()

	tsBuf := &bytes.Buffer{}
	linesBuf := &bytes.Buffer{}
	smBuf := &bytes.Buffer{}

	compressedWriter := pool.GetWriter(linesBuf)
	defer pool.PutWriter(compressedWriter)

	encBuf := make([]byte, binary.MaxVarintLen64)

	_ = b.forEntries(context.Background(), logproto.FORWARD, 0, math.MaxInt64,
		func(_ *stats.Context, ts int64, line string, smSymbols symbols) error {
			var n int
			n = binary.PutVarint(encBuf, ts)
			tsBuf.Write(encBuf[:n])

			n = binary.PutUvarint(encBuf, uint64(len(line)))
			linesInBuf.Write(encBuf[:n])
			linesInBuf.WriteString(line)

			symbolsSectionBuf.Reset()
			n = binary.PutUvarint(encBuf, uint64(len(smSymbols)))
			symbolsSectionBuf.Write(encBuf[:n])

			for _, l := range smSymbols {
				n = binary.PutUvarint(encBuf, uint64(l.Name))
				symbolsSectionBuf.Write(encBuf[:n])

				n = binary.PutUvarint(encBuf, uint64(l.Value))
				symbolsSectionBuf.Write(encBuf[:n])
			}

			// write the length of symbols section
			n = binary.PutUvarint(encBuf, uint64(symbolsSectionBuf.Len()))
			smBuf.Write(encBuf[:n])
			smBuf.Write(symbolsSectionBuf.Bytes())

			return nil
		},
	)

	if _, err := compressedWriter.Write(linesInBuf.Bytes()); err != nil {
		return nil, nil, nil, errors.Wrap(err, "appending entry")
	}
	if err := compressedWriter.Close(); err != nil {
		return nil, nil, nil, errors.Wrap(err, "flushing pending compress buffer")
	}

	return tsBuf.Bytes(), linesBuf.Bytes(), smBuf.Bytes(), nil
}

func (b *organisedHeadBlock) CompressedBlock(pool compression.WriterPool) (block, int, error) {
	ts, lines, sm, err := b.SerialiseBlock(pool)
	if err != nil {
		return block{}, 0, err
	}
	mint, maxt := b.Bounds()

	return block{
		b:          lines,
		numEntries: b.Entries(),
		mint:       mint,
		maxt:       maxt,
		sm:         sm,
		ts:         ts,
	}, len(lines), nil
}

// todo (shantanu): rename these iterators to something meaningful
// newOrganizedSampleIterator iterates over new block format v5.
func newOrganizedSampleIterator(ctx context.Context, pool compression.ReaderPool, b []byte, format byte, extractor log.StreamSampleExtractor, symbolizer *symbolizer, sm []byte, ts []byte, queryMetricsOnly bool) iter.SampleIterator {
	return &sampleOrganizedBufferedIterator{
		organizedBufferedIterator: newOrganizedBufferedIterator(ctx, pool, b, format, symbolizer, sm, ts, queryMetricsOnly),
		extractor:                 extractor,
		stats:                     stats.FromContext(ctx),
	}
}

func newOrganizedBufferedIterator(ctx context.Context, pool compression.ReaderPool, b []byte, format byte, symbolizer *symbolizer, sm []byte, ts []byte, queryMetricsOnly bool) *organizedBufferedIterator {
	st := stats.FromContext(ctx)
	st.AddCompressedBytes(int64(len(b)))
	return &organizedBufferedIterator{
		origBytes: b,
		stats:     st,

		pool:       pool,
		symbolizer: symbolizer,

		format:           format,
		tsBytes:          ts,
		smBytes:          sm,
		queryMetricsOnly: queryMetricsOnly,
	}
}

type organizedBufferedIterator struct {
	origBytes []byte
	stats     *stats.Context

	reader io.Reader

	pool       compression.ReaderPool
	symbolizer *symbolizer

	err error

	readBuf      [20]byte // Enough bytes to store two varints.
	readBufValid int      // How many bytes are left in readBuf from previous read.

	format   byte
	buf      []byte // The buffer for a single entry.
	currLine []byte // the current line, this is the same as the buffer but sliced the line size.
	currTs   int64

	symbolsBuf             []symbol      // The buffer for a single entry's symbols.
	currStructuredMetadata labels.Labels // The current labels.

	closed bool

	// Buffers and readers for structured metadata bytes
	smBytes []byte
	smBuf   []symbol

	// Buffers and readers for timestamp bytes
	tsBytes          []byte
	queryMetricsOnly bool
}

func (e *organizedBufferedIterator) Next() bool {
	var decompressedBytes, decompressedStructuredMetadataBytes int64
	var line []byte
	if !e.closed && e.reader == nil && !e.queryMetricsOnly {
		var err error

		e.reader, err = e.pool.GetReader(bytes.NewReader(e.origBytes))
		if err != nil {
			e.err = err
			return false
		}
	}

	// todo (shantanu): need a better way to close the iterator instead of individually doing this.
	ts, ok := e.nextTs()

	if !ok {
		e.Close()
		return false
	}
	// Add timestamp bytes
	decompressedBytes += binary.MaxVarintLen64

	if !e.queryMetricsOnly {
		var lineBytes int64
		line, ok, lineBytes = e.nextLine()
		if !ok {
			e.Close()
			return false
		}
		decompressedBytes += lineBytes
	}

	structuredMetadata, ok := e.nextMetadata()
	if ok && len(e.smBuf) > 0 {
		// Count the section length varint
		decompressedStructuredMetadataBytes += binary.MaxVarintLen64
		// Count the number of symbols varint
		decompressedStructuredMetadataBytes += binary.MaxVarintLen64
		// For each symbol we read both name and value as varints
		decompressedStructuredMetadataBytes += int64(len(e.smBuf) * 2 * binary.MaxVarintLen64)

		e.stats.AddDecompressedStructuredMetadataBytes(decompressedStructuredMetadataBytes)
		decompressedBytes += decompressedStructuredMetadataBytes
	}

	e.currTs = ts
	e.currLine = line
	e.currStructuredMetadata = structuredMetadata

	e.stats.AddDecompressedBytes(decompressedBytes)
	return true
}

func (e *organizedBufferedIterator) nextTs() (ts int64, ok bool) {
	if len(e.tsBytes) == 0 {
		return 0, false
	}

	ts, n := binary.Varint(e.tsBytes)
	if n <= 0 {
		return 0, false
	}
	e.tsBytes = e.tsBytes[n:]

	return ts, true
}

func (e *organizedBufferedIterator) nextLine() ([]byte, bool, int64) {
	var decompressedBytes int64
	var lw, lineSize, lastAttempt int

	for lw == 0 {
		n, err := e.reader.Read(e.readBuf[e.readBufValid:])
		if err != nil {
			if err != io.EOF {
				e.err = err
				return nil, false, 0
			}
			if e.readBufValid == 0 { // Got EOF and no data in the buffer.
				return nil, false, 0
			}
			if e.readBufValid == lastAttempt { // Got EOF and could not parse same data last time.
				e.err = fmt.Errorf("invalid data in chunk")
				return nil, false, 0
			}

		}
		var l uint64
		e.readBufValid += n
		l, lw = binary.Uvarint(e.readBuf[:e.readBufValid])
		lineSize = int(l)

		lastAttempt = e.readBufValid
	}
	// line length varint
	decompressedBytes += binary.MaxVarintLen64

	if lineSize >= maxLineLength {
		e.err = fmt.Errorf("line too long %d, max limit: %d", lineSize, maxLineLength)
		return nil, false, 0
	}

	// if the buffer is small, we get a new one
	if e.buf == nil || lineSize > cap(e.buf) {
		if e.buf != nil {
			BytesBufferPool.Put(e.buf)
		}
		e.buf = BytesBufferPool.Get(lineSize).([]byte)
		if lineSize > cap(e.buf) {
			e.err = fmt.Errorf("could not get a line buffer of size %d, actual %d", lineSize, cap(e.buf))
			return nil, false, 0
		}
	}

	e.buf = e.buf[:lineSize]
	n := copy(e.buf, e.readBuf[lw:e.readBufValid])
	e.readBufValid = copy(e.readBuf[:], e.readBuf[lw+n:e.readBufValid])

	for n < lineSize {
		r, err := e.reader.Read(e.buf[n:lineSize])
		n += r
		if err != nil {
			// We might get EOF after reading enough bytes to fill the buffer, which is OK.
			// EOF and zero bytes read when the buffer isn't full is an error.
			if err == io.EOF && r != 0 {
				continue
			}
			e.err = err
			return nil, false, 0
		}
	}

	decompressedBytes += int64(lineSize)
	e.stats.AddDecompressedLines(1)
	return e.buf[:lineSize], true, decompressedBytes
}

func (e *organizedBufferedIterator) nextMetadata() (labels.Labels, bool) {
	// [width of buffer][number of symbols][name][value]...
	if len(e.smBytes) == 0 {
		return nil, false
	}
	_, n := binary.Uvarint(e.smBytes)
	if n <= 0 {
		return nil, false
	}
	e.smBytes = e.smBytes[n:]
	sm, n := binary.Uvarint(e.smBytes)
	if n <= 0 {
		return nil, false
	}
	smLength := int(sm)
	e.smBytes = e.smBytes[n:]
	if len(e.smBuf) < smLength {
		e.smBuf = make([]symbol, smLength)
	} else {
		e.smBuf = e.smBuf[:smLength]
	}
	var name, val uint64
	for i := 0; i < smLength; i++ {
		name, n = binary.Uvarint(e.smBytes)
		if n <= 0 {
			return nil, false
		}
		e.smBytes = e.smBytes[n:]
		val, n = binary.Uvarint(e.smBytes)
		if n <= 0 {
			return nil, false
		}
		e.smBytes = e.smBytes[n:]

		e.smBuf[i].Name = uint32(name)
		e.smBuf[i].Value = uint32(val)
	}

	return e.symbolizer.Lookup(e.smBuf[:smLength], e.currStructuredMetadata), true
}

func (e *organizedBufferedIterator) Err() error {
	return e.err
}

func (e *organizedBufferedIterator) Close() error {
	if !e.closed {
		e.closed = true
		e.close()
	}

	return e.err
}

func (e *organizedBufferedIterator) close() {
	if e.reader != nil {
		e.pool.PutReader(e.reader)
		e.reader = nil
	}

	if e.buf != nil {
		BytesBufferPool.Put(e.buf)
		e.buf = nil
	}

	if e.symbolsBuf != nil {
		SymbolsPool.Put(e.symbolsBuf)
		e.symbolsBuf = nil
	}

	if e.currStructuredMetadata != nil {
		structuredMetadataPool.Put(e.currStructuredMetadata) // nolint:staticcheck
		e.currStructuredMetadata = nil
	}

	e.origBytes = nil
}

type entryOrganizedBufferedIterator struct {
	*organizedBufferedIterator
	pipeline log.StreamPipeline
	stats    *stats.Context

	cur        logproto.Entry
	currLabels log.LabelsResult
}

func (e *entryOrganizedBufferedIterator) Labels() string {
	return e.currLabels.String()
}

func (e *entryOrganizedBufferedIterator) At() logproto.Entry {
	return e.cur
}

func (e *entryOrganizedBufferedIterator) StreamHash() uint64 {
	return e.pipeline.BaseLabels().Hash()
}

func (e *entryOrganizedBufferedIterator) Next() bool {
	for e.organizedBufferedIterator.Next() {
		newLine, lbs, matches := e.pipeline.Process(e.currTs, e.currLine, e.currStructuredMetadata...)
		if !matches {
			continue
		}

		e.stats.AddPostFilterLines(1)
		e.currLabels = lbs
		e.cur.Timestamp = time.Unix(0, e.currTs)
		e.cur.Line = string(newLine)
		e.cur.StructuredMetadata = logproto.FromLabelsToLabelAdapters(lbs.StructuredMetadata())
		e.cur.Parsed = logproto.FromLabelsToLabelAdapters(lbs.Parsed())

		return true
	}
	return false
}

func (e *entryOrganizedBufferedIterator) Close() error {
	if e.pipeline.ReferencedStructuredMetadata() {
		e.stats.SetQueryReferencedStructuredMetadata()
	}

	return e.organizedBufferedIterator.Close()
}

func newEntryOrganizedBufferedIterator(ctx context.Context, pool compression.ReaderPool, b []byte, pipeline log.StreamPipeline, format byte, symbolizer *symbolizer, sm []byte, ts []byte) iter.EntryIterator {
	return &entryOrganizedBufferedIterator{
		organizedBufferedIterator: newOrganizedBufferedIterator(ctx, pool, b, format, symbolizer, sm, ts, false),
		pipeline:                  pipeline,
		stats:                     stats.FromContext(ctx),
	}
}

type sampleOrganizedBufferedIterator struct {
	*organizedBufferedIterator

	extractor log.StreamSampleExtractor
	stats     *stats.Context

	cur              logproto.Sample
	currLabels       log.LabelsResult
	queryMetricsOnly bool
}

func (s *sampleOrganizedBufferedIterator) At() logproto.Sample {
	// todo(shantanu) : check how this hash is used and check deduping
	// might need another buffer to store hash of each log line separately
	return s.cur
}

func (s *sampleOrganizedBufferedIterator) StreamHash() uint64 {
	return s.extractor.BaseLabels().Hash()
}

func (s *sampleOrganizedBufferedIterator) Next() bool {
	for s.organizedBufferedIterator.Next() {
		val, labels, ok := s.extractor.Process(s.currTs, s.currLine, s.currStructuredMetadata...)
		if !ok {
			continue
		}
		s.stats.AddPostFilterLines(1)
		s.currLabels = labels
		s.cur.Value = val
		s.cur.Hash = xxhash.Sum64(s.currLine)
		s.cur.Timestamp = s.currTs
		return true
	}
	return false
}

func (s *sampleOrganizedBufferedIterator) Close() error {
	if s.extractor.ReferencedStructuredMetadata() {
		s.stats.SetQueryReferencedStructuredMetadata()
	}

	return s.organizedBufferedIterator.Close()
}

func (s *sampleOrganizedBufferedIterator) Labels() string {
	return s.currLabels.String()
}
