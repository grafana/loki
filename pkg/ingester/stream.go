package ingester

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/weaveworks/common/httpgrpc"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/log"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
	"github.com/grafana/loki/pkg/usagestats"
	"github.com/grafana/loki/pkg/util/flagext"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/validation"
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

	chunkCreatedStats = usagestats.NewCounter("ingester_chunk_created")
)

var ErrEntriesExist = errors.New("duplicate push - entries already exist")

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
	limiter *StreamRateLimiter
	cfg     *Config
	tenant  string
	// Newest chunk at chunks[n-1].
	// Not thread-safe; assume accesses to this are locked by caller.
	chunks   []chunkDesc
	fp       model.Fingerprint // possibly remapped fingerprint, used in the streams map
	chunkMtx sync.RWMutex

	labels       labels.Labels
	labelsString string

	// most recently pushed line. This is used to prevent duplicate pushes.
	// It also determines chunk synchronization when unordered writes are disabled.
	lastLine line

	// keeps track of the highest timestamp accepted by the stream.
	// This is used when unordered writes are enabled to cap the validity window
	// of accepted writes and for chunk synchronization.
	highestTs time.Time

	metrics *ingesterMetrics

	tailers   map[uint32]*tailer
	tailerMtx sync.RWMutex

	// entryCt is a counter which is incremented on each accepted entry.
	// This allows us to discard WAL entries during replays which were
	// already recovered via checkpoints. Historically out of order
	// errors were used to detect this, but this counter has been
	// introduced to facilitate removing the ordering constraint.
	entryCt int64

	unorderedWrites bool
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

func newStream(cfg *Config, limits RateLimiterStrategy, tenant string, fp model.Fingerprint, labels labels.Labels, unorderedWrites bool, metrics *ingesterMetrics) *stream {
	return &stream{
		limiter:         NewStreamRateLimiter(limits, tenant, 10*time.Second),
		cfg:             cfg,
		fp:              fp,
		labels:          labels,
		labelsString:    labels.String(),
		tailers:         map[uint32]*tailer{},
		metrics:         metrics,
		tenant:          tenant,
		unorderedWrites: unorderedWrites,
	}
}

