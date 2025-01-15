package ingester

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/opentracing/opentracing-go"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/distributor/writefailures"
	"github.com/grafana/loki/v3/pkg/ingester/wal"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/loghttp/push"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/runtime"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/flagext"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/validation"
)

var ErrEntriesExist = errors.New("duplicate push - entries already exist")

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

	labels           labels.Labels
	labelsString     string
	labelHash        uint64
	labelHashNoShard uint64

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

	unorderedWrites      bool
	streamRateCalculator *StreamRateCalculator

	writeFailures *writefailures.Manager

	chunkFormat          byte
	chunkHeadBlockFormat chunkenc.HeadBlockFmt

	configs *runtime.TenantConfigs
}

type chunkDesc struct {
	chunk   *chunkenc.MemChunk
	closed  bool
	synced  bool
	flushed time.Time
	reason  string

	lastUpdated time.Time
}

type entryWithError struct {
	entry *logproto.Entry
	e     error
}

func newStream(
	chunkFormat byte,
	headBlockFmt chunkenc.HeadBlockFmt,
	cfg *Config,
	limits RateLimiterStrategy,
	tenant string,
	fp model.Fingerprint,
	labels labels.Labels,
	unorderedWrites bool,
	streamRateCalculator *StreamRateCalculator,
	metrics *ingesterMetrics,
	writeFailures *writefailures.Manager,
	configs *runtime.TenantConfigs,
) *stream {
	hashNoShard, _ := labels.HashWithoutLabels(make([]byte, 0, 1024), ShardLbName)
	return &stream{
		limiter:              NewStreamRateLimiter(limits, tenant, 10*time.Second),
		cfg:                  cfg,
		fp:                   fp,
		labels:               labels,
		labelsString:         labels.String(),
		labelHash:            labels.Hash(),
		labelHashNoShard:     hashNoShard,
		tailers:              map[uint32]*tailer{},
		metrics:              metrics,
		tenant:               tenant,
		streamRateCalculator: streamRateCalculator,

		unorderedWrites:      unorderedWrites,
		writeFailures:        writeFailures,
		chunkFormat:          chunkFormat,
		chunkHeadBlockFormat: headBlockFmt,

		configs: configs,
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
	s.metrics.chunksCreatedTotal.Inc()
	return nil
}

// setChunks is used during checkpoint recovery
func (s *stream) setChunks(chunks []Chunk) (bytesAdded, entriesAdded int, err error) {
	s.chunkMtx.Lock()
	defer s.chunkMtx.Unlock()
	chks, err := fromWireChunks(s.cfg, s.chunkHeadBlockFormat, chunks)
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
	return chunkenc.NewMemChunk(s.chunkFormat, s.cfg.parsedEncoding, s.chunkHeadBlockFormat, s.cfg.BlockSize, s.cfg.TargetChunkSize)
}

func (s *stream) Push(
	ctx context.Context,
	entries []logproto.Entry,
	// WAL record to add push contents to.
	// May be nil to disable this functionality.
	record *wal.Record,
	// Counter used in WAL replay to avoid duplicates.
	// If this is non-zero, the stream will reject entries
	// with a counter value less than or equal to it's own.
	// It is set to zero and thus bypassed outside of WAL replays.
	counter int64,
	// Lock chunkMtx while pushing.
	// If this is false, chunkMtx must be held outside Push.
	lockChunk bool,
	// Whether nor not to ingest all at once or not. It is a per-tenant configuration.
	rateLimitWholeStream bool,

	usageTracker push.UsageTracker,
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

	toStore, invalid := s.validateEntries(ctx, entries, isReplay, rateLimitWholeStream, usageTracker)
	if rateLimitWholeStream && hasRateLimitErr(invalid) {
		return 0, errorForFailedEntries(s, invalid, len(entries))
	}

	prevNumChunks := len(s.chunks)
	if prevNumChunks == 0 {
		s.chunks = append(s.chunks, chunkDesc{
			chunk: s.NewChunk(),
		})
		s.metrics.chunksCreatedTotal.Inc()
		s.metrics.chunkCreatedStats.Inc(1)
	}

	bytesAdded, storedEntries, entriesWithErr := s.storeEntries(ctx, toStore, usageTracker)
	s.recordAndSendToTailers(record, storedEntries)

	if len(s.chunks) != prevNumChunks {
		s.metrics.memoryChunks.Add(float64(len(s.chunks) - prevNumChunks))
	}

	return bytesAdded, errorForFailedEntries(s, append(invalid, entriesWithErr...), len(entries))
}

func errorForFailedEntries(s *stream, failedEntriesWithError []entryWithError, totalEntries int) error {
	if len(failedEntriesWithError) == 0 {
		return nil
	}

	lastEntryWithErr := failedEntriesWithError[len(failedEntriesWithError)-1]
	_, ok := lastEntryWithErr.e.(*validation.ErrStreamRateLimit)
	outOfOrder := chunkenc.IsOutOfOrderErr(lastEntryWithErr.e)
	if !outOfOrder && !ok {
		return lastEntryWithErr.e
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
			"entry with timestamp %s ignored, reason: '%s',\n",
			entryWithError.entry.Timestamp.String(), entryWithError.e.Error())
	}

	fmt.Fprintf(&buf, "user '%s', total ignored: %d out of %d for stream: %s", s.tenant, len(failedEntriesWithError), totalEntries, streamName)

	return httpgrpc.Errorf(statusCode, "%s", buf.String())
}

