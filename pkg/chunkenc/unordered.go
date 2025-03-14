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

var noopStreamPipeline = log.NewNoopPipeline().ForStream(labels.Labels{})

type HeadBlock interface {
	IsEmpty() bool
	CheckpointTo(w io.Writer) error
	CheckpointBytes(b []byte) ([]byte, error)
	CheckpointSize() int
	LoadBytes(b []byte) error
	Serialise(pool compression.WriterPool) ([]byte, error)
	Reset()
	Bounds() (mint, maxt int64)
	Entries() int
	UncompressedSize() int
	Convert(HeadBlockFmt, *symbolizer) (HeadBlock, error)
	Append(int64, string, labels.Labels) (bool, error)
	Iterator(
		ctx context.Context,
		direction logproto.Direction,
		mint,
		maxt int64,
		pipeline log.StreamPipeline,
	) iter.EntryIterator
	SampleIterator(
		ctx context.Context,
		mint,
		maxt int64,
		extractor ...log.StreamSampleExtractor,
	) iter.SampleIterator
	Format() HeadBlockFmt
}

type unorderedHeadBlock struct {
	format HeadBlockFmt
	// Opted for range tree over skiplist for space reduction.
	// Inserts: O(log(n))
	// Scans: (O(k+log(n))) where k=num_scanned_entries & n=total_entries
	rt rangetree.RangeTree

	symbolizer *symbolizer
	lines      int   // number of entries
	size       int   // size of uncompressed bytes.
	mint, maxt int64 // upper and lower bounds
}

func newUnorderedHeadBlock(headBlockFmt HeadBlockFmt, symbolizer *symbolizer) *unorderedHeadBlock {
	return &unorderedHeadBlock{
		format:     headBlockFmt,
		symbolizer: symbolizer,
		rt:         rangetree.New(1),
	}
}

func (hb *unorderedHeadBlock) Format() HeadBlockFmt { return hb.format }

func (hb *unorderedHeadBlock) IsEmpty() bool {
	return hb.size == 0
}

func (hb *unorderedHeadBlock) Bounds() (int64, int64) {
	return hb.mint, hb.maxt
}

func (hb *unorderedHeadBlock) Entries() int {
	return hb.lines
}

func (hb *unorderedHeadBlock) UncompressedSize() int {
	return hb.size
}

func (hb *unorderedHeadBlock) Reset() {
	x := newUnorderedHeadBlock(hb.format, hb.symbolizer)
	*hb = *x
}

type nsEntry struct {
	line                      string
	structuredMetadataSymbols symbols
}

// collection of entries belonging to the same nanosecond
type nsEntries struct {
	ts      int64
	entries []nsEntry
}

func (e *nsEntries) ValueAtDimension(_ uint64) int64 {
	return e.ts
}

// unorderedHeadBlock will return true if the entry is a duplicate, false otherwise
func (hb *unorderedHeadBlock) Append(ts int64, line string, structuredMetadata labels.Labels) (bool, error) {
	if hb.format < UnorderedWithStructuredMetadataHeadBlockFmt {
		// structuredMetadata must be ignored for the previous head block formats
		structuredMetadata = nil
	}
	// This is an allocation hack. The rangetree lib does not
	// support the ability to pass a "mutate" function during an insert
	// and instead will displace any existing entry at the specified timestamp.
	// Since Loki supports multiple lines per timestamp,
	// we insert an entry without any log lines,
	// which is ordered by timestamp alone.
	// Then, we detect if we've displaced any existing entries, and
	// append the new one to the existing, preallocated slice.
	// If not, we create a slice with one entry.
	e := &nsEntries{
		ts: ts,
	}
	displaced := hb.rt.Add(e)
	if displaced[0] != nil {
		// While we support multiple entries at the same timestamp, we _do_ de-duplicate
		// entries at the same time with the same content, iterate through any existing
		// entries and ignore the line if we already have an entry with the same content
		for _, et := range displaced[0].(*nsEntries).entries {
			if et.line == line {
				e.entries = displaced[0].(*nsEntries).entries
				return true, nil
			}
		}
		e.entries = append(displaced[0].(*nsEntries).entries, nsEntry{line, hb.symbolizer.Add(structuredMetadata)})
	} else {
		e.entries = []nsEntry{{line, hb.symbolizer.Add(structuredMetadata)}}
	}

	// Update hb metdata
	if hb.size == 0 || hb.mint > ts {
		hb.mint = ts
	}

	if hb.maxt < ts {
		hb.maxt = ts
	}

	hb.size += len(line)
	hb.size += len(structuredMetadata) * 2 * 4 // 4 bytes per label and value pair as structuredMetadataSymbols
	hb.lines++

	return false, nil
}

