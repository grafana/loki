package chunkenc

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"math"
	"sort"
	"time"

	"github.com/Workiva/go-datastructures/rangetree"
	"github.com/cespare/xxhash/v2"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/pkg/labels"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/log"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
)

var (
	noopStreamPipeline = log.NewNoopPipeline().ForStream(labels.Labels{})
)

type unorderedHeadBlock struct {
	// Opted for range tree over skiplist for space reduction.
	// Inserts: O(log(n))
	// Scans: (O(k+log(n))) where k=num_scanned_entries & n=total_entries
	rt rangetree.RangeTree

	lines      int   // number of entries
	size       int   // size of uncompressed bytes.
	mint, maxt int64 // upper and lower bounds
}

func newUnorderedHeadBlock() *unorderedHeadBlock {
	return &unorderedHeadBlock{
		rt: rangetree.New(1),
	}
}

func (hb *unorderedHeadBlock) isEmpty() bool {
	return hb.size == 0
}

// collection of entries belonging to the same nanosecond
type nsEntries struct {
	ts      int64
	entries []string
}

func (e *nsEntries) ValueAtDimension(_ uint64) int64 {
	return e.ts
}

func (hb *unorderedHeadBlock) append(ts int64, line string) {
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
		e.entries = append(displaced[0].(*nsEntries).entries, line)
	} else {
		e.entries = []string{line}
	}

	// Update hb metdata
	if hb.size == 0 || hb.mint > ts {
		hb.mint = ts
	}

	if hb.maxt < ts {
		hb.maxt = ts
	}

	hb.size += len(line)
	hb.lines++

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
	entryFn func(int64, string) error, // returning an error exits early
) (err error) {
	if hb.isEmpty() || (maxt < hb.mint || hb.maxt < mint) {
		return
	}

	entries := hb.rt.Query(interval{
		mint: mint,
		maxt: maxt,
	})

	chunkStats := stats.GetChunkData(ctx)
	process := func(es *nsEntries) {
		chunkStats.HeadChunkLines += int64(len(es.entries))

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
			line := es.entries[i]
			chunkStats.HeadChunkBytes += int64(len(line))
			err = entryFn(es.ts, line)

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

func (hb *unorderedHeadBlock) iterator(
	ctx context.Context,
	direction logproto.Direction,
	mint,
	maxt int64,
	pipeline log.StreamPipeline,
) iter.EntryIterator {

	// We are doing a copy everytime, this is because b.entries could change completely,
	// the alternate would be that we allocate a new b.entries everytime we cut a block,
	// but the tradeoff is that queries to near-realtime data would be much lower than
	// cutting of blocks.
	streams := map[uint64]*logproto.Stream{}

	_ = hb.forEntries(
		ctx,
		direction,
		mint,
		maxt,
		func(ts int64, line string) error {
			newLine, parsedLbs, ok := pipeline.ProcessString(line)
			if !ok {
				return nil
			}

			var stream *logproto.Stream
			lhash := parsedLbs.Hash()
			if stream, ok = streams[lhash]; !ok {
				stream = &logproto.Stream{
					Labels: parsedLbs.String(),
				}
				streams[lhash] = stream
			}

			stream.Entries = append(stream.Entries, logproto.Entry{
				Timestamp: time.Unix(0, ts),
				Line:      newLine,
			})
			return nil
		},
	)

	if len(streams) == 0 {
		return iter.NoopIterator
	}
	streamsResult := make([]logproto.Stream, 0, len(streams))
	for _, stream := range streams {
		streamsResult = append(streamsResult, *stream)
	}
	return iter.NewStreamsIterator(ctx, streamsResult, direction)
}

func (hb *unorderedHeadBlock) sampleIterator(
	ctx context.Context,
	mint,
	maxt int64,
	extractor log.StreamSampleExtractor,
) iter.SampleIterator {

	series := map[uint64]*logproto.Series{}

	_ = hb.forEntries(
		ctx,
		logproto.FORWARD,
		mint,
		maxt,
		func(ts int64, line string) error {
			value, parsedLabels, ok := extractor.ProcessString(line)
			if !ok {
				return nil
			}
			var found bool
			var s *logproto.Series
			lhash := parsedLabels.Hash()
			if s, found = series[lhash]; !found {
				s = &logproto.Series{
					Labels: parsedLabels.String(),
				}
				series[lhash] = s
			}

			// []byte here doesn't create allocation because Sum64 has go:noescape directive
			// It specifies that the function does not allow any of the pointers passed as arguments
			// to escape into the heap or into the values returned from the function.
			h := xxhash.Sum64([]byte(line))
			s.Samples = append(s.Samples, logproto.Sample{
				Timestamp: ts,
				Value:     value,
				Hash:      h,
			})
			return nil
		},
	)

	if len(series) == 0 {
		return iter.NoopIterator
	}
	seriesRes := make([]logproto.Series, 0, len(series))
	for _, s := range series {
		// todo(ctovena) not sure we need this sort.
		sort.Sort(s)
		seriesRes = append(seriesRes, *s)
	}
	return iter.NewMultiSeriesIterator(ctx, seriesRes)
}

// serialise is used in creating an ordered, compressed block from an unorderedHeadBlock
func (hb *unorderedHeadBlock) serialise(pool WriterPool) ([]byte, error) {
	inBuf := serializeBytesBufferPool.Get().(*bytes.Buffer)
	defer func() {
		inBuf.Reset()
		serializeBytesBufferPool.Put(inBuf)
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
		func(ts int64, line string) error {
			n := binary.PutVarint(encBuf, ts)
			inBuf.Write(encBuf[:n])

			n = binary.PutUvarint(encBuf, uint64(len(line)))
			inBuf.Write(encBuf[:n])

			inBuf.WriteString(line)
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

// CheckpointSize returns the estimated size of the headblock checkpoint.
func (hb *unorderedHeadBlock) CheckpointSize(version byte) int {
	size := 1                                                          // version
	size += binary.MaxVarintLen32 * 2                                  // total entries + total size
	size += binary.MaxVarintLen64 * 2                                  // mint,maxt
	size += (binary.MaxVarintLen64 + binary.MaxVarintLen32) * hb.lines // ts + len of log line.
	size += hb.size                                                    // uncompressed bytes of lines
	return size
}

// CheckpointBytes serializes a headblock to []byte. This is used by the WAL checkpointing,
// which does not want to mutate a chunk by cutting it (otherwise risking content address changes), but
// needs to serialize/deserialize the data to disk to ensure data durability.
func (hb *unorderedHeadBlock) CheckpointBytes(version byte, b []byte) ([]byte, error) {
	buf := bytes.NewBuffer(b[:0])
	err := hb.CheckpointTo(version, buf)
	return buf.Bytes(), err
}

// CheckpointTo serializes a headblock to a `io.Writer`. see `CheckpointBytes`.
func (hb *unorderedHeadBlock) CheckpointTo(version byte, w io.Writer) error {
	eb := EncodeBufferPool.Get().(*encbuf)
	defer EncodeBufferPool.Put(eb)

	eb.reset()

	eb.putByte(version)
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
		func(ts int64, line string) error {
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
			return nil
		},
	)

	return nil
}

func (hb *unorderedHeadBlock) FromCheckpoint(b []byte) error {
	// ensure it's empty
	*hb = *newUnorderedHeadBlock()

	if len(b) < 1 {
		return nil
	}

	db := decbuf{b: b}

	version := db.byte()
	if db.err() != nil {
		return errors.Wrap(db.err(), "verifying headblock header")
	}
	switch version {
	case chunkFormatV1, chunkFormatV2, chunkFormatV3:
	default:
		return errors.Errorf("incompatible headBlock version (%v), only V1,V2,V3 is currently supported", version)
	}

	n := db.uvarint()

	if err := db.err(); err != nil {
		return errors.Wrap(err, "verifying headblock metadata")
	}

	for i := 0; i < n && db.err() == nil; i++ {
		ts := db.varint64()
		lineLn := db.uvarint()
		line := string(db.bytes(lineLn))
		hb.append(ts, line)
	}

	if err := db.err(); err != nil {
		return errors.Wrap(err, "decoding entries")
	}

	return nil
}