func hasRateLimitErr(errs []entryWithError) bool {
	if len(errs) == 0 {
		return false
	}

	lastErr := errs[len(errs)-1]
	_, ok := lastErr.e.(*validation.ErrStreamRateLimit)
	return ok
}

func (s *stream) recordAndSendToTailers(record *wal.Record, entries []logproto.Entry) {
	if len(entries) == 0 {
		return
	}

	// record will be nil when replaying the wal (we don't want to rewrite wal entries as we replay them).
	if record != nil {
		record.AddEntries(uint64(s.fp), s.entryCt, entries...)
	} else {
		// If record is nil, this is a WAL recovery.
		s.metrics.recoveredEntriesTotal.Add(float64(len(entries)))
	}

	s.tailerMtx.RLock()
	hasTailers := len(s.tailers) != 0
	s.tailerMtx.RUnlock()
	if hasTailers {
		stream := logproto.Stream{Labels: s.labelsString, Entries: entries}

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
	}
}

func (s *stream) storeEntries(ctx context.Context, entries []logproto.Entry, usageTracker push.UsageTracker) (int, []logproto.Entry, []entryWithError) {
	if sp := opentracing.SpanFromContext(ctx); sp != nil {
		sp.LogKV("event", "stream started to store entries", "labels", s.labelsString)
		defer sp.LogKV("event", "stream finished to store entries")
	}

	var bytesAdded, outOfOrderSamples, outOfOrderBytes int

	var invalid []entryWithError
	storedEntries := make([]logproto.Entry, 0, len(entries))
	for i := 0; i < len(entries); i++ {
		chunk := &s.chunks[len(s.chunks)-1]
		if chunk.closed || !chunk.chunk.SpaceFor(&entries[i]) || s.cutChunkForSynchronization(entries[i].Timestamp, s.highestTs, chunk, s.cfg.SyncPeriod, s.cfg.SyncMinUtilization) {
			chunk = s.cutChunk(ctx)
		}

		chunk.lastUpdated = time.Now()
		dup, err := chunk.chunk.Append(&entries[i])
		if err != nil {
			invalid = append(invalid, entryWithError{&entries[i], err})
			if chunkenc.IsOutOfOrderErr(err) {
				s.writeFailures.Log(s.tenant, err)
				outOfOrderSamples++
				outOfOrderBytes += util.EntryTotalSize(&entries[i])
			}
			continue
		}
		if dup {
			s.handleLoggingOfDuplicateEntry(entries[i])
		}

		s.entryCt++
		s.lastLine.ts = entries[i].Timestamp
		s.lastLine.content = entries[i].Line
		if s.highestTs.Before(entries[i].Timestamp) {
			s.highestTs = entries[i].Timestamp
		}

		bytesAdded += len(entries[i].Line)
		storedEntries = append(storedEntries, entries[i])
	}
	s.reportMetrics(ctx, outOfOrderSamples, outOfOrderBytes, 0, 0, usageTracker)
	return bytesAdded, storedEntries, invalid
}

