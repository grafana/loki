package index

import (
	"context"
	"encoding/json"
	"time"

	"github.com/grafana/dskit/instrument"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/stats"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/sharding"
	loki_instrument "github.com/grafana/loki/v3/pkg/util/instrument"
)

type Filterable interface {
	// SetChunkFilterer sets a chunk filter to be used when retrieving chunks.
	SetChunkFilterer(chunkFilter chunk.RequestChunkFilterer)
}

type BaseReader interface {
	GetSeries(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]labels.Labels, error)
	LabelValuesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string, labelName string, matchers ...*labels.Matcher) ([]string, error)
	LabelNamesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string, matchers ...*labels.Matcher) ([]string, error)
}

type StatsReader interface {
	Stats(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) (*stats.Stats, error)
	Volume(ctx context.Context, userID string, from, through model.Time, limit int32, targetLabels []string, aggregateBy string, matchers ...*labels.Matcher) (*logproto.VolumeResponse, error)
	GetShards(
		ctx context.Context,
		userID string,
		from, through model.Time,
		targetBytesPerShard uint64,
		predicate chunk.Predicate,
	) (*logproto.ShardsResponse, error)

	// If the underlying index supports it, this will return the ForSeries interface
	// which is used in bloom-filter accelerated sharding calculation optimization.
	HasForSeries(from, through model.Time) (sharding.ForSeries, bool)

	// HasChunkSizingInfo tells whether the index type for the given period supports listing chunks with their sizing info
	HasChunkSizingInfo(from, through model.Time) bool
	// GetChunkRefsWithSizingInfo should only be called after if HasChunkSizingInfo acknowledges that underlying index supports listing chunks with sizing info
	GetChunkRefsWithSizingInfo(ctx context.Context, userID string, from, through model.Time, predicate chunk.Predicate) ([]logproto.ChunkRefWithSizingInfo, error)
}

type Reader interface {
	BaseReader
	StatsReader
	Filterable
	GetChunkRefs(ctx context.Context, userID string, from, through model.Time, predicate chunk.Predicate) ([]logproto.ChunkRef, error)
}

type Writer interface {
	IndexChunk(ctx context.Context, from, through model.Time, chk chunk.Chunk) error
}

type ReaderWriter interface {
	Reader
	Writer
}

// Flusher is implemented by index stores that can force their in-memory indexes
// (e.g. the TSDB head) to be built and shipped to object storage on demand,
// rather than waiting for the periodic rotation/upload.
type Flusher interface {
	// FlushIndexes forces any in-memory index data to be persisted to object storage.
	FlushIndexes(ctx context.Context) error
}

// SyncTriggerNever is the LastTrigger value the HTTP API reports for an index
// that has not synced yet (neither a periodic nor a manual sync has run). It is
// emitted in place of an empty trigger so the "never synced" state is explicit
// rather than inferred from a zero LastDuration.
const SyncTriggerNever = "never_triggered"

// SyncStatus reports the state of an index store's background sync.
type SyncStatus struct {
	// Name identifies the index this status belongs to (e.g. the schema period's
	// store name). Set by the composite store that aggregates per-index statuses.
	Name string
	// InProgress is true while a sync (manual or periodic) is running.
	InProgress bool
	// CurrentDuration is how long the in-progress sync has been running. Only
	// meaningful when InProgress is true.
	CurrentDuration time.Duration
	// LastDuration is how long the previous completed sync took. Zero if no sync
	// has completed yet.
	LastDuration time.Duration
	// LastTrigger is what triggered the current or most recent sync ("periodic"
	// or "manual"), or empty if no sync has run yet. The HTTP API renders an
	// empty value as SyncTriggerNever ("never_triggered").
	LastTrigger string
}

