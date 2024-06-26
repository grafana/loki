package ingesterrf1

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/grafana/dskit/httpgrpc"
	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/distributor/writefailures"
	"github.com/grafana/loki/v3/pkg/loghttp/push"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util/flagext"
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
	fp model.Fingerprint // possibly remapped fingerprint, used in the streams map

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

	//tailers   map[uint32]*tailer
	//tailerMtx sync.RWMutex

	// entryCt is a counter which is incremented on each accepted entry.
	// This allows us to discard WAL entries during replays which were
	// already recovered via checkpoints. Historically out of order
	// errors were used to detect this, but this counter has been
	// introduced to facilitate removing the ordering constraint.
	entryCt int64

	unorderedWrites bool
	//streamRateCalculator *StreamRateCalculator

	writeFailures *writefailures.Manager

	chunkFormat          byte
	chunkHeadBlockFormat chunkenc.HeadBlockFmt
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
	//streamRateCalculator *StreamRateCalculator,
	metrics *ingesterMetrics,
	writeFailures *writefailures.Manager,
) *stream {
	//hashNoShard, _ := labels.HashWithoutLabels(make([]byte, 0, 1024), ShardLbName)
	return &stream{
		limiter:      NewStreamRateLimiter(limits, tenant, 10*time.Second),
		cfg:          cfg,
		fp:           fp,
		labels:       labels,
		labelsString: labels.String(),
		labelHash:    labels.Hash(),
		//labelHashNoShard:     hashNoShard,
		//tailers:              map[uint32]*tailer{},
		metrics: metrics,
		tenant:  tenant,
		//streamRateCalculator: streamRateCalculator,

		unorderedWrites:      unorderedWrites,
		writeFailures:        writeFailures,
		chunkFormat:          chunkFormat,
		chunkHeadBlockFormat: headBlockFmt,
	}
}

// consumeChunk manually adds a chunk to the stream that was received during
// ingester chunk transfer.
// Must hold chunkMtx
// DEPRECATED: chunk transfers are no longer suggested and remain for compatibility.
func (s *stream) consumeChunk(_ context.Context, _ *logproto.Chunk) error {
	return nil
}

func (s *stream) Push(
	ctx context.Context,
	entries []logproto.Entry,
	// Whether nor not to ingest all at once or not. It is a per-tenant configuration.
	rateLimitWholeStream bool,

	usageTracker push.UsageTracker,
	flushCtx *flushCtx,
) (int, error) {

	toStore, invalid := s.validateEntries(ctx, entries, rateLimitWholeStream, usageTracker)
	if rateLimitWholeStream && hasRateLimitErr(invalid) {
		return 0, errorForFailedEntries(s, invalid, len(entries))
	}

	bytesAdded, _ := s.storeEntries(ctx, toStore, usageTracker, flushCtx)

	return bytesAdded, errorForFailedEntries(s, invalid, len(entries))
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

	return httpgrpc.Errorf(statusCode, buf.String())
}

func hasRateLimitErr(errs []entryWithError) bool {
	if len(errs) == 0 {
		return false
	}

	lastErr := errs[len(errs)-1]
	_, ok := lastErr.e.(*validation.ErrStreamRateLimit)
	return ok
}

func (s *stream) storeEntries(ctx context.Context, entries []logproto.Entry, usageTracker push.UsageTracker, flushCtx *flushCtx) (int, []*logproto.Entry) {
	if sp := opentracing.SpanFromContext(ctx); sp != nil {
		sp.LogKV("event", "stream started to store entries", "labels", s.labelsString)
		defer sp.LogKV("event", "stream finished to store entries")
	}

	var bytesAdded, outOfOrderSamples, outOfOrderBytes int

	storedEntries := make([]*logproto.Entry, 0, len(entries))
	for i := 0; i < len(entries); i++ {
		s.entryCt++
		s.lastLine.ts = entries[i].Timestamp
		s.lastLine.content = entries[i].Line
		if s.highestTs.Before(entries[i].Timestamp) {
			s.highestTs = entries[i].Timestamp
		}

		bytesAdded += len(entries[i].Line)
		storedEntries = append(storedEntries, &entries[i])
	}
	flushCtx.segmentWriter.Append(s.tenant, s.labels.String(), s.labels, storedEntries)
	s.reportMetrics(ctx, outOfOrderSamples, outOfOrderBytes, 0, 0, usageTracker)
	return bytesAdded, storedEntries
}

func (s *stream) validateEntries(ctx context.Context, entries []logproto.Entry, rateLimitWholeStream bool, usageTracker push.UsageTracker) ([]logproto.Entry, []entryWithError) {

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

		lineBytes := len(entries[i].Line)
		totalBytes += lineBytes

		now := time.Now()
		if !rateLimitWholeStream && !s.limiter.AllowN(now, len(entries[i].Line)) {
			failedEntriesWithError = append(failedEntriesWithError, entryWithError{&entries[i], &validation.ErrStreamRateLimit{RateLimit: flagext.ByteSize(limit), Labels: s.labelsString, Bytes: flagext.ByteSize(lineBytes)}})
			s.writeFailures.Log(s.tenant, failedEntriesWithError[len(failedEntriesWithError)-1].e)
			rateLimitedSamples++
			rateLimitedBytes += lineBytes
			continue
		}

		// The validity window for unordered writes is the highest timestamp present minus 1/2 * max-chunk-age.
		cutoff := highestTs.Add(-s.cfg.MaxChunkAge / 2)
		if s.unorderedWrites && !highestTs.IsZero() && cutoff.After(entries[i].Timestamp) {
			failedEntriesWithError = append(failedEntriesWithError, entryWithError{&entries[i], chunkenc.ErrTooFarBehind(entries[i].Timestamp, cutoff)})
			s.writeFailures.Log(s.tenant, fmt.Errorf("%w for stream %s", failedEntriesWithError[len(failedEntriesWithError)-1].e, s.labels))
			outOfOrderSamples++
			outOfOrderBytes += lineBytes
			continue
		}

		validBytes += lineBytes

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
			failedEntriesWithError = append(failedEntriesWithError, entryWithError{&toStore[i], &validation.ErrStreamRateLimit{RateLimit: flagext.ByteSize(limit), Labels: s.labelsString, Bytes: flagext.ByteSize(len(toStore[i].Line))}})
			rateLimitedBytes += len(toStore[i].Line)
		}
	}

	//s.streamRateCalculator.Record(s.tenant, s.labelHash, s.labelHashNoShard, totalBytes)
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

func (s *stream) resetCounter() {
	s.entryCt = 0
}