func (s *stream) handleLoggingOfDuplicateEntry(entry logproto.Entry) {
	if s.configs == nil {
		return
	}
	if s.configs.LogDuplicateMetrics(s.tenant) {
		s.metrics.duplicateLogBytesTotal.WithLabelValues(s.tenant).Add(float64(len(entry.Line)))
	}
	if s.configs.LogDuplicateStreamInfo(s.tenant) {
		errMsg := fmt.Sprintf("duplicate log entry with size=%d at timestamp %s for stream %s", len(entry.Line), entry.Timestamp.Format(time.RFC3339), s.labelsString)
		dupErr := errors.New(errMsg)
		s.writeFailures.Log(s.tenant, dupErr)
	}

}

func (s *stream) validateEntries(ctx context.Context, entries []logproto.Entry, isReplay, rateLimitWholeStream bool, usageTracker push.UsageTracker) ([]logproto.Entry, []entryWithError) {

	var (
		outOfOrderSamples, outOfOrderBytes   int
		rateLimitedSamples, rateLimitedBytes int
		validBytes, totalBytes               int
		failedEntriesWithError               []entryWithError
		limit                                = s.limiter.lim.Limit()
		lastLine                             = s.lastLine
		highestTs                            = s.highestTs
		toStore                              = make([]logproto.Entry, 0, len(entries))
	)

	for i := range entries {
		// If this entry matches our last appended line's timestamp and contents,
		// ignore it.
		//
		// This check is done at the stream level so it persists across cut and
		// flushed chunks.
		//
		// NOTE: it's still possible for duplicates to be appended if a stream is
		// deleted from inactivity.
		if entries[i].Timestamp.Equal(lastLine.ts) && entries[i].Line == lastLine.content {
			continue
		}

		entryBytes := util.EntryTotalSize(&entries[i])
		totalBytes += entryBytes

		now := time.Now()
		if !rateLimitWholeStream && !s.limiter.AllowN(now, entryBytes) {
			failedEntriesWithError = append(failedEntriesWithError, entryWithError{&entries[i], &validation.ErrStreamRateLimit{RateLimit: flagext.ByteSize(limit), Labels: s.labelsString, Bytes: flagext.ByteSize(entryBytes)}})
			s.writeFailures.Log(s.tenant, failedEntriesWithError[len(failedEntriesWithError)-1].e)
			rateLimitedSamples++
			rateLimitedBytes += entryBytes
			continue
		}

		// The validity window for unordered writes is the highest timestamp present minus 1/2 * max-chunk-age.
		cutoff := highestTs.Add(-s.cfg.MaxChunkAge / 2)
		if !isReplay && s.unorderedWrites && !highestTs.IsZero() && cutoff.After(entries[i].Timestamp) {
			failedEntriesWithError = append(failedEntriesWithError, entryWithError{&entries[i], chunkenc.ErrTooFarBehind(entries[i].Timestamp, cutoff)})
			s.writeFailures.Log(s.tenant, fmt.Errorf("%w for stream %s", failedEntriesWithError[len(failedEntriesWithError)-1].e, s.labels))
			outOfOrderSamples++
			outOfOrderBytes += entryBytes
			continue
		}

		validBytes += entryBytes

		lastLine.ts = entries[i].Timestamp
		lastLine.content = entries[i].Line
		if highestTs.Before(entries[i].Timestamp) {
			highestTs = entries[i].Timestamp
		}

		toStore = append(toStore, entries[i])
	}

	// Each successful call to 'AllowN' advances the limiter. With all-or-nothing
	// ingestion, the limiter should only be advanced when the whole stream can be
	// sent
	now := time.Now()
	if rateLimitWholeStream && !s.limiter.AllowN(now, validBytes) {
		// Report that the whole stream was rate limited
		rateLimitedSamples = len(toStore)
		failedEntriesWithError = make([]entryWithError, 0, len(toStore))
		for i := 0; i < len(toStore); i++ {
			failedEntriesWithError = append(failedEntriesWithError, entryWithError{
				&toStore[i],
				&validation.ErrStreamRateLimit{
					RateLimit: flagext.ByteSize(limit),
					Labels:    s.labelsString,
					Bytes:     flagext.ByteSize(util.EntryTotalSize(&toStore[i])),
				},
			})
			rateLimitedBytes += util.EntryTotalSize(&toStore[i])
		}

		// Log the only last error to the write failures manager.
		s.writeFailures.Log(s.tenant, failedEntriesWithError[len(failedEntriesWithError)-1].e)
	}

	s.streamRateCalculator.Record(s.tenant, s.labelHash, s.labelHashNoShard, totalBytes)
	s.reportMetrics(ctx, outOfOrderSamples, outOfOrderBytes, rateLimitedSamples, rateLimitedBytes, usageTracker)
	return toStore, failedEntriesWithError
}

