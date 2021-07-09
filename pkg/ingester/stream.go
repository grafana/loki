package ingester

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	util_log "github.com/cortexproject/cortex/pkg/util/log"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/weaveworks/common/httpgrpc"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/log"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
)

var (
	chunksCreatedTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "ingester_chunks_created_total",
		Help:      "The total number of chunks created in the ingester.",
	})
	samplesPerChunk = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "loki",
		Subsystem: "ingester",
		Name:      "samples_per_chunk",
		Help:      "The number of samples in a chunk.",

		Buckets: prometheus.LinearBuckets(4096, 2048, 6),
	})
	blocksPerChunk = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "loki",
		Subsystem: "ingester",
		Name:      "blocks_per_chunk",
		Help:      "The number of blocks in a chunk.",

		Buckets: prometheus.ExponentialBuckets(5, 2, 6),
	})
)

var (
	ErrEntriesExist = errors.New("duplicate push - entries already exist")
)

func init() {
	prometheus.MustRegister(chunksCreatedTotal)
	prometheus.MustRegister(samplesPerChunk)
	prometheus.MustRegister(blocksPerChunk)
}

type line struct {
	ts      time.Time
	content string
}

type stream struct {
	cfg *Config
	// Newest chunk at chunks[n-1].
	// Not thread-safe; assume accesses to this are locked by caller.
	chunks   []chunkDesc
	fp       model.Fingerprint // possibly remapped fingerprint, used in the streams map
	chunkMtx sync.RWMutex

	labels       labels.Labels
	labelsString string
	lastLine     line
	metrics      *ingesterMetrics

	tailers   map[uint32]*tailer
	tailerMtx sync.RWMutex

	// entryCt is a counter which is incremented on each accepted entry.
	// This allows us to discard WAL entries during replays which were
	// already recovered via checkpoints. Historically out of order
	// errors were used to detect this, but this counter has been
	// introduced to facilitate removing the ordering constraint.
	entryCt int64
}

type chunkDesc struct {
	chunk   *chunkenc.MemChunk
	closed  bool
	synced  bool
	flushed time.Time

	lastUpdated time.Time
}

type entryWithError struct {
	entry *logproto.Entry
	e     error
}

func newStream(cfg *Config, fp model.Fingerprint, labels labels.Labels, metrics *ingesterMetrics) *stream {
	return &stream{
		cfg:          cfg,
		fp:           fp,
		labels:       labels,
		labelsString: labels.String(),
		tailers:      map[uint32]*tailer{},
		metrics:      metrics,
	}
}

// consumeChunk manually adds a chunk to the stream that was received during
// ingester chunk transfer.
func (s *stream) consumeChunk(_ context.Context, chunk *logproto.Chunk) error {
	c, err := chunkenc.NewByteChunk(chunk.Data, s.cfg.BlockSize, s.cfg.TargetChunkSize)
	if err != nil {
		return err
	}

	s.chunkMtx.Lock()
	defer s.chunkMtx.Unlock()
	s.chunks = append(s.chunks, chunkDesc{
		chunk: c,
	})
	chunksCreatedTotal.Inc()
	return nil
}

// setChunks is used during checkpoint recovery
func (s *stream) setChunks(chunks []Chunk) (bytesAdded, entriesAdded int, err error) {
	s.chunkMtx.Lock()
	defer s.chunkMtx.Unlock()
	chks, err := fromWireChunks(s.cfg, chunks)
	if err != nil {
		return 0, 0, err
	}
	s.chunks = chks
	for _, c := range s.chunks {
		entriesAdded += c.chunk.Size()
		bytesAdded += c.chunk.UncompressedSize()
	}
	return bytesAdded, entriesAdded, nil
}

func (s *stream) NewChunk() *chunkenc.MemChunk {
	return chunkenc.NewMemChunk(s.cfg.parsedEncoding, s.cfg.BlockSize, s.cfg.TargetChunkSize)
}