// MarshalJSON renders the status for the HTTP API: durations as Go duration
// strings (e.g. "1m30s"), with CurrentDuration included only while a sync is in
// progress. time.Duration would otherwise encode as integer nanoseconds, since
// encoding/json never calls its String method. An empty LastTrigger (no sync has
// run yet) is rendered as SyncTriggerNever so the state is explicit.
func (s SyncStatus) MarshalJSON() ([]byte, error) {
	out := struct {
		Name            string  `json:"name"`
		InProgress      bool    `json:"in_progress"`
		LastTrigger     string  `json:"last_trigger"`
		CurrentDuration *string `json:"current_duration,omitempty"`
		LastDuration    string  `json:"last_duration"`
	}{
		Name:         s.Name,
		InProgress:   s.InProgress,
		LastTrigger:  s.LastTrigger,
		LastDuration: s.LastDuration.String(),
	}
	if out.LastTrigger == "" {
		out.LastTrigger = SyncTriggerNever
	}
	if s.InProgress {
		d := s.CurrentDuration.String()
		out.CurrentDuration = &d
	}
	return json.Marshal(out)
}

// Syncer is implemented by index stores that can, on demand, refresh their
// object-listing cache and download newly shipped index files asynchronously,
// rather than waiting for the periodic resync.
type Syncer interface {
	// TriggerSync starts a background sync (refreshing the list cache first) if
	// none is already in progress. It returns true if a new sync was started.
	TriggerSync() bool
	// SyncStatus reports the current/last sync status.
	SyncStatus() SyncStatus
}

type MonitoredReaderWriter struct {
	rw      ReaderWriter
	metrics *metrics
}

func NewMonitoredReaderWriter(rw ReaderWriter, reg prometheus.Registerer) *MonitoredReaderWriter {
	return &MonitoredReaderWriter{
		rw:      rw,
		metrics: newMetrics(reg),
	}
}

func (m MonitoredReaderWriter) GetChunkRefs(ctx context.Context, userID string, from, through model.Time, predicate chunk.Predicate) ([]logproto.ChunkRef, error) {
	var chunks []logproto.ChunkRef

	if err := loki_instrument.TimeRequest(ctx, "chunk_refs", instrument.NewHistogramCollector(m.metrics.indexQueryLatency), instrument.ErrorCode, func(ctx context.Context) error {
		var err error
		chunks, err = m.rw.GetChunkRefs(ctx, userID, from, through, predicate)
		return err
	}); err != nil {
		return nil, err
	}

	return chunks, nil
}

func (m MonitoredReaderWriter) GetChunkRefsWithSizingInfo(ctx context.Context, userID string, from, through model.Time, predicate chunk.Predicate) ([]logproto.ChunkRefWithSizingInfo, error) {
	var chunks []logproto.ChunkRefWithSizingInfo

	if err := loki_instrument.TimeRequest(ctx, "chunk_refs_with_sizing_info", instrument.NewHistogramCollector(m.metrics.indexQueryLatency), instrument.ErrorCode, func(ctx context.Context) error {
		var err error
		chunks, err = m.rw.GetChunkRefsWithSizingInfo(ctx, userID, from, through, predicate)
		return err
	}); err != nil {
		return nil, err
	}

	return chunks, nil
}

func (m MonitoredReaderWriter) GetSeries(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]labels.Labels, error) {
	var lbls []labels.Labels
	if err := loki_instrument.TimeRequest(ctx, "series", instrument.NewHistogramCollector(m.metrics.indexQueryLatency), instrument.ErrorCode, func(ctx context.Context) error {
		var err error
		lbls, err = m.rw.GetSeries(ctx, userID, from, through, matchers...)
		return err
	}); err != nil {
		return nil, err
	}

	return lbls, nil
}

func (m MonitoredReaderWriter) LabelValuesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string, labelName string, matchers ...*labels.Matcher) ([]string, error) {
	var values []string
	if err := loki_instrument.TimeRequest(ctx, "label_values", instrument.NewHistogramCollector(m.metrics.indexQueryLatency), instrument.ErrorCode, func(ctx context.Context) error {
		var err error
		values, err = m.rw.LabelValuesForMetricName(ctx, userID, from, through, metricName, labelName, matchers...)
		return err
	}); err != nil {
		return nil, err
	}

	return values, nil
}

func (m MonitoredReaderWriter) LabelNamesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string, matchers ...*labels.Matcher) ([]string, error) {
	var values []string
	if err := loki_instrument.TimeRequest(ctx, "label_names", instrument.NewHistogramCollector(m.metrics.indexQueryLatency), instrument.ErrorCode, func(ctx context.Context) error {
		var err error
		values, err = m.rw.LabelNamesForMetricName(ctx, userID, from, through, metricName, matchers...)
		return err
	}); err != nil {
		return nil, err
	}

	return values, nil
}