func (s *stream) reportMetrics(ctx context.Context, outOfOrderSamples, outOfOrderBytes, rateLimitedSamples, rateLimitedBytes int, usageTracker push.UsageTracker) {
	if outOfOrderSamples > 0 {
		name := validation.OutOfOrder
		if s.unorderedWrites {
			name = validation.TooFarBehind
		}
		validation.DiscardedSamples.WithLabelValues(name, s.tenant).Add(float64(outOfOrderSamples))
		validation.DiscardedBytes.WithLabelValues(name, s.tenant).Add(float64(outOfOrderBytes))
		if usageTracker != nil {
			usageTracker.DiscardedBytesAdd(ctx, s.tenant, name, s.labels, float64(outOfOrderBytes))
		}
	}
	if rateLimitedSamples > 0 {
		validation.DiscardedSamples.WithLabelValues(validation.StreamRateLimit, s.tenant).Add(float64(rateLimitedSamples))
		validation.DiscardedBytes.WithLabelValues(validation.StreamRateLimit, s.tenant).Add(float64(rateLimitedBytes))
		if usageTracker != nil {
			usageTracker.DiscardedBytesAdd(ctx, s.tenant, validation.StreamRateLimit, s.labels, float64(rateLimitedBytes))
		}
	}
}

func (s *stream) cutChunk(ctx context.Context) *chunkDesc {
	if sp := opentracing.SpanFromContext(ctx); sp != nil {
		sp.LogKV("event", "stream started to cut chunk")
		defer sp.LogKV("event", "stream finished to cut chunk")
	}
	// If the chunk has no more space call Close to make sure anything in the head block is cut and compressed
	chunk := &s.chunks[len(s.chunks)-1]
	err := chunk.chunk.Close()
	if err != nil {
		// This should be an unlikely situation, returning an error up the stack doesn't help much here
		// so instead log this to help debug the issue if it ever arises.
		level.Error(util_log.WithContext(ctx, util_log.Logger)).Log("msg", "failed to Close chunk", "err", err)
	}
	chunk.closed = true

	s.metrics.samplesPerChunk.Observe(float64(chunk.chunk.Size()))
	s.metrics.blocksPerChunk.Observe(float64(chunk.chunk.BlockCount()))
	s.metrics.chunksCreatedTotal.Inc()
	s.metrics.chunkCreatedStats.Inc(1)

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

func headBlockType(chunkfmt byte, unorderedWrites bool) chunkenc.HeadBlockFmt {
	if unorderedWrites {
		if chunkfmt >= chunkenc.ChunkFormatV3 {
			return chunkenc.ChunkHeadFormatFor(chunkfmt)
		}
	}
	return chunkenc.OrderedHeadBlockFmt
}
