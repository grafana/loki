package chunkenc

import (
	"bytes"
	"context"
	"encoding/binary"
	"math"
	"time"

	"github.com/Workiva/go-datastructures/rangetree"
	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/log"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/pkg/labels"
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

type interval struct {
	mint, maxt int64
}

func (i interval) LowAtDimension(_ uint64) int64 { return i.mint }

// rangetree library treats this as inclusive, but we want exclusivity
func (i interval) HighAtDimension(_ uint64) int64 { return i.maxt - 1 }

func (hb *unorderedHeadBlock) iterator(
	ctx context.Context,
	direction logproto.Direction,
	mint,
	maxt int64,
	pipeline log.StreamPipeline,
) iter.EntryIterator {
	if hb.isEmpty() || (maxt < hb.mint || hb.maxt < mint) {
		return iter.NoopIterator
	}

	entries := hb.rt.Query(interval{
		mint: mint,
		maxt: maxt,
	})

	chunkStats := stats.GetChunkData(ctx)

	// We are doing a copy everytime, this is because b.entries could change completely,
	// the alternate would be that we allocate a new b.entries everytime we cut a block,
	// but the tradeoff is that queries to near-realtime data would be much lower than
	// cutting of blocks.
	streams := map[uint64]*logproto.Stream{}

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
			newLine, parsedLbs, ok := pipeline.ProcessString(line)
			if !ok {
				return
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
				Timestamp: time.Unix(0, es.ts),
				Line:      newLine,
			})
		}

	}

	if direction == logproto.FORWARD {
		for _, e := range entries {
			process(e.(*nsEntries))
		}
	} else {
		for i := len(entries) - 1; i >= 0; i-- {
			process(entries[i].(*nsEntries))
		}
	}

	if len(streams) == 0 {
		return iter.NoopIterator
	}
	streamsResult := make([]logproto.Stream, 0, len(streams))
	for _, stream := range streams {
		streamsResult = append(streamsResult, *stream)
	}
	return iter.NewStreamsIterator(ctx, streamsResult, direction)
}

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

	itr := hb.iterator(
		context.Background(),
		logproto.FORWARD,
		0,
		math.MaxInt64,
		noopStreamPipeline,
	)

	// TODO(owen-d): we don't have to reuse the iterator implementation here
	// and could avoid allocations due to re-casting the underlying types.
	for itr.Next() {
		e := itr.Entry()
		n := binary.PutVarint(encBuf, e.Timestamp.UnixNano())
		inBuf.Write(encBuf[:n])

		n = binary.PutUvarint(encBuf, uint64(len(e.Line)))
		inBuf.Write(encBuf[:n])

		inBuf.WriteString(e.Line)
	}

	if _, err := compressedWriter.Write(inBuf.Bytes()); err != nil {
		return nil, errors.Wrap(err, "appending entry")
	}
	if err := compressedWriter.Close(); err != nil {
		return nil, errors.Wrap(err, "flushing pending compress buffer")
	}

	return outBuf.Bytes(), nil
}
