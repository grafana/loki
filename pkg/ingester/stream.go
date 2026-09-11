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
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/distributor/writefailures"
	"github.com/grafana/loki/v3/pkg/ingester/shardstreams"
	"github.com/grafana/loki/v3/pkg/ingester/wal"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/loghttp/push"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/runtime"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/grafana/loki/v3/pkg/util/flagext"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/validation"

	pushtypes "github.com/grafana/loki/pkg/push"
)

var ErrEntriesExist = errors.New("duplicate push - entries already exist")

type line struct {
	ts                 time.Time
	content            string
	structuredMetadata pushtypes.LabelsAdapter
}

type stream struct {
	limiter *StreamRateLimiter
	cfg     *Config
	tenant  string
	// chunks are not necessarily ordered: entries are normally appended to
	// chunks[n-1], but when ingester-side time-sharding (see shardstreams.Config)
	// is active, older entries may be routed into any of several other
	// concurrently open chunks tracked by openHeads.
	// Not thread-safe; assume accesses to this are locked by caller.
	chunks   []chunkDesc
	fp       model.Fingerprint // possibly remapped fingerprint, used in the streams map
	chunkMtx sync.RWMutex

	// limits is used to resolve the tenant's ingester-side time-sharding
	// config on every push, so runtime overrides changes take effect without
	// recreating the stream.
	limits Limits
	// skipTimeSharding is true for client-managed backfill streams
	// (constants.BackfillLabel present), which already self-shard by time, so
	// the ingester's own time-bucketing is skipped to avoid double-bucketing.
	skipTimeSharding bool
	// openHeads maps a time-bucket's start (unix seconds) to the index in
	// chunks of that bucket's current appendable head chunk. Only populated
	// while ingester-side time-sharding is enabled and active for this stream.
	openHeads map[int64]int
	// bucketHighestTs tracks, per open time-bucket (keyed the same as
	// openHeads), the highest entry timestamp accepted so far. This is the
	// per-bucket analogue of highestTs below, used to bound out-of-order
	// tolerance relative to each bucket rather than to the stream as a whole.
	bucketHighestTs map[int64]time.Time

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

	streamRateCalculator *StreamRateCalculator

	writeFailures *writefailures.Manager

	chunkFormat          byte
	chunkHeadBlockFormat chunkenc.HeadBlockFmt

	configs *runtime.TenantConfigs

	retentionHours string
	policy         string
}

type chunkDesc struct {
	chunk   *chunkenc.MemChunk
	closed  bool
	synced  bool
	flushed time.Time
	reason  string

	lastUpdated time.Time

	// bucketStart is non-zero when this chunk was created as a time-sharded
	// bucket for out-of-order/backfilled entries (see shardstreams.Config).
	// Zero for chunks created via the normal single-head append path.
	bucketStart time.Time
}

type entryWithError struct {
	entry *logproto.Entry
	e     error
}

func newStream(
	chunkFormat byte,
	headBlockFmt chunkenc.HeadBlockFmt,
	cfg *Config,
	rateLimitStrategy RateLimiterStrategy,
	tenant string,
	fp model.Fingerprint,
	ls labels.Labels,
	streamRateCalculator *StreamRateCalculator,
	metrics *ingesterMetrics,
	writeFailures *writefailures.Manager,
	configs *runtime.TenantConfigs,
	retentionHours string,
	policy string,
	limits Limits,
) *stream {
	hashNoShard, _ := ls.HashWithoutLabels(make([]byte, 0, 1024), ShardLbName)
	return &stream{
		limiter:              NewStreamRateLimiter(rateLimitStrategy, tenant, policy, 10*time.Second),
		cfg:                  cfg,
		fp:                   fp,
		labels:               ls,
		labelsString:         ls.String(),
		labelHash:            labels.StableHash(ls),
		labelHashNoShard:     hashNoShard,
		tailers:              map[uint32]*tailer{},
		metrics:              metrics,
		tenant:               tenant,
		streamRateCalculator: streamRateCalculator,

		writeFailures:        writeFailures,
		chunkFormat:          chunkFormat,
		chunkHeadBlockFormat: headBlockFmt,

		configs:        configs,
		retentionHours: retentionHours,
		policy:         policy,

		limits:           limits,
		skipTimeSharding: ls.Has(constants.BackfillLabel),
	}
}