func (s *stream) Push(
	ctx context.Context,
	entries []logproto.Entry,
	// WAL record to add push contents to.
	// May be nil to disable this functionality.
	record *WALRecord,
	// Counter used in WAL replay to avoid duplicates.
	// If this is non-zero, the stream will reject entries
	// with a counter value less than or equal to it's own.
	// It is set to zero and thus bypassed outside of WAL replays.
	counter int64,
) (int, error) {
	s.chunkMtx.Lock()
	defer s.chunkMtx.Unlock()

	if counter > 0 && counter <= s.entryCt {
		return 0, ErrEntriesExist
	}

	var bytesAdded int
	prevNumChunks := len(s.chunks)
	var lastChunkTimestamp time.Time
	if prevNumChunks == 0 {
		s.chunks = append(s.chunks, chunkDesc{
			chunk: s.NewChunk(),
		})
		chunksCreatedTotal.Inc()
	} else {
		_, lastChunkTimestamp = s.chunks[len(s.chunks)-1].chunk.Bounds()
	}

	var storedEntries []logproto.Entry
	failedEntriesWithError := []entryWithError{}

	// Don't fail on the first append error - if samples are sent out of order,
	// we still want to append the later ones.
	for i := range entries {
		// If this entry matches our last appended line's timestamp and contents,
		// ignore it.
		//
		// This check is done at the stream level so it persists across cut and
		// flushed chunks.
		//
		// NOTE: it's still possible for duplicates to be appended if a stream is
		// deleted from inactivity.
		if entries[i].Timestamp.Equal(s.lastLine.ts) && entries[i].Line == s.lastLine.content {
			continue
		}

		chunk := &s.chunks[len(s.chunks)-1]
		if chunk.closed || !chunk.chunk.SpaceFor(&entries[i]) || s.cutChunkForSynchronization(entries[i].Timestamp, lastChunkTimestamp, chunk, s.cfg.SyncPeriod, s.cfg.SyncMinUtilization) {
			// If the chunk has no more space call Close to make sure anything in the head block is cut and compressed
			err := chunk.chunk.Close()
			if err != nil {
				// This should be an unlikely situation, returning an error up the stack doesn't help much here
				// so instead log this to help debug the issue if it ever arises.
				level.Error(util_log.WithContext(ctx, util_log.Logger)).Log("msg", "failed to Close chunk", "err", err)
			}
			chunk.closed = true

			samplesPerChunk.Observe(float64(chunk.chunk.Size()))
			blocksPerChunk.Observe(float64(chunk.chunk.BlockCount()))
			chunksCreatedTotal.Inc()

			s.chunks = append(s.chunks, chunkDesc{
				chunk: s.NewChunk(),
			})
			chunk = &s.chunks[len(s.chunks)-1]
			lastChunkTimestamp = time.Time{}
		}
		if err := chunk.chunk.Append(&entries[i]); err != nil {
			failedEntriesWithError = append(failedEntriesWithError, entryWithError{&entries[i], err})
		} else {
			storedEntries = append(storedEntries, entries[i])
			lastChunkTimestamp = entries[i].Timestamp
			s.lastLine.ts = lastChunkTimestamp
			s.lastLine.content = entries[i].Line
			s.entryCt++

			// length of string plus
			bytesAdded += len(entries[i].Line)
		}
		chunk.lastUpdated = time.Now()
	}

	if len(storedEntries) != 0 {
		// record will be nil when replaying the wal (we don't want to rewrite wal entries as we replay them).
		if record != nil {
			record.AddEntries(uint64(s.fp), s.entryCt, storedEntries...)
		} else {
			// If record is nil, this is a WAL recovery.
			s.metrics.recoveredEntriesTotal.Add(float64(len(storedEntries)))
		}

		s.tailerMtx.RLock()
		hasTailers := len(s.tailers) != 0
		s.tailerMtx.RUnlock()
		if hasTailers {
			go func() {
				stream := logproto.Stream{Labels: s.labelsString, Entries: storedEntries}

				closedTailers := []uint32{}

				s.tailerMtx.RLock()
				for _, tailer := range s.tailers {
					if tailer.isClosed() {
						closedTailers = append(closedTailers, tailer.getID())
						continue
					}
					tailer.send(stream, s.labels)
				}
				s.tailerMtx.RUnlock()

				if len(closedTailers) != 0 {
					s.tailerMtx.Lock()
					defer s.tailerMtx.Unlock()

					for _, closedTailerID := range closedTailers {
						delete(s.tailers, closedTailerID)
					}
				}
			}()
		}

	}

	if len(failedEntriesWithError) > 0 {
		lastEntryWithErr := failedEntriesWithError[len(failedEntriesWithError)-1]
		if lastEntryWithErr.e == chunkenc.ErrOutOfOrder {
			// return bad http status request response with all failed entries
			buf := bytes.Buffer{}
			streamName := s.labelsString

			limitedFailedEntries := failedEntriesWithError
			if maxIgnore := s.cfg.MaxReturnedErrors; maxIgnore > 0 && len(limitedFailedEntries) > maxIgnore {
				limitedFailedEntries = limitedFailedEntries[:maxIgnore]
			}

			for _, entryWithError := range limitedFailedEntries {
				fmt.Fprintf(&buf,
					"entry with timestamp %s ignored, reason: '%s' for stream: %s,\n",
					entryWithError.entry.Timestamp.String(), entryWithError.e.Error(), streamName)
			}

			fmt.Fprintf(&buf, "total ignored: %d out of %d", len(failedEntriesWithError), len(entries))

			return bytesAdded, httpgrpc.Errorf(http.StatusBadRequest, buf.String())
		}
		return bytesAdded, lastEntryWithErr.e
	}

	if len(s.chunks) != prevNumChunks {
		memoryChunks.Add(float64(len(s.chunks) - prevNumChunks))
	}
	return bytesAdded, nil
}