// consumeChunk manually adds a chunk to the stream that was received during
// ingester chunk transfer.
// Must hold chunkMtx
// DEPRECATED: chunk transfers are no longer suggested and remain for compatibility.
func (s *stream) consumeChunk(_ context.Context, chunk *logproto.Chunk) error {
	c, err := chunkenc.NewByteChunk(chunk.Data, s.cfg.BlockSize, s.cfg.TargetChunkSize)
	if err != nil {
		return err
	}

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
	return chunkenc.NewMemChunk(s.cfg.parsedEncoding, headBlockType(s.unorderedWrites), s.cfg.BlockSize, s.cfg.TargetChunkSize)
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
	// Lock chunkMtx while pushing.
	// If this is false, chunkMtx must be held outside Push.
	lockChunk bool,
) (int, error) {
	if lockChunk {
		s.chunkMtx.Lock()
		defer s.chunkMtx.Unlock()
	}

	isReplay := counter > 0
	if isReplay && counter <= s.entryCt {
		var byteCt int
		for _, e := range entries {
			byteCt += len(e.Line)
		}

		s.metrics.walReplaySamplesDropped.WithLabelValues(duplicateReason).Add(float64(len(entries)))
		s.metrics.walReplayBytesDropped.WithLabelValues(duplicateReason).Add(float64(byteCt))
		return 0, ErrEntriesExist
	}

	var bytesAdded int
	prevNumChunks := len(s.chunks)
	if prevNumChunks == 0 {
		s.chunks = append(s.chunks, chunkDesc{
			chunk: s.NewChunk(),
		})
		chunksCreatedTotal.Inc()
		chunkCreatedStats.Inc(1)
	}

	var storedEntries []logproto.Entry
	failedEntriesWithError := []entryWithError{}

	var outOfOrderSamples, outOfOrderBytes int
	var rateLimitedSamples, rateLimitedBytes int
	defer func() {
		if outOfOrderSamples > 0 {
			name := validation.OutOfOrder
			if s.unorderedWrites {
				name = validation.TooFarBehind
			}
			validation.DiscardedSamples.WithLabelValues(name, s.tenant).Add(float64(outOfOrderSamples))
			validation.DiscardedBytes.WithLabelValues(name, s.tenant).Add(float64(outOfOrderBytes))
		}
		if rateLimitedSamples > 0 {
			validation.DiscardedSamples.WithLabelValues(validation.StreamRateLimit, s.tenant).Add(float64(rateLimitedSamples))
			validation.DiscardedBytes.WithLabelValues(validation.StreamRateLimit, s.tenant).Add(float64(rateLimitedBytes))
		}
	}()

	// This call uses a mutex under the hood, cache the result since we're checking the limit
	// on each entry in the push (hot path) and we only use this value when logging entries
	// over the rate limit.
	limit := s.limiter.lim.Limit()

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
		if chunk.closed || !chunk.chunk.SpaceFor(&entries[i]) || s.cutChunkForSynchronization(entries[i].Timestamp, s.highestTs, chunk, s.cfg.SyncPeriod, s.cfg.SyncMinUtilization) {
			chunk = s.cutChunk(ctx)
		}
		// Check if this this should be rate limited.
		now := time.Now()
		if !s.limiter.AllowN(now, len(entries[i].Line)) {
			failedEntriesWithError = append(failedEntriesWithError, entryWithError{&entries[i], &validation.ErrStreamRateLimit{RateLimit: flagext.ByteSize(limit), Labels: s.labelsString, Bytes: flagext.ByteSize(len(entries[i].Line))}})
			rateLimitedSamples++
			rateLimitedBytes += len(entries[i].Line)
			continue
		}

		// The validity window for unordered writes is the highest timestamp present minus 1/2 * max-chunk-age.
		if !isReplay && s.unorderedWrites && !s.highestTs.IsZero() && s.highestTs.Add(-s.cfg.MaxChunkAge/2).After(entries[i].Timestamp) {
			failedEntriesWithError = append(failedEntriesWithError, entryWithError{&entries[i], chunkenc.ErrTooFarBehind})
			outOfOrderSamples++
			outOfOrderBytes += len(entries[i].Line)
		} else if err := chunk.chunk.Append(&entries[i]); err != nil {
			failedEntriesWithError = append(failedEntriesWithError, entryWithError{&entries[i], err})
			if chunkenc.IsOutOfOrderErr(err) {
				outOfOrderSamples++
				outOfOrderBytes += len(entries[i].Line)
			}
		} else {
			storedEntries = append(storedEntries, entries[i])
			s.lastLine.ts = entries[i].Timestamp
			s.lastLine.content = entries[i].Line
			if s.highestTs.Before(entries[i].Timestamp) {
				s.highestTs = entries[i].Timestamp
			}
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

	if len(s.chunks) != prevNumChunks {
		memoryChunks.Add(float64(len(s.chunks) - prevNumChunks))
	}

	if len(failedEntriesWithError) > 0 {
		lastEntryWithErr := failedEntriesWithError[len(failedEntriesWithError)-1]
		_, ok := lastEntryWithErr.e.(*validation.ErrStreamRateLimit)
		outOfOrder := chunkenc.IsOutOfOrderErr(lastEntryWithErr.e)
		if !outOfOrder && !ok {
			return bytesAdded, lastEntryWithErr.e
		}
		var statusCode int
		if outOfOrder {
			statusCode = http.StatusBadRequest
		}
		if ok {
			statusCode = http.StatusTooManyRequests
		}
		// Return a http status 4xx request response with all failed entries.
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

		return bytesAdded, httpgrpc.Errorf(statusCode, buf.String())
	}

	return bytesAdded, nil
}

func (s *stream) cutChunk(ctx context.Context) *chunkDesc {
	// If the chunk has no more space call Close to make sure anything in the head block is cut and compressed
	chunk := &s.chunks[len(s.chunks)-1]
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
	chunkCreatedStats.Inc(1)

	s.chunks = append(s.chunks, chunkDesc{
		chunk: s.NewChunk(),
	})
	return &s.chunks[len(s.chunks)-1]
}

// Returns true, if chunk should be cut before adding new entry. This is done to make ingesters
// cut the chunk for this stream at the same moment, so that new chunk will contain exactly the same entries.
func (s *stream) cutChunkForSynchronization(entryTimestamp, latestTs time.Time, c *chunkDesc, synchronizePeriod time.Duration, minUtilization float64) bool {
	// Never sync when it's not enabled, it's the first push, or if a write isn't the latest ts
	// to prevent syncing many unordered writes.
	if synchronizePeriod <= 0 || latestTs.IsZero() || latestTs.After(entryTimestamp) {
		return false
	}

	// we use fingerprint as a jitter here, basically offsetting stream synchronization points to different
	// this breaks if streams are mapped to different fingerprints on different ingesters, which is too bad.
	cts := (uint64(entryTimestamp.UnixNano()) + uint64(s.fp)) % uint64(synchronizePeriod.Nanoseconds())
	pts := (uint64(latestTs.UnixNano()) + uint64(s.fp)) % uint64(synchronizePeriod.Nanoseconds())

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
func (s *stream) Iterator(ctx context.Context, statsCtx *stats.Context, from, through time.Time, direction logproto.Direction, pipeline log.StreamPipeline) (iter.EntryIterator, error) {
	s.chunkMtx.RLock()
	defer s.chunkMtx.RUnlock()
	iterators := make([]iter.EntryIterator, 0, len(s.chunks))

	var lastMax time.Time
	ordered := true

	for _, c := range s.chunks {
		mint, maxt := c.chunk.Bounds()

		// skip this chunk
		if through.Before(mint) || maxt.Before(from) {
			continue
		}

		if mint.Before(lastMax) {
			ordered = false
		}
		lastMax = maxt

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

	if statsCtx != nil {
		statsCtx.AddIngesterTotalChunkMatched(int64(len(iterators)))
	}

	if ordered {
		return iter.NewNonOverlappingIterator(iterators), nil
	}
	return iter.NewSortEntryIterator(iterators, direction), nil
}

// Returns an SampleIterator.
func (s *stream) SampleIterator(ctx context.Context, statsCtx *stats.Context, from, through time.Time, extractor log.StreamSampleExtractor) (iter.SampleIterator, error) {
	s.chunkMtx.RLock()
	defer s.chunkMtx.RUnlock()
	iterators := make([]iter.SampleIterator, 0, len(s.chunks))

	var lastMax time.Time
	ordered := true

	for _, c := range s.chunks {
		mint, maxt := c.chunk.Bounds()

		// skip this chunk
		if through.Before(mint) || maxt.Before(from) {
			continue
		}

		if mint.Before(lastMax) {
			ordered = false
		}
		lastMax = maxt

		if itr := c.chunk.SampleIterator(ctx, from, through, extractor); itr != nil {
			iterators = append(iterators, itr)
		}
	}

	if statsCtx != nil {
		statsCtx.AddIngesterTotalChunkMatched(int64(len(iterators)))
	}

	if ordered {
		return iter.NewNonOverlappingSampleIterator(iterators), nil
	}
	return iter.NewSortSampleIterator(iterators), nil
}

func (s *stream) addTailer(t *tailer) {
	s.tailerMtx.Lock()
	defer s.tailerMtx.Unlock()

	s.tailers[t.getID()] = t
}

func (s *stream) resetCounter() {
	s.entryCt = 0
}

func headBlockType(unorderedWrites bool) chunkenc.HeadBlockFmt {
	if unorderedWrites {
		return chunkenc.UnorderedHeadBlockFmt
	}
	return chunkenc.OrderedHeadBlockFmt
}