// setChunks is used during checkpoint recovery.
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
	s.rebuildOpenHeads()
	return bytesAdded, entriesAdded, nil
}

func (s *stream) NewChunk() *chunkenc.MemChunk {
	return chunkenc.NewMemChunk(s.chunkFormat, s.cfg.parsedEncoding, s.chunkHeadBlockFormat, s.cfg.BlockSize, s.cfg.TargetChunkSize)
}

// rebuildOpenHeads recomputes openHeads by scanning chunks for still-open
// (unclosed) time-shard bucket chunks, also seeding bucketHighestTs for each
// from the chunk's own bounds. Used both after chunks has been compacted
// (e.g. flushed chunks removed), which invalidates the indices openHeads
// previously pointed to, and during checkpoint recovery, where openHeads and
// bucketHighestTs otherwise start out empty despite chunks carrying restored
// bucket heads. Callers must hold chunkMtx.
func (s *stream) rebuildOpenHeads() {
	prevOpen := len(s.openHeads)
	s.openHeads = nil
	for idx := range s.chunks {
		c := &s.chunks[idx]
		if !c.closed && !c.bucketStart.IsZero() {
			if s.openHeads == nil {
				s.openHeads = map[int64]int{}
			}
			key := c.bucketStart.Unix()
			s.openHeads[key] = idx

			if _, maxTs := c.chunk.Bounds(); s.bucketHighestTs[key].Before(maxTs) {
				if s.bucketHighestTs == nil {
					s.bucketHighestTs = map[int64]time.Time{}
				}
				s.bucketHighestTs[key] = maxTs
			}
		}
	}
	if delta := len(s.openHeads) - prevOpen; delta != 0 {
		s.metrics.streamTimeShardOpenBuckets.Add(float64(delta))
	}
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
	// format of the request - loki or otlp, mainly used for metrics
	format string,

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

	// Resolved once per push (rather than separately in validateEntries and
	// storeEntries) so both agree on exactly the same ignore-recent boundary
	// for every entry in this batch.
	now := time.Now()
	tsCfg := s.ingesterTimeShardingConfig()

	toStore, invalid := s.validateEntries(ctx, entries, isReplay, rateLimitWholeStream, usageTracker, format, now, tsCfg)
	if rateLimitWholeStream && hasRateLimitErr(invalid) {
		return 0, errorForFailedEntries(s, invalid, len(entries))
	}

	prevNumChunks := len(s.chunks)
	if prevNumChunks == 0 {
		s.appendNewChunk(time.Time{})
	}

	bytesAdded, storedEntries, entriesWithErr := s.storeEntries(ctx, toStore, usageTracker, format, now, tsCfg)
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

// ingesterTimeShardingConfig resolves this stream's tenant's ingester-side
// time-sharding config on every call, so runtime overrides changes take
// effect without recreating the stream. Returns the zero value (disabled) if
// s.limits is nil, which should only happen in tests that don't exercise this
// feature.
func (s *stream) ingesterTimeShardingConfig() shardstreams.Config {
	if s.limits == nil {
		return shardstreams.Config{}
	}
	return s.limits.IngesterTimeSharding(s.tenant)
}

// bucketStartFor returns the start of the time-bucket of the given width that
// ts falls into.
func bucketStartFor(ts time.Time, width time.Duration) time.Time {
	return ts.Truncate(width)
}

func (s *stream) storeEntries(ctx context.Context, entries []logproto.Entry, usageTracker push.UsageTracker, format string, now time.Time, tsCfg shardstreams.Config) (int, []logproto.Entry, []entryWithError) {
	sp := trace.SpanFromContext(ctx)
	sp.AddEvent("stream started to store entries", trace.WithAttributes(
		attribute.String("labels", s.labelsString)),
	)
	defer sp.AddEvent("stream finished to store entries")

	var bytesAdded, outOfOrderSamples, outOfOrderBytes int

	timeShardingActive := tsCfg.Enabled && !s.skipTimeSharding
	bucketWidth := s.cfg.MaxChunkAge / 2
	var ignoreRecentFrom time.Time
	if timeShardingActive {
		ignoreRecentFrom = now.Add(-tsCfg.IgnoreRecent)
	}

	var invalid []entryWithError
	storedEntries := make([]logproto.Entry, 0, len(entries))
	for i := 0; i < len(entries); i++ {
		var (
			chunk       *chunkDesc
			bucketStart time.Time
		)
		if timeShardingActive && entries[i].Timestamp.Before(ignoreRecentFrom) {
			bucketStart = bucketStartFor(entries[i].Timestamp, bucketWidth)
			chunk = s.headForBucket(ctx, bucketStart, &entries[i])
		} else {
			chunk = &s.chunks[len(s.chunks)-1]
			if chunk.closed || !chunk.chunk.SpaceFor(&entries[i]) || s.cutChunkForSynchronization(entries[i].Timestamp, s.highestTs, chunk, s.cfg.SyncPeriod, s.cfg.SyncMinUtilization) {
				chunk = s.cutChunk(ctx)
			}
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
		s.lastLine.structuredMetadata = entries[i].StructuredMetadata
		if !bucketStart.IsZero() {
			if s.bucketHighestTs == nil {
				s.bucketHighestTs = map[int64]time.Time{}
			}
			key := bucketStart.Unix()
			if s.bucketHighestTs[key].Before(entries[i].Timestamp) {
				s.bucketHighestTs[key] = entries[i].Timestamp
			}
			s.metrics.timeShardedSamplesTotal.WithLabelValues(s.tenant).Inc()
			s.metrics.timeShardedBytesTotal.WithLabelValues(s.tenant).Add(float64(len(entries[i].Line)))
		} else if s.highestTs.Before(entries[i].Timestamp) {
			s.highestTs = entries[i].Timestamp
		}

		bytesAdded += len(entries[i].Line)
		storedEntries = append(storedEntries, entries[i])
	}
	s.reportMetrics(ctx, outOfOrderSamples, outOfOrderBytes, 0, 0, 0, 0, usageTracker, format)
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

func (s *stream) validateEntries(ctx context.Context, entries []logproto.Entry, isReplay, rateLimitWholeStream bool, usageTracker push.UsageTracker, format string, now time.Time, tsCfg shardstreams.Config) ([]logproto.Entry, []entryWithError) {

	var (
		outOfOrderSamples, outOfOrderBytes         int
		rateLimitedSamples, rateLimitedBytes       int
		tooManyBucketsSamples, tooManyBucketsBytes int
		validBytes, totalBytes                     int
		failedEntriesWithError                     []entryWithError
		limit                                      = s.limiter.lim.Limit()
		lastLine                                   = s.lastLine
		highestTs                                  = s.highestTs
		toStore                                    = make([]logproto.Entry, 0, len(entries))
	)

	timeShardingActive := tsCfg.Enabled && !s.skipTimeSharding
	bucketWidth := s.cfg.MaxChunkAge / 2
	var ignoreRecentFrom time.Time
	var openBuckets map[int64]struct{}
	bucketHighest := map[int64]time.Time{}
	if timeShardingActive {
		ignoreRecentFrom = now.Add(-tsCfg.IgnoreRecent)
		openBuckets = make(map[int64]struct{}, len(s.openHeads))
		for k := range s.openHeads {
			openBuckets[k] = struct{}{}
		}
	}

	for i := range entries {
		// If this entry matches our last appended line's timestamp and contents,
		// ignore it.
		//
		// This check is done at the stream level so it persists across cut and
		// flushed chunks.
		//
		// NOTE: it's still possible for duplicates to be appended if a stream is
		// deleted from inactivity.
		if entries[i].Timestamp.Equal(lastLine.ts) &&
			entries[i].Line == lastLine.content &&
			labelsEqual(entries[i].StructuredMetadata, lastLine.structuredMetadata) {
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

		if timeShardingActive && entries[i].Timestamp.Before(ignoreRecentFrom) {
			// This entry is old enough to be routed to a time-bucketed chunk
			// rather than the stream's live head. Validate it against that
			// bucket's own high-water mark instead of the stream-wide one, so
			// backfilling old data doesn't get rejected just because the
			// stream has since received much more recent entries.
			bucketStart := bucketStartFor(entries[i].Timestamp, bucketWidth)
			key := bucketStart.Unix()

			if _, seen := openBuckets[key]; !seen {
				if len(openBuckets) >= tsCfg.MaxOpenBuckets {
					failedEntriesWithError = append(failedEntriesWithError, entryWithError{&entries[i], chunkenc.ErrTooManyTimeShardBuckets(entries[i].Timestamp, tsCfg.MaxOpenBuckets)})
					s.writeFailures.Log(s.tenant, fmt.Errorf("%w for stream %s", failedEntriesWithError[len(failedEntriesWithError)-1].e, s.labels))
					tooManyBucketsSamples++
					tooManyBucketsBytes += entryBytes
					continue
				}
				openBuckets[key] = struct{}{}
			}

			bHighest, ok := bucketHighest[key]
			if !ok {
				bHighest = s.bucketHighestTs[key]
			}
			cutoff := bHighest.Add(-bucketWidth)
			if !isReplay && !bHighest.IsZero() && cutoff.After(entries[i].Timestamp) {
				failedEntriesWithError = append(failedEntriesWithError, entryWithError{&entries[i], chunkenc.ErrTooFarBehind(entries[i].Timestamp, cutoff)})
				s.writeFailures.Log(s.tenant, fmt.Errorf("%w for stream %s", failedEntriesWithError[len(failedEntriesWithError)-1].e, s.labels))
				outOfOrderSamples++
				outOfOrderBytes += entryBytes
				continue
			}

			if bHighest.Before(entries[i].Timestamp) {
				bucketHighest[key] = entries[i].Timestamp
			}
		} else {
			// The validity window for unordered writes is the highest timestamp present minus 1/2 * max-chunk-age.
			cutoff := highestTs.Add(-s.cfg.MaxChunkAge / 2)
			if !isReplay && !highestTs.IsZero() && cutoff.After(entries[i].Timestamp) {
				failedEntriesWithError = append(failedEntriesWithError, entryWithError{&entries[i], chunkenc.ErrTooFarBehind(entries[i].Timestamp, cutoff)})
				s.writeFailures.Log(s.tenant, fmt.Errorf("%w for stream %s", failedEntriesWithError[len(failedEntriesWithError)-1].e, s.labels))
				outOfOrderSamples++
				outOfOrderBytes += entryBytes
				continue
			}

			if highestTs.Before(entries[i].Timestamp) {
				highestTs = entries[i].Timestamp
			}
		}

		validBytes += entryBytes

		lastLine.ts = entries[i].Timestamp
		lastLine.content = entries[i].Line
		lastLine.structuredMetadata = entries[i].StructuredMetadata

		toStore = append(toStore, entries[i])
	}

	// Each successful call to 'AllowN' advances the limiter. With all-or-nothing
	// ingestion, the limiter should only be advanced when the whole stream can be
	// sent
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
	s.reportMetrics(ctx, outOfOrderSamples, outOfOrderBytes, rateLimitedSamples, rateLimitedBytes, tooManyBucketsSamples, tooManyBucketsBytes, usageTracker, format)
	return toStore, failedEntriesWithError
}

func (s *stream) reportMetrics(ctx context.Context, outOfOrderSamples, outOfOrderBytes, rateLimitedSamples, rateLimitedBytes, tooManyBucketsSamples, tooManyBucketsBytes int, usageTracker push.UsageTracker, format string) {
	if outOfOrderSamples > 0 {
		name := validation.TooFarBehind
		validation.DiscardedSamples.WithLabelValues(name, s.tenant, s.retentionHours, s.policy, format).Add(float64(outOfOrderSamples))
		validation.DiscardedBytes.WithLabelValues(name, s.tenant, s.retentionHours, s.policy, format).Add(float64(outOfOrderBytes))
		if usageTracker != nil {
			usageTracker.DiscardedBytesAdd(ctx, s.tenant, name, s.labels, float64(outOfOrderBytes), format)
		}
	}
	if rateLimitedSamples > 0 {
		validation.DiscardedSamples.WithLabelValues(validation.StreamRateLimit, s.tenant, s.retentionHours, s.policy, format).Add(float64(rateLimitedSamples))
		validation.DiscardedBytes.WithLabelValues(validation.StreamRateLimit, s.tenant, s.retentionHours, s.policy, format).Add(float64(rateLimitedBytes))
		if usageTracker != nil {
			usageTracker.DiscardedBytesAdd(ctx, s.tenant, validation.StreamRateLimit, s.labels, float64(rateLimitedBytes), format)
		}
	}
	if tooManyBucketsSamples > 0 {
		name := validation.TooManyTimeShardBuckets
		validation.DiscardedSamples.WithLabelValues(name, s.tenant, s.retentionHours, s.policy, format).Add(float64(tooManyBucketsSamples))
		validation.DiscardedBytes.WithLabelValues(name, s.tenant, s.retentionHours, s.policy, format).Add(float64(tooManyBucketsBytes))
		if usageTracker != nil {
			usageTracker.DiscardedBytesAdd(ctx, s.tenant, name, s.labels, float64(tooManyBucketsBytes), format)
		}
	}
}

// closeChunk closes the given chunk (making sure anything in the head block
// is cut and compressed) and records its final per-chunk stats. It does not
// create a replacement chunk; callers do that via appendNewChunk.
func (s *stream) closeChunk(ctx context.Context, chunk *chunkDesc) {
	sp := trace.SpanFromContext(ctx)
	sp.AddEvent("stream started to cut chunk")
	defer sp.AddEvent("stream finished to cut chunk")

	err := chunk.chunk.Close()
	if err != nil {
		// This should be an unlikely situation, returning an error up the stack doesn't help much here
		// so instead log this to help debug the issue if it ever arises.
		level.Error(util_log.WithContext(ctx, util_log.Logger)).Log("msg", "failed to Close chunk", "err", err)
	}
	chunk.closed = true

	s.metrics.samplesPerChunk.Observe(float64(chunk.chunk.Size()))
	s.metrics.blocksPerChunk.Observe(float64(chunk.chunk.BlockCount()))
}

// appendNewChunk appends a new, empty appendable chunk to s.chunks and
// returns it. bucketStart is zero for the normal single-head path; when
// non-zero, the new chunk is tracked in openHeads as that bucket's current
// head.
func (s *stream) appendNewChunk(bucketStart time.Time) *chunkDesc {
	s.chunks = append(s.chunks, chunkDesc{
		chunk:       s.NewChunk(),
		bucketStart: bucketStart,
	})
	idx := len(s.chunks) - 1

	if !bucketStart.IsZero() {
		if s.openHeads == nil {
			s.openHeads = map[int64]int{}
		}
		key := bucketStart.Unix()
		if _, existed := s.openHeads[key]; !existed {
			s.metrics.streamTimeShardOpenBuckets.Inc()
		}
		s.openHeads[key] = idx
	}

	s.metrics.chunksCreatedTotal.Inc()
	s.metrics.chunkCreatedStats.Inc(1)

	return &s.chunks[idx]
}

func (s *stream) cutChunk(ctx context.Context) *chunkDesc {
	// If the chunk has no more space call Close to make sure anything in the head block is cut and compressed
	s.closeChunk(ctx, &s.chunks[len(s.chunks)-1])
	return s.appendNewChunk(time.Time{})
}

// headForBucket returns the current appendable head chunk for the given
// time-bucket, creating one (or replacing a full/closed one) as needed. Used
// only when ingester-side time-sharding is active for this stream.
func (s *stream) headForBucket(ctx context.Context, bucketStart time.Time, entry *logproto.Entry) *chunkDesc {
	key := bucketStart.Unix()
	if idx, ok := s.openHeads[key]; ok {
		c := &s.chunks[idx]
		if !c.closed && c.chunk.SpaceFor(entry) {
			return c
		}
		if !c.closed {
			s.closeChunk(ctx, c)
		}
	}
	return s.appendNewChunk(bucketStart)
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

func labelsEqual(a, b pushtypes.LabelsAdapter) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i].Name != b[i].Name || a[i].Value != b[i].Value {
			return false
		}
	}

	return true
}
