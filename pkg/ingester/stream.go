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

	pushtypes "github.com/grafana/loki/pkg/push"
)

var ErrEntriesExist = errors.New("duplicate push - entries already exist")

type line struct {
	ts      time.Time
	content string
	// Hash of the structured metadata the line was stored with, i.e. of its effective
	// structured metadata once the shared sets it referenced were materialized into it. It
	// takes part in duplicate detection, see stream.validateEntries.
	effectiveHash uint64
}

// discardReason identifies the bucket an entry that failed validation was accounted to. It is
// only used to attribute the batch's shared structured metadata once when no entry of the
// batch made it through, see stream.validateEntries.
type discardReason int

const (
	discardNone discardReason = iota
	discardRateLimited
	discardTooFarBehind
)

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
	ls labels.Labels,
	streamRateCalculator *StreamRateCalculator,
	metrics *ingesterMetrics,
	writeFailures *writefailures.Manager,
	configs *runtime.TenantConfigs,
	retentionHours string,
	policy string,
) *stream {
	hashNoShard, _ := ls.HashWithoutLabels(make([]byte, 0, 1024), ShardLbName)
	return &stream{
		limiter:              NewStreamRateLimiter(limits, tenant, policy, 10*time.Second),
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
	}
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
	// Pool of structured metadata sets shared by the entries of this push, carried once by
	// the stream instead of being copied into every entry (the OTLP resource and scope
	// attributes when otlp_defer_structured_metadata_expansion is on). Each entry references
	// at most one resource and one scope set of it by index, see push.Stream.SharedFor. May
	// be empty, which is the case for native pushes and for WAL replays.
	//
	// Read-only: the sets are aliased by the caller and by the chunks the entries are
	// appended to.
	sets []logproto.SharedStructuredMetadataSet,
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

	// The whole push is materialized once, here, and every consumer of it downstream works on
	// that one batch. See pushBatch.
	batch := newPushBatch(entries, sets)
	// The pool is stored once per stream, so it is charged once per batch. See validateEntries.
	sharedSize := util.SharedSetsSize(sets)

	toStore, invalid := s.validateEntries(ctx, batch, sharedSize, isReplay, rateLimitWholeStream, usageTracker, format)
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

	bytesAdded, storedEntries, entriesWithErr := s.storeEntries(ctx, toStore, usageTracker, format)
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

// recordAndSendToTailers hands the entries that were just stored to the WAL and to the tail
// clients. entries is the stored form of the push batch, see pushBatch: the entries hold all of
// their structured metadata themselves, so both consumers take them as they are.
//
// Both keep reading them after this returns, the WAL encoder until the record is written and
// the tailer queues until each client drains them, so nothing here may modify them.
func (s *stream) recordAndSendToTailers(record *wal.Record, entries []logproto.Entry) {
	if len(entries) == 0 {
		return
	}

	s.tailerMtx.RLock()
	hasTailers := len(s.tailers) != 0
	s.tailerMtx.RUnlock()

	// record will be nil when replaying the wal (we don't want to rewrite wal entries as we replay them).
	if record != nil {
		// No pool is written alongside the entries: the shared structured metadata of the push
		// is already in them, so the record is the very same record a push that shared nothing
		// produces, in the format every Loki version can read. A replay hands these entries
		// back untouched and rebuilds the chunk byte for byte, duplicate detection telling the
		// same entries apart, because they are the entries that were stored.
		record.AddEntries(uint64(s.fp), s.entryCt, nil, entries...)
	} else {
		// If record is nil, this is a WAL recovery.
		s.metrics.recoveredEntriesTotal.Add(float64(len(entries)))
	}

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

// pushBatch is the entries of one push, in the two forms the ingester needs them in.
//
// The entries of a push may carry their structured metadata split in two: their own, plus a
// reference into the pool of shared structured metadata sets their stream carries, which holds
// the OTLP resource and scope attributes when otlp_defer_structured_metadata_expansion is on.
// That split is a property of the wire format, and it stops here: the batch is materialized
// once into its effective form, and every consumer downstream of this, the chunks, the WAL
// record and the tail clients, is handed entries that hold all of their structured metadata
// themselves, exactly like the entries of a producer that expanded the pool itself. The storage
// path is therefore the unchanged one, whether or not the push used the pool.
type pushBatch struct {
	// stored is the batch as it is ingested: appended to the chunk, written to the WAL record
	// and sent to the tail clients. It is the materialized effective view, or the caller's own
	// entries when there was nothing to materialize.
	//
	// Immutable, and aliased well past the end of the push: the WAL encoder reads it until the
	// record is written, and the tailer queues hold it until every tail client has drained
	// them. Neither the entries nor their structured metadata may be modified, reordered or
	// appended to; the materialized lists may themselves alias the pool's sets, see
	// pushtypes.EffectiveStructuredMetadata.
	stored []logproto.Entry

	// pushed is the batch as the caller handed it over, and is nil when nothing was
	// materialized, i.e. when stored is that batch already. It is only read for size
	// accounting: the shared sets are charged once per batch rather than once per entry, so an
	// entry is charged for its own structured metadata and not for the materialized union.
	pushed []logproto.Entry
}

// newPushBatch materializes the effective view of a push, once, so that resolving the pool is
// per batch work rather than per consumer work.
//
// A push that shares nothing, which is every native push and every WAL replay, is passed
// through without a copy: its entries are stored as they are and the flag-off path stays what
// it always was.
func newPushBatch(entries []logproto.Entry, sets []logproto.SharedStructuredMetadataSet) pushBatch {
	if len(sets) == 0 {
		return pushBatch{stored: entries}
	}

	// A view carrying just the stream's sets, so that reference resolution goes through
	// push.Stream.SharedFor and inherits its bounds-safe handling of a bad reference.
	pool := pushtypes.Stream{SharedStructuredMetadataSets: sets}

	stored := make([]logproto.Entry, len(entries))
	for i := range entries {
		resource, scope := pool.SharedFor(&entries[i])

		stored[i] = entries[i]
		stored[i].StructuredMetadata = pushtypes.EffectiveStructuredMetadata(resource, scope, entries[i].StructuredMetadata)
		// The references only mean anything next to the pool they index, and the pool goes no
		// further than this: what it held is in the entries now. Left in place they would be
		// read against whatever pool the receiver of these entries happens to have.
		stored[i].SharedResourceRef = 0
		stored[i].SharedScopeRef = 0
	}

	// The caller's entries are never modified: they are aliased by the caller itself, and by
	// the replication path, so their structured metadata must not be reordered, appended to or
	// replaced.
	return pushBatch{stored: stored, pushed: entries}
}

// len is the number of entries in the batch.
func (b *pushBatch) len() int {
	return len(b.stored)
}

// entryTotalSize is the size the i-th entry is accounted for: its line and its own structured
// metadata, the shared sets being charged once for the whole batch instead. This is the
// unexpanded, tenant-facing unit, used by the per-stream rate limiter and by the discard metrics.
func (b *pushBatch) entryTotalSize(i int) int {
	if b.pushed != nil {
		return util.EntryTotalSize(&b.pushed[i])
	}
	return util.EntryTotalSize(&b.stored[i])
}

// storedEntryTotalSize is the size of the i-th entry as it is actually stored, that is with the
// shared structured metadata it references materialized into it. It is the expanded-equivalent
// unit: the same number the very same payload would have produced with
// otlp_defer_structured_metadata_expansion off.
//
// For a batch that shares nothing, stored is the pushed batch itself, so this and entryTotalSize
// are the same number by construction.
func (b *pushBatch) storedEntryTotalSize(i int) int {
	return util.EntryTotalSize(&b.stored[i])
}

// effectiveHash identifies the structured metadata the i-th entry is stored with. It is the
// hash of the effective list, so it is a property of what the entry means and not of how its
// metadata happened to be carried on the wire: an entry pushed with a pool and the same entry
// replayed from a WAL record, its metadata already materialized, hash the same.
//
// The hash is computed on the spot rather than cached: the callers only ask for it for the
// handful of entries a push actually compares, not for every entry of the batch.
func (b *pushBatch) effectiveHash(i int) uint64 {
	return util.StructuredMetadataHash(b.stored[i].StructuredMetadata)
}

// filter returns an empty batch of the same shape as this one, with room for as many entries,
// to collect the entries of this batch that pass validation.
func (b *pushBatch) filter() pushBatch {
	filtered := pushBatch{stored: make([]logproto.Entry, 0, b.len())}
	if b.pushed != nil {
		filtered.pushed = make([]logproto.Entry, 0, b.len())
	}
	return filtered
}

// keep adds the i-th entry of src to this batch, in both of its forms.
func (b *pushBatch) keep(src *pushBatch, i int) {
	b.stored = append(b.stored, src.stored[i])
	if b.pushed != nil {
		b.pushed = append(b.pushed, src.pushed[i])
	}
}

func (s *stream) storeEntries(ctx context.Context, batch pushBatch, usageTracker push.UsageTracker, format string) (int, []logproto.Entry, []entryWithError) {
	sp := trace.SpanFromContext(ctx)
	sp.AddEvent("stream started to store entries", trace.WithAttributes(
		attribute.String("labels", s.labelsString)),
	)
	defer sp.AddEvent("stream finished to store entries")

	var bytesAdded, outOfOrderSamples, outOfOrderBytes int

	var invalid []entryWithError
	entries := batch.stored
	storedEntries := make([]logproto.Entry, 0, len(entries))
	// s.lastLine is not read again until the next push, so only the last stored entry's values
	// matter: assigning them once after the loop keeps the hash to one per push instead of one
	// per entry.
	lastStored := -1
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
				outOfOrderBytes += batch.entryTotalSize(i)
			}
			continue
		}
		if dup {
			s.handleLoggingOfDuplicateEntry(entries[i])
		}

		s.entryCt++
		lastStored = i
		if s.highestTs.Before(entries[i].Timestamp) {
			s.highestTs = entries[i].Timestamp
		}

		bytesAdded += len(entries[i].Line)
		storedEntries = append(storedEntries, entries[i])
	}
	if lastStored >= 0 {
		s.lastLine.ts = entries[lastStored].Timestamp
		s.lastLine.content = entries[lastStored].Line
		s.lastLine.effectiveHash = batch.effectiveHash(lastStored)
	}
	s.reportMetrics(ctx, outOfOrderSamples, outOfOrderBytes, 0, 0, usageTracker, format)
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

