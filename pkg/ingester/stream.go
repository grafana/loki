package ingester

import (
	"context"
	"time"

	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
)

var (
	chunksCreatedTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "ingester_chunks_created_total",
		Help:      "The total number of chunks created in the ingester.",
	})
	chunksFlushedTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "ingester_chunks_flushed_total",
		Help:      "The total number of chunks flushed by the ingester.",
	})
	samplesPerChunk = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "loki",
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

type stream struct {
	// Newest chunk at chunks[n-1].
	// Not thread-safe; assume accesses to this are locked by caller.
	chunks []chunkDesc
	fp     model.Fingerprint
	labels []client.LabelPair
}

type chunkDesc struct {
	chunk   chunkenc.Chunk
	closed  bool
	flushed time.Time
}

func newStream(fp model.Fingerprint, labels []client.LabelPair) *stream {
	return &stream{
		fp:     fp,
		labels: labels,
	}
}

func (s *stream) Push(_ context.Context, entries []logproto.Entry) error {
	if len(s.chunks) == 0 {
		s.chunks = append(s.chunks, chunkDesc{
			chunk: chunkenc.NewMemChunk(chunkenc.EncGZIP),
		})
		chunksCreatedTotal.Inc()
	}

	for i := range entries {
		if s.chunks[0].closed || !s.chunks[0].chunk.SpaceFor(&entries[i]) {
			samplesPerChunk.Observe(float64(s.chunks[0].chunk.Size()))
			s.chunks = append(s.chunks, chunkDesc{
				chunk: chunkenc.NewMemChunk(chunkenc.EncGZIP),
			})
			chunksCreatedTotal.Inc()
		}
		if err := s.chunks[len(s.chunks)-1].chunk.Append(&entries[i]); err != nil {
			return err
		}
	}

	return nil
}

// Returns an iterator.
func (s *stream) Iterator(from, through time.Time, direction logproto.Direction) (iter.EntryIterator, error) {
	iterators := make([]iter.EntryIterator, 0, len(s.chunks))
	for _, c := range s.chunks {
		iter, err := c.chunk.Iterator(from, through, direction)
		if err != nil {
			return nil, err
		}
		if iter != nil {
			iterators = append(iterators, iter)
		}
	}

	if direction != logproto.FORWARD {
		for left, right := 0, len(iterators)-1; left < right; left, right = left+1, right-1 {
			iterators[left], iterators[right] = iterators[right], iterators[left]
		}
	}

	return iter.NewNonOverlappingIterator(iterators, client.FromLabelPairsToLabels(s.labels).String()), nil
}