// Returns true, if chunk should be cut before adding new entry. This is done to make ingesters
// cut the chunk for this stream at the same moment, so that new chunk will contain exactly the same entries.
func (s *stream) cutChunkForSynchronization(entryTimestamp, prevEntryTimestamp time.Time, c *chunkDesc, synchronizePeriod time.Duration, minUtilization float64) bool {
	if synchronizePeriod <= 0 || prevEntryTimestamp.IsZero() {
		return false
	}

	// we use fingerprint as a jitter here, basically offsetting stream synchronization points to different
	// this breaks if streams are mapped to different fingerprints on different ingesters, which is too bad.
	cts := (uint64(entryTimestamp.UnixNano()) + uint64(s.fp)) % uint64(synchronizePeriod.Nanoseconds())
	pts := (uint64(prevEntryTimestamp.UnixNano()) + uint64(s.fp)) % uint64(synchronizePeriod.Nanoseconds())

	// if current entry timestamp has rolled over synchronization period
	if cts < pts {
		if minUtilization <= 0 {
			c.synced = true
			return true
		}

		if c.chunk.Utilization() > minUtilization {
			c.synced = true
			return true
		}
	}

	return false
}

func (s *stream) Bounds() (from, to time.Time) {
	s.chunkMtx.RLock()
	defer s.chunkMtx.RUnlock()
	if len(s.chunks) > 0 {
		from, _ = s.chunks[0].chunk.Bounds()
		_, to = s.chunks[len(s.chunks)-1].chunk.Bounds()
	}
	return from, to
}

// Returns an iterator.
func (s *stream) Iterator(ctx context.Context, ingStats *stats.IngesterData, from, through time.Time, direction logproto.Direction, pipeline log.StreamPipeline) (iter.EntryIterator, error) {
	s.chunkMtx.RLock()
	defer s.chunkMtx.RUnlock()
	iterators := make([]iter.EntryIterator, 0, len(s.chunks))
	for _, c := range s.chunks {
		itr, err := c.chunk.Iterator(ctx, from, through, direction, pipeline)
		if err != nil {
			return nil, err
		}
		if itr != nil {
			iterators = append(iterators, itr)
		}
	}

	if direction != logproto.FORWARD {
		for left, right := 0, len(iterators)-1; left < right; left, right = left+1, right-1 {
			iterators[left], iterators[right] = iterators[right], iterators[left]
		}
	}

	if ingStats != nil {
		ingStats.TotalChunksMatched += int64(len(s.chunks))
	}
	return iter.NewNonOverlappingIterator(iterators, ""), nil
}

// Returns an SampleIterator.
func (s *stream) SampleIterator(ctx context.Context, ingStats *stats.IngesterData, from, through time.Time, extractor log.StreamSampleExtractor) (iter.SampleIterator, error) {
	s.chunkMtx.RLock()
	defer s.chunkMtx.RUnlock()
	iterators := make([]iter.SampleIterator, 0, len(s.chunks))
	for _, c := range s.chunks {
		if itr := c.chunk.SampleIterator(ctx, from, through, extractor); itr != nil {
			iterators = append(iterators, itr)
		}
	}

	if ingStats != nil {
		ingStats.TotalChunksMatched += int64(len(s.chunks))
	}
	return iter.NewNonOverlappingSampleIterator(iterators, ""), nil
}

func (s *stream) addTailer(t *tailer) {
	s.tailerMtx.Lock()
	defer s.tailerMtx.Unlock()

	s.tailers[t.getID()] = t
}

func (s *stream) resetCounter() {
	s.entryCt = 0
}
