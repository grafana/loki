package ingester

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/pkg/labels"

	"github.com/grafana/tempo/pkg/chunkenc"
	"github.com/grafana/tempo/pkg/iter"
	"github.com/grafana/tempo/pkg/logproto"
)

var (
	chunksCreatedTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "tempo",
		Name:      "ingester_chunks_created_total",
		Help:      "The total number of chunks created in the ingester.",
	})
	chunksFlushedTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "tempo",
		Name:      "ingester_chunks_flushed_total",
		Help:      "The total number of chunks flushed by the ingester.",
	})

	samplesPerChunk = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "tempo",
		Subsystem: "ingester",
		Name:      "samples_per_chunk",
		Help:      "The number of samples in a chunk.",

		Buckets: prometheus.LinearBuckets(4096, 2048, 6),
	})
)

func init() {
	prometheus.MustRegister(chunksCreatedTotal)
	prometheus.MustRegister(chunksFlushedTotal)
	prometheus.MustRegister(samplesPerChunk)
}

const tmpMaxChunks = 3

type stream struct {
	// Newest chunk at chunks[0].
	// Not thread-safe; assume accesses to this are locked by caller.
	chunks []chunkenc.Chunk
	labels labels.Labels
}

func newStream(labels labels.Labels) *stream {
	return &stream{
		labels: labels,
	}
}

func (s *stream) Push(_ context.Context, entries []logproto.Entry) error {
	if len(s.chunks) == 0 {
		s.chunks = append(s.chunks, chunkenc.NewMemChunk(chunkenc.EncGZIP))
		chunksCreatedTotal.Inc()
	}

	for i := range entries {
		if !s.chunks[0].SpaceFor(&entries[i]) {
			samplesPerChunk.Observe(float64(s.chunks[0].Size()))
			s.chunks = append([]chunkenc.Chunk{chunkenc.NewMemChunk(chunkenc.EncGZIP)}, s.chunks...)
			chunksCreatedTotal.Inc()
		}
		if err := s.chunks[0].Append(&entries[i]); err != nil {
			return err
		}
	}

	// Temp; until we implement flushing, only keep N chunks in memory.
	if len(s.chunks) > tmpMaxChunks {
		chunksFlushedTotal.Add(float64(len(s.chunks) - tmpMaxChunks))
		s.chunks = s.chunks[:tmpMaxChunks]
	}
	return nil
}

// Returns an iterator.
func (s *stream) Iterator(from, through time.Time, direction logproto.Direction) (iter.EntryIterator, error) {
	iterators := make([]iter.EntryIterator, 0, len(s.chunks))
	for _, c := range s.chunks {
		iter, err := c.Iterator(from, through, direction)
		if err != nil {
			return nil, err
		}
		if iter != nil {
			iterators = append(iterators, iter)
		}
	}

	if direction == logproto.FORWARD {
		for left, right := 0, len(iterators)-1; left < right; left, right = left+1, right-1 {
			iterators[left], iterators[right] = iterators[right], iterators[left]
		}
	}

	return iter.NewNonOverlappingIterator(iterators, s.labels.String()), nil
}