func (m MonitoredReaderWriter) Stats(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) (*stats.Stats, error) {
	var sts *stats.Stats
	if err := loki_instrument.TimeRequest(ctx, "stats", instrument.NewHistogramCollector(m.metrics.indexQueryLatency), instrument.ErrorCode, func(ctx context.Context) error {
		var err error
		sts, err = m.rw.Stats(ctx, userID, from, through, matchers...)
		return err
	}); err != nil {
		return nil, err
	}

	return sts, nil
}

func (m MonitoredReaderWriter) Volume(ctx context.Context, userID string, from, through model.Time, limit int32, targetLabels []string, aggregateBy string, matchers ...*labels.Matcher) (*logproto.VolumeResponse, error) {
	var vol *logproto.VolumeResponse
	if err := loki_instrument.TimeRequest(ctx, "volume", instrument.NewHistogramCollector(m.metrics.indexQueryLatency), instrument.ErrorCode, func(ctx context.Context) error {
		var err error
		vol, err = m.rw.Volume(ctx, userID, from, through, limit, targetLabels, aggregateBy, matchers...)
		return err
	}); err != nil {
		return nil, err
	}

	return vol, nil
}

func (m MonitoredReaderWriter) GetShards(
	ctx context.Context,
	userID string,
	from, through model.Time,
	targetBytesPerShard uint64,
	predicate chunk.Predicate,
) (*logproto.ShardsResponse, error) {
	var shards *logproto.ShardsResponse
	if err := loki_instrument.TimeRequest(ctx, "shards", instrument.NewHistogramCollector(m.metrics.indexQueryLatency), instrument.ErrorCode, func(ctx context.Context) error {
		var err error
		start := time.Now()
		shards, err = m.rw.GetShards(ctx, userID, from, through, targetBytesPerShard, predicate)

		if err == nil {
			// record duration here from caller to avoid needing to do this in two separate places:
			// 1) when we resolve shards from the index alone
			// 2) when we resolve shards from the index + blooms
			// NB(owen-d): since this is measured by the callee, it does not include time in queue,
			// over the wire, etc.
			shards.Statistics.Index.ShardsDuration = int64(time.Since(start))
		}

		return err
	}); err != nil {
		return nil, err
	}
	return shards, nil
}

func (m MonitoredReaderWriter) SetChunkFilterer(chunkFilter chunk.RequestChunkFilterer) {
	m.rw.SetChunkFilterer(chunkFilter)
}

func (m MonitoredReaderWriter) IndexChunk(ctx context.Context, from, through model.Time, chk chunk.Chunk) error {
	return loki_instrument.TimeRequest(ctx, "index_chunk", instrument.NewHistogramCollector(m.metrics.indexQueryLatency), instrument.ErrorCode, func(ctx context.Context) error {
		return m.rw.IndexChunk(ctx, from, through, chk)
	})
}

func (m MonitoredReaderWriter) HasForSeries(from, through model.Time) (sharding.ForSeries, bool) {
	if impl, ok := m.rw.HasForSeries(from, through); ok {
		wrapped := sharding.ForSeriesFunc(
			func(
				ctx context.Context,
				userID string,
				fpFilter index.FingerprintFilter,
				from model.Time,
				through model.Time,
				fn func(
					labels.Labels,
					model.Fingerprint,
					[]index.ChunkMeta,
				) (stop bool),
				matchers ...*labels.Matcher,
			) error {
				return loki_instrument.TimeRequest(ctx, "for_series", instrument.NewHistogramCollector(m.metrics.indexQueryLatency), instrument.ErrorCode, func(ctx context.Context) error {
					return impl.ForSeries(ctx, userID, fpFilter, from, through, fn, matchers...)
				})
			},
		)
		return wrapped, true
	}
	return nil, false
}

func (m MonitoredReaderWriter) HasChunkSizingInfo(from, through model.Time) bool {
	return m.rw.HasChunkSizingInfo(from, through)
}