func metaLabelsLen(metaLabels labels.Labels) int {
	length := 0
	for _, label := range metaLabels {
		length += len(label.Name) + len(label.Value)
	}
	return length
}

// Implements rangetree.Interval
type interval struct {
	mint, maxt int64
}

func (i interval) LowAtDimension(_ uint64) int64 { return i.mint }

// rangetree library treats this as inclusive, but we want exclusivity,
// or [from, through) in nanoseconds
func (i interval) HighAtDimension(_ uint64) int64 { return i.maxt - 1 }

// helper for base logic across {Entry,Sample}Iterator
func (hb *unorderedHeadBlock) forEntries(
	ctx context.Context,
	direction logproto.Direction,
	mint,
	maxt int64,
	entryFn func(*stats.Context, int64, string, symbols) error, // returning an error exits early
) (err error) {
	if hb.IsEmpty() || (maxt < hb.mint || hb.maxt < mint) {
		return
	}

	entries := hb.rt.Query(interval{
		mint: mint,
		maxt: maxt,
	})

	chunkStats := stats.FromContext(ctx)
	process := func(es *nsEntries) {
		chunkStats.AddHeadChunkLines(int64(len(es.entries)))

		// preserve write ordering of entries with the same ts
		var i int
		if direction == logproto.BACKWARD {
			i = len(es.entries) - 1
		}
		next := func() {
			if direction == logproto.FORWARD {
				i++
			} else {
				i--
			}
		}

		for ; i < len(es.entries) && i >= 0; next() {
			line := es.entries[i].line
			structuredMetadataSymbols := es.entries[i].structuredMetadataSymbols
			structuredMetadataBytes := int64(2 * len(structuredMetadataSymbols) * 4) // 2 * num_symbols * 4 bytes(uint32)
			chunkStats.AddHeadChunkStructuredMetadataBytes(structuredMetadataBytes)
			chunkStats.AddHeadChunkBytes(int64(len(line)) + structuredMetadataBytes)

			err = entryFn(chunkStats, es.ts, line, structuredMetadataSymbols)

		}
	}

	if direction == logproto.FORWARD {
		for _, e := range entries {
			process(e.(*nsEntries))
			if err != nil {
				return err
			}
		}
	} else {
		for i := len(entries) - 1; i >= 0; i-- {
			process(entries[i].(*nsEntries))
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (hb *unorderedHeadBlock) Iterator(ctx context.Context, direction logproto.Direction, mint, maxt int64, pipeline log.StreamPipeline) iter.EntryIterator {
	// We are doing a copy everytime, this is because b.entries could change completely,
	// the alternate would be that we allocate a new b.entries everytime we cut a block,
	// but the tradeoff is that queries to near-realtime data would be much lower than
	// cutting of blocks.
	streams := map[string]*logproto.Stream{}
	baseHash := pipeline.BaseLabels().Hash()
	var structuredMetadata labels.Labels
	_ = hb.forEntries(
		ctx,
		direction,
		mint,
		maxt,
		func(statsCtx *stats.Context, ts int64, line string, structuredMetadataSymbols symbols) error {
			structuredMetadata = hb.symbolizer.Lookup(structuredMetadataSymbols, structuredMetadata)
			newLine, parsedLbs, matches := pipeline.ProcessString(ts, line, structuredMetadata...)
			if !matches {
				return nil
			}
			statsCtx.AddPostFilterLines(1)
			var stream *logproto.Stream
			labels := parsedLbs.String()
			var ok bool
			if stream, ok = streams[labels]; !ok {
				stream = &logproto.Stream{
					Labels: labels,
					Hash:   baseHash,
				}
				streams[labels] = stream
			}

			stream.Entries = append(stream.Entries, logproto.Entry{
				Timestamp:          time.Unix(0, ts),
				Line:               newLine,
				StructuredMetadata: logproto.FromLabelsToLabelAdapters(parsedLbs.StructuredMetadata()),
				Parsed:             logproto.FromLabelsToLabelAdapters(parsedLbs.Parsed()),
			})
			return nil
		},
	)

	if pipeline.ReferencedStructuredMetadata() {
		stats.FromContext(ctx).SetQueryReferencedStructuredMetadata()
	}
	if len(streams) == 0 {
		return iter.NoopEntryIterator
	}
	streamsResult := make([]logproto.Stream, 0, len(streams))
	for _, stream := range streams {
		streamsResult = append(streamsResult, *stream)
	}

	return iter.EntryIteratorWithClose(iter.NewStreamsIterator(streamsResult, direction), func() error {
		if structuredMetadata != nil {
			structuredMetadataPool.Put(structuredMetadata) // nolint:staticcheck
		}
		return nil
	})
}

func (hb *unorderedHeadBlock) SampleIterator(
	ctx context.Context,
	mint,
	maxt int64,
	extractor ...log.StreamSampleExtractor,
) iter.SampleIterator {
	series := map[string]*logproto.Series{}
	setQueryReferencedStructuredMetadata := false
	var structuredMetadata labels.Labels
	_ = hb.forEntries(
		ctx,
		logproto.FORWARD,
		mint,
		maxt,
		func(statsCtx *stats.Context, ts int64, line string, structuredMetadataSymbols symbols) error {
			structuredMetadata = hb.symbolizer.Lookup(structuredMetadataSymbols, structuredMetadata)

			for _, extractor := range extractor {
				value, lbls, ok := extractor.ProcessString(ts, line, structuredMetadata...)
				if !ok {
					return nil
				}
				var (
					found bool
					s     *logproto.Series
				)

				lblStr := lbls.String()
				s, found = series[lblStr]
				if !found {
					baseHash := extractor.BaseLabels().Hash()
					s = &logproto.Series{
						Labels:     lblStr,
						Samples:    SamplesPool.Get(hb.lines).([]logproto.Sample)[:0],
						StreamHash: baseHash,
					}
					series[lblStr] = s
				}
				s.Samples = append(s.Samples, logproto.Sample{
					Timestamp: ts,
					Value:     value,
					Hash:      xxhash.Sum64(unsafeGetBytes(line)),
				})
				if extractor.ReferencedStructuredMetadata() {
					setQueryReferencedStructuredMetadata = true
				}
			}
			statsCtx.AddPostFilterLines(1)
			return nil
		},
	)

	if setQueryReferencedStructuredMetadata {
		stats.FromContext(ctx).SetQueryReferencedStructuredMetadata()
	}

	if len(series) == 0 {
		return iter.NoopSampleIterator
	}
	seriesRes := make([]logproto.Series, 0, len(series))
	for _, s := range series {
		seriesRes = append(seriesRes, *s)
	}
	return iter.SampleIteratorWithClose(iter.NewMultiSeriesIterator(seriesRes), func() error {
		for _, s := range series {
			SamplesPool.Put(s.Samples)
		}
		if structuredMetadata != nil {
			structuredMetadataPool.Put(structuredMetadata) // nolint:staticcheck
		}
		return nil
	})
}

// nolint:unused
// serialise is used in creating an ordered, compressed block from an unorderedHeadBlock
func (hb *unorderedHeadBlock) Serialise(pool compression.WriterPool) ([]byte, error) {
	inBuf := serializeBytesBufferPool.Get().(*bytes.Buffer)
	defer func() {
		inBuf.Reset()
		serializeBytesBufferPool.Put(inBuf)
	}()

	symbolsSectionBuf := serializeBytesBufferPool.Get().(*bytes.Buffer)
	defer func() {
		symbolsSectionBuf.Reset()
		serializeBytesBufferPool.Put(symbolsSectionBuf)
	}()

	outBuf := &bytes.Buffer{}

	encBuf := make([]byte, binary.MaxVarintLen64)
	compressedWriter := pool.GetWriter(outBuf)
	defer pool.PutWriter(compressedWriter)

	_ = hb.forEntries(
		context.Background(),
		logproto.FORWARD,
		0,
		math.MaxInt64,
		func(_ *stats.Context, ts int64, line string, structuredMetadataSymbols symbols) error {
			n := binary.PutVarint(encBuf, ts)
			inBuf.Write(encBuf[:n])

			n = binary.PutUvarint(encBuf, uint64(len(line)))
			inBuf.Write(encBuf[:n])

			inBuf.WriteString(line)

			if hb.format >= UnorderedWithStructuredMetadataHeadBlockFmt {
				symbolsSectionBuf.Reset()
				// Serialize structured metadata symbols to symbolsSectionBuf so that we can find and write its length before
				// writing symbols section to inbuf since we can't estimate its size beforehand due to variable length encoding.

				// write the number of symbol pairs
				n = binary.PutUvarint(encBuf, uint64(len(structuredMetadataSymbols)))
				symbolsSectionBuf.Write(encBuf[:n])

				// write the symbols
				for _, l := range structuredMetadataSymbols {
					n = binary.PutUvarint(encBuf, uint64(l.Name))
					symbolsSectionBuf.Write(encBuf[:n])

					n = binary.PutUvarint(encBuf, uint64(l.Value))
					symbolsSectionBuf.Write(encBuf[:n])
				}

				// write the length of symbols section first
				n = binary.PutUvarint(encBuf, uint64(symbolsSectionBuf.Len()))
				inBuf.Write(encBuf[:n])

				// copy the symbols section
				inBuf.Write(symbolsSectionBuf.Bytes())
			}
			return nil
		},
	)

	if _, err := compressedWriter.Write(inBuf.Bytes()); err != nil {
		return nil, errors.Wrap(err, "appending entry")
	}
	if err := compressedWriter.Close(); err != nil {
		return nil, errors.Wrap(err, "flushing pending compress buffer")
	}

	return outBuf.Bytes(), nil
}

func (hb *unorderedHeadBlock) Convert(version HeadBlockFmt, symbolizer *symbolizer) (HeadBlock, error) {
	if hb.format == version {
		return hb, nil
	}
	out := version.NewBlock(symbolizer)

	err := hb.forEntries(
		context.Background(),
		logproto.FORWARD,
		0,
		math.MaxInt64,
		func(_ *stats.Context, ts int64, line string, structuredMetadataSymbols symbols) error {
			_, err := out.Append(ts, line, hb.symbolizer.Lookup(structuredMetadataSymbols, nil))
			return err
		},
	)
	return out, err
}

// CheckpointSize returns the estimated size of the headblock checkpoint.
func (hb *unorderedHeadBlock) CheckpointSize() int {
	size := 1                                                          // version
	size += binary.MaxVarintLen32 * 2                                  // total entries + total size
	size += binary.MaxVarintLen64 * 2                                  // mint,maxt
	size += (binary.MaxVarintLen64 + binary.MaxVarintLen32) * hb.lines // ts + len of log line.
	if hb.format >= UnorderedWithStructuredMetadataHeadBlockFmt {
		// number of labels of structured metadata stored for each log entry
		size += binary.MaxVarintLen32 * hb.lines
	}
	size += hb.size // uncompressed bytes of lines
	return size
}

// CheckpointBytes serializes a headblock to []byte. This is used by the WAL checkpointing,
// which does not want to mutate a chunk by cutting it (otherwise risking content address changes), but
// needs to serialize/deserialize the data to disk to ensure data durability.
func (hb *unorderedHeadBlock) CheckpointBytes(b []byte) ([]byte, error) {
	buf := bytes.NewBuffer(b[:0])
	err := hb.CheckpointTo(buf)
	return buf.Bytes(), err
}

// CheckpointTo serializes a headblock to a `io.Writer`. see `CheckpointBytes`.
func (hb *unorderedHeadBlock) CheckpointTo(w io.Writer) error {
	eb := EncodeBufferPool.Get().(*encbuf)
	defer EncodeBufferPool.Put(eb)

	eb.reset()

	eb.putByte(byte(hb.Format()))
	_, err := w.Write(eb.get())
	if err != nil {
		return errors.Wrap(err, "write headBlock version")
	}
	eb.reset()

	eb.putUvarint(hb.lines)

	_, err = w.Write(eb.get())
	if err != nil {
		return errors.Wrap(err, "write headBlock metas")
	}
	eb.reset()

	err = hb.forEntries(
		context.Background(),
		logproto.FORWARD,
		0,
		math.MaxInt64,
		func(_ *stats.Context, ts int64, line string, structuredMetadataSymbols symbols) error {
			eb.putVarint64(ts)
			eb.putUvarint(len(line))
			_, err = w.Write(eb.get())
			if err != nil {
				return errors.Wrap(err, "write headBlock entry ts")
			}
			eb.reset()

			_, err := io.WriteString(w, line)
			if err != nil {
				return errors.Wrap(err, "write headblock entry line")
			}

			if hb.format >= UnorderedWithStructuredMetadataHeadBlockFmt {
				// structured metadata
				eb.putUvarint(len(structuredMetadataSymbols))
				_, err = w.Write(eb.get())
				if err != nil {
					return errors.Wrap(err, "write headBlock entry meta labels length")
				}
				eb.reset()
				for _, l := range structuredMetadataSymbols {
					eb.putUvarint(int(l.Name))
					eb.putUvarint(int(l.Value))
					_, err = w.Write(eb.get())
					if err != nil {
						return errors.Wrap(err, "write headBlock entry structuredMetadataSymbols")
					}
					eb.reset()
				}
			}

			return nil
		},
	)

	return nil
}

func (hb *unorderedHeadBlock) LoadBytes(b []byte) error {
	// ensure it's empty
	*hb = *newUnorderedHeadBlock(hb.format, hb.symbolizer)

	if len(b) < 1 {
		return nil
	}

	db := decbuf{b: b}

	version := db.byte()
	if db.err() != nil {
		return errors.Wrap(db.err(), "verifying headblock header")
	}

	if version < UnorderedHeadBlockFmt.Byte() {
		return errors.Errorf("incompatible headBlock version (%v), only V4 and the next versions are currently supported", version)
	}

	n := db.uvarint()

	if err := db.err(); err != nil {
		return errors.Wrap(err, "verifying headblock metadata")
	}

	for i := 0; i < n && db.err() == nil; i++ {
		ts := db.varint64()
		lineLn := db.uvarint()
		line := string(db.bytes(lineLn))

		var structuredMetadataSymbols symbols
		if version >= UnorderedWithStructuredMetadataHeadBlockFmt.Byte() {
			metaLn := db.uvarint()
			if metaLn > 0 {
				structuredMetadataSymbols = make([]symbol, metaLn)
				for j := 0; j < metaLn && db.err() == nil; j++ {
					structuredMetadataSymbols[j] = symbol{
						Name:  uint32(db.uvarint()),
						Value: uint32(db.uvarint()),
					}
				}
			}
		}
		if _, err := hb.Append(ts, line, hb.symbolizer.Lookup(structuredMetadataSymbols, nil)); err != nil {
			return err
		}
	}

	if err := db.err(); err != nil {
		return errors.Wrap(err, "decoding entries")
	}

	return nil
}

// HeadFromCheckpoint handles reading any head block format and returning the desired form.
// This is particularly helpful replaying WALs from different configurations
// such as after enabling unordered writes.
func HeadFromCheckpoint(b []byte, desiredIfNotUnordered HeadBlockFmt, symbolizer *symbolizer) (HeadBlock, error) {
	if len(b) == 0 {
		return desiredIfNotUnordered.NewBlock(symbolizer), nil
	}

	db := decbuf{b: b}

	version := db.byte()
	if db.err() != nil {
		return nil, errors.Wrap(db.err(), "verifying headblock header")
	}
	format := HeadBlockFmt(version)
	if format > UnorderedWithStructuredMetadataHeadBlockFmt {
		return nil, fmt.Errorf("unexpected head block version: %v", format)
	}

	decodedBlock := format.NewBlock(symbolizer)
	if err := decodedBlock.LoadBytes(b); err != nil {
		return nil, err
	}

	if decodedBlock.Format() < UnorderedHeadBlockFmt && decodedBlock.Format() != desiredIfNotUnordered {
		return decodedBlock.Convert(desiredIfNotUnordered, nil)
	}
	return decodedBlock, nil
}