func (s *stream) validateEntries(ctx context.Context, batch pushBatch, sharedSize int, isReplay, rateLimitWholeStream bool, usageTracker push.UsageTracker, format string) (pushBatch, []entryWithError) {

	var (
		outOfOrderSamples, outOfOrderBytes   int
		rateLimitedSamples, rateLimitedBytes int
		validBytes                           int
		failedEntriesWithError               []entryWithError
		limit                                = s.limiter.lim.Limit()
		lastLine                             = s.lastLine
		highestTs                            = s.highestTs
		entries                              = batch.stored
		toStore                              = batch.filter()
		// The hash of lastLine's structured metadata is only needed when a candidate entry
		// already matches lastLine's timestamp and line, which is the rare duplicate case, so it
		// is computed on demand. While lastLineHashIdx is not -1 it names the entry of this batch
		// lastLine was taken from and lastLine.effectiveHash is stale.
		lastLineHashIdx = -1
	)

	// The shared structured metadata is stored once for the whole stream rather than once per
	// entry, so it is charged once per push batch instead of being added to every entry's
	// size. This mirrors the unexpanded accounting the distributor does (documented in
	// pkg/distributor/validator.go) and keeps the rates the ingester enforces and reports
	// consistent with it.
	//
	// The rule, applied in exactly one place below: the shared bytes are folded into the
	// first entry that is *accepted*, at the same point that entry's own bytes accrue to
	// validBytes, so the rate limiter advances by them exactly once. An entry that is dropped
	// never carries them, they stay pending for the next one. If nothing is accepted they are
	// attributed once to the discard bucket of the first non-duplicate entry, so that a batch is
	// never charged twice nor silently uncharged. A batch whose entries are all duplicates is
	// charged nothing.
	unchargedShared := sharedSize
	// Discard bucket the pending shared bytes fall back to when no entry is accepted.
	firstDiscard := discardNone

	// streamRateBytes is what gets recorded to the stream rate calculator, and is the one number
	// in this function that is EXPANDED-EQUIVALENT rather than unexpanded: every entry is charged
	// for the shared sets it references, i.e. for the entry as it is actually stored, so the
	// number equals what the same payload would have produced with
	// otlp_defer_structured_metadata_expansion off.
	//
	// It differs from the per-stream rate limiter charge above ON PURPOSE - this is not a bug.
	// The rate limiter is tenant-facing and stays unexpanded; the rate calculator is not: it
	// feeds the rate store the distributor reads to decide how many shards a stream gets
	// (Distributor.shardCountFor computes rate + pushSize). The distributor now measures pushSize
	// in expanded-equivalent bytes, so the rate term has to be in the same unit or the shard
	// arithmetic adds two different units together. See the split documented in
	// Distributor.PushWithResolver, which also names the follow-up that flips both back.
	//
	// For a batch that shares nothing this is bit for bit the old number: stored is the pushed
	// batch, and sharedSize is 0, so the per-entry terms agree and there is no pool charge to
	// place. Duplicate entries contribute nothing, exactly as before.
	streamRateBytes := 0

	for i := range entries {
		// If this entry matches our last appended line's timestamp and contents,
		// ignore it.
		//
		// This check is done at the stream level so it persists across cut and
		// flushed chunks.
		//
		// NOTE: it's still possible for duplicates to be appended if a stream is
		// deleted from inactivity.
		//
		// The structured metadata is compared by the hash of its effective form, which is a
		// property of what the entry means rather than of the shape its metadata arrived in.
		// The two have to be told apart because the same entry reaches a stream in both
		// shapes: after an abrupt restart the same data is replayed from the WAL, holding its
		// metadata materialized, and re-consumed from the ingest queue, referencing a pool.
		// Comparing the shapes would make those two look like different entries and store the
		// line twice.
		//
		// Hashing rather than comparing the lists pairwise trades a 2^-64 chance of dropping
		// an entry that is not in fact a duplicate for not having to keep the last line's
		// structured metadata around, which for a pooled push would pin the sets of the whole
		// pool it aliases for as long as the stream lives.
		// The timestamp and the line are compared first, and the structured metadata only if
		// they match: it is the expensive term and it decides nothing on its own.
		if entries[i].Timestamp.Equal(lastLine.ts) && entries[i].Line == lastLine.content {
			if lastLineHashIdx >= 0 {
				lastLine.effectiveHash = batch.effectiveHash(lastLineHashIdx)
				lastLineHashIdx = -1
			}
			if batch.effectiveHash(i) == lastLine.effectiveHash {
				continue
			}
		}

		// The validity window for unordered writes is the highest timestamp present minus 1/2 * max-chunk-age.
		// Evaluated up front, before the rate limit check that reports first, so that the
		// shared charge is only ever attached to an entry that will make it to toStore.
		cutoff := highestTs.Add(-s.cfg.MaxChunkAge / 2)
		tooFarBehind := !isReplay && !highestTs.IsZero() && cutoff.After(entries[i].Timestamp)

		// Only an entry that is going to be accepted absorbs the pending shared bytes. The
		// rate limiter is asked for them together with the entry, so that it advances by them
		// if and only if they are charged.
		sharedCharge := 0
		if !tooFarBehind {
			sharedCharge = unchargedShared
		}
		entryBytes := batch.entryTotalSize(i) + sharedCharge

		// The entry is not a duplicate, so it counts towards the recorded rate whatever the
		// verdict below is, and it counts in the expanded-equivalent unit. See streamRateBytes.
		streamRateBytes += batch.storedEntryTotalSize(i)

		now := time.Now()
		// The limiter is charged the UNEXPANDED size, deliberately not the size recorded to the
		// rate calculator just above: the per-stream rate limit is tenant-facing, so a tenant must
		// not be charged more for the same payload just because its resource and scope attributes
		// travel in a pool. See streamRateBytes for why the two differ.
		if !rateLimitWholeStream && !s.limiter.AllowN(now, entryBytes) {
			// The limiter was asked for the entry plus the pending shared bytes and refused the
			// two together, so that is the size the error reports: reporting the entry alone
			// would understate what was actually rejected.
			//
			// Accounting is a separate question. The entry is dropped, so it does not absorb the
			// shared charge and those bytes stay pending for the next entry, which is why the
			// counters below only take the entry's own bytes.
			rejectedBytes := entryBytes
			entryBytes -= sharedCharge
			failedEntriesWithError = append(failedEntriesWithError, entryWithError{&entries[i], &validation.ErrStreamRateLimit{RateLimit: flagext.ByteSize(limit), Labels: s.labelsString, Bytes: flagext.ByteSize(rejectedBytes)}})
			s.writeFailures.Log(s.tenant, failedEntriesWithError[len(failedEntriesWithError)-1].e)
			rateLimitedSamples++
			rateLimitedBytes += entryBytes
			if firstDiscard == discardNone {
				firstDiscard = discardRateLimited
			}
			continue
		}

		if tooFarBehind {
			// sharedCharge is 0 here, the shared bytes stay pending.
			failedEntriesWithError = append(failedEntriesWithError, entryWithError{&entries[i], chunkenc.ErrTooFarBehind(entries[i].Timestamp, cutoff)})
			s.writeFailures.Log(s.tenant, fmt.Errorf("%w for stream %s", failedEntriesWithError[len(failedEntriesWithError)-1].e, s.labels))
			outOfOrderSamples++
			outOfOrderBytes += entryBytes
			if firstDiscard == discardNone {
				firstDiscard = discardTooFarBehind
			}
			continue
		}

		// Accepted: this is the single point at which the batch's shared structured metadata
		// is charged.
		unchargedShared -= sharedCharge
		validBytes += entryBytes

		lastLine.ts = entries[i].Timestamp
		lastLine.content = entries[i].Line
		lastLineHashIdx = i
		if highestTs.Before(entries[i].Timestamp) {
			highestTs = entries[i].Timestamp
		}

		toStore.keep(&batch, i)
	}

	// Bytes of shared structured metadata that were actually charged to an accepted entry,
	// and are therefore part of validBytes.
	chargedShared := sharedSize - unchargedShared

	// Each successful call to 'AllowN' advances the limiter. With all-or-nothing
	// ingestion, the limiter should only be advanced when the whole stream can be
	// sent
	//
	// validBytes is UNEXPANDED, like the per-entry limiter charge above and unlike
	// streamRateBytes: the limiter is tenant-facing and the pool is one charge per batch here. The
	// divergence from what is recorded to the rate calculator is intentional, see streamRateBytes.
	now := time.Now()
	if rateLimitWholeStream && !s.limiter.AllowN(now, validBytes) {
		// Report that the whole stream was rate limited
		rateLimitedSamples = toStore.len()
		failedEntriesWithError = make([]entryWithError, 0, toStore.len())
		for i := 0; i < toStore.len(); i++ {
			failedEntriesWithError = append(failedEntriesWithError, entryWithError{
				&toStore.stored[i],
				&validation.ErrStreamRateLimit{
					RateLimit: flagext.ByteSize(limit),
					Labels:    s.labelsString,
					Bytes:     flagext.ByteSize(toStore.entryTotalSize(i)),
				},
			})
			rateLimitedBytes += toStore.entryTotalSize(i)
		}
		// The whole batch is discarded, so the shared structured metadata goes with it, but
		// only if it was charged in the first place: entries dropped inside the loop above
		// never absorbed it, and the fallback below has not run yet.
		rateLimitedBytes += chargedShared

		// Log the only last error to the write failures manager.
		if len(failedEntriesWithError) > 0 {
			s.writeFailures.Log(s.tenant, failedEntriesWithError[len(failedEntriesWithError)-1].e)
		}
	}

	// Nothing was accepted, so nothing absorbed the shared structured metadata. Attribute it
	// once to the discard reason of the first non-duplicate entry so the batch is accounted
	// for exactly once overall. Nothing to do for a batch made only of duplicates.
	//
	// Only the discard buckets need this fallback. They are tenant-facing, so they are in the
	// unexpanded unit where the pool is a single per-batch charge that has to land somewhere.
	// streamRateBytes is not: it already charged every non-duplicate entry for the sets it
	// references, whatever the verdict was, so there is nothing left over to place.
	if unchargedShared > 0 {
		switch firstDiscard {
		case discardRateLimited:
			rateLimitedBytes += unchargedShared
			unchargedShared = 0
		case discardTooFarBehind:
			outOfOrderBytes += unchargedShared
			unchargedShared = 0
		}
	}

	s.streamRateCalculator.Record(s.tenant, s.labelHash, s.labelHashNoShard, streamRateBytes)
	s.reportMetrics(ctx, outOfOrderSamples, outOfOrderBytes, rateLimitedSamples, rateLimitedBytes, usageTracker, format)
	return toStore, failedEntriesWithError
}

func (s *stream) reportMetrics(ctx context.Context, outOfOrderSamples, outOfOrderBytes, rateLimitedSamples, rateLimitedBytes int, usageTracker push.UsageTracker, format string) {
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
}

func (s *stream) cutChunk(ctx context.Context) *chunkDesc {
	sp := trace.SpanFromContext(ctx)
	sp.AddEvent("stream started to cut chunk")
	defer sp.AddEvent("stream finished to cut chunk")

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
func (s *stream) SampleIterator(ctx context.Context, statsCtx *stats.Context, from, through time.Time, extractors ...log.StreamSampleExtractor) (iter.SampleIterator, error) {
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

		if itr := c.chunk.SampleIterator(ctx, from, through, extractors...); itr != nil {
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
