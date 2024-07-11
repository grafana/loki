package ingesterrf1

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"sync"

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/analytics"
	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/distributor/writefailures"
	"github.com/grafana/loki/v3/pkg/ingester/index"
	"github.com/grafana/loki/v3/pkg/loghttp/push"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/runtime"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/util/constants"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/validation"
)

var (
	memoryStreams = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: constants.Loki,
		Name:      "ingester_rf1_memory_streams",
		Help:      "The total number of streams in memory per tenant.",
	}, []string{"tenant"})
	memoryStreamsLabelsBytes = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: constants.Loki,
		Name:      "ingester_rf1_memory_streams_labels_bytes",
		Help:      "Total bytes of labels of the streams in memory.",
	})
	streamsCreatedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: constants.Loki,
		Name:      "ingester_rf1_streams_created_total",
		Help:      "The total number of streams created per tenant.",
	}, []string{"tenant"})
	streamsRemovedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: constants.Loki,
		Name:      "ingester_rf1_streams_removed_total",
		Help:      "The total number of streams removed per tenant.",
	}, []string{"tenant"})

	streamsCountStats = analytics.NewInt("ingester_rf1_streams_count")
)

type instance struct {
	streamsCreatedTotal prometheus.Counter
	streamsRemovedTotal prometheus.Counter

	customStreamsTracker push.UsageTracker
	cfg                  *Config

	streams *streamsMap

	index  *index.Multi
	mapper *FpMapper // using of mapper no longer needs mutex because reading from streams is lock-free

	limiter            *Limiter
	streamCountLimiter *streamCountLimiter
	ownedStreamsSvc    *ownedStreamService

	configs *runtime.TenantConfigs

	metrics *ingesterMetrics

	streamRateCalculator *StreamRateCalculator

	writeFailures *writefailures.Manager

	schemaconfig *config.SchemaConfig

	instanceID string

	buf []byte // buffer used to compute fps.

	// tailers   map[uint32]*tailer
	tailerMtx sync.RWMutex
}

func (i *instance) Push(ctx context.Context, req *logproto.PushRequest, flushCtx *flushCtx) error {
	rateLimitWholeStream := i.limiter.limits.ShardStreams(i.instanceID).Enabled

	var appendErr error
	for _, reqStream := range req.Streams {
		s, _, err := i.streams.LoadOrStoreNew(reqStream.Labels,
			func() (*stream, error) {
				s, err := i.createStream(ctx, reqStream)
				return s, err
			},
			func(s *stream) error {
				return nil
			},
		)
		if err != nil {
			appendErr = err
			continue
		}

		_, appendErr = s.Push(ctx, reqStream.Entries, rateLimitWholeStream, i.customStreamsTracker, flushCtx)
	}
	return appendErr
}

func newInstance(
	cfg *Config,
	periodConfigs []config.PeriodConfig,
	instanceID string,
	limiter *Limiter,
	configs *runtime.TenantConfigs,
	metrics *ingesterMetrics,
	streamRateCalculator *StreamRateCalculator,
	writeFailures *writefailures.Manager,
	customStreamsTracker push.UsageTracker,
) (*instance, error) {
	fmt.Println("new instance for", instanceID)
	invertedIndex, err := index.NewMultiInvertedIndex(periodConfigs, uint32(cfg.IndexShards))
	if err != nil {
		return nil, err
	}
	streams := newStreamsMap()
	ownedStreamsSvc := newOwnedStreamService(instanceID, limiter)
	c := config.SchemaConfig{Configs: periodConfigs}
	i := &instance{
		cfg:        cfg,
		streams:    streams,
		buf:        make([]byte, 0, 1024),
		index:      invertedIndex,
		instanceID: instanceID,
		//
		streamsCreatedTotal: streamsCreatedTotal.WithLabelValues(instanceID),
		streamsRemovedTotal: streamsRemovedTotal.WithLabelValues(instanceID),
		//
		//tailers:            map[uint32]*tailer{},
		limiter:            limiter,
		streamCountLimiter: newStreamCountLimiter(instanceID, streams.Len, limiter, ownedStreamsSvc),
		ownedStreamsSvc:    ownedStreamsSvc,
		configs:            configs,
		metrics:            metrics,

		streamRateCalculator: streamRateCalculator,

		writeFailures: writeFailures,
		schemaconfig:  &c,

		customStreamsTracker: customStreamsTracker,
	}
	i.mapper = NewFPMapper(i.getLabelsFromFingerprint)

	return i, nil
}

func (i *instance) createStream(ctx context.Context, pushReqStream logproto.Stream) (*stream, error) {
	// record is only nil when replaying WAL. We don't want to drop data when replaying a WAL after
	// reducing the stream limits, for instance.
	var err error

	labels, err := syntax.ParseLabels(pushReqStream.Labels)
	if err != nil {
		if i.configs.LogStreamCreation(i.instanceID) {
			level.Debug(util_log.Logger).Log(
				"msg", "failed to create stream, failed to parse labels",
				"org_id", i.instanceID,
				"err", err,
				"stream", pushReqStream.Labels,
			)
		}
		return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
	}

	if err != nil {
		return i.onStreamCreationError(ctx, pushReqStream, err, labels)
	}

	fp := i.getHashForLabels(labels)

	sortedLabels := i.index.Add(logproto.FromLabelsToLabelAdapters(labels), fp)

	chunkfmt, headfmt, err := i.chunkFormatAt(minTs(&pushReqStream))
	if err != nil {
		return nil, fmt.Errorf("failed to create stream: %w", err)
	}

	s := newStream(chunkfmt, headfmt, i.cfg, i.limiter, i.instanceID, fp, sortedLabels, i.limiter.UnorderedWrites(i.instanceID) /*i.streamRateCalculator,*/, i.metrics, i.writeFailures)

	i.onStreamCreated(s)

	return s, nil
}

// minTs is a helper to return minimum Unix timestamp (as `model.Time`)
// across all the entries in a given `stream`.
func minTs(stream *logproto.Stream) model.Time {
	// NOTE: We choose `min` timestamp because, the chunk is written once then
	// added to the index buckets for may be different days. It would better rather to have
	// some latest(say v13) indices reference older (say v12) compatible chunks than vice versa.

	streamMinTs := int64(math.MaxInt64)
	for _, entry := range stream.Entries {
		ts := entry.Timestamp.UnixNano()
		if streamMinTs > ts {
			streamMinTs = ts
		}
	}
	return model.TimeFromUnixNano(streamMinTs)
}

// chunkFormatAt returns chunk formats to use at given period of time.
func (i *instance) chunkFormatAt(at model.Time) (byte, chunkenc.HeadBlockFmt, error) {
	// NOTE: We choose chunk formats for stream based on it's entries timestamp.
	// Rationale being, a single (ingester) instance can be running across multiple schema period
	// and choosing correct periodConfig during creation of stream is more accurate rather
	// than choosing it during starting of instance itself.

	periodConfig, err := i.schemaconfig.SchemaForTime(at)
	if err != nil {
		return 0, 0, err
	}

	chunkFormat, headblock, err := periodConfig.ChunkFormat()
	if err != nil {
		return 0, 0, err
	}

	return chunkFormat, headblock, nil
}

func (i *instance) getHashForLabels(ls labels.Labels) model.Fingerprint {
	var fp uint64
	fp, i.buf = ls.HashWithoutLabels(i.buf, []string(nil)...)
	return i.mapper.MapFP(model.Fingerprint(fp), ls)
}

// Return labels associated with given fingerprint. Used by fingerprint mapper.
func (i *instance) getLabelsFromFingerprint(fp model.Fingerprint) labels.Labels {
	s, ok := i.streams.LoadByFP(fp)
	if !ok {
		return nil
	}
	return s.labels
}

func (i *instance) onStreamCreationError(ctx context.Context, pushReqStream logproto.Stream, err error, labels labels.Labels) (*stream, error) {
	if i.configs.LogStreamCreation(i.instanceID) {
		level.Debug(util_log.Logger).Log(
			"msg", "failed to create stream, exceeded limit",
			"org_id", i.instanceID,
			"err", err,
			"stream", pushReqStream.Labels,
		)
	}

	validation.DiscardedSamples.WithLabelValues(validation.StreamLimit, i.instanceID).Add(float64(len(pushReqStream.Entries)))
	bytes := 0
	for _, e := range pushReqStream.Entries {
		bytes += len(e.Line)
	}
	validation.DiscardedBytes.WithLabelValues(validation.StreamLimit, i.instanceID).Add(float64(bytes))
	if i.customStreamsTracker != nil {
		i.customStreamsTracker.DiscardedBytesAdd(ctx, i.instanceID, validation.StreamLimit, labels, float64(bytes))
	}
	return nil, httpgrpc.Errorf(http.StatusTooManyRequests, validation.StreamLimitErrorMsg, labels, i.instanceID)
}

func (i *instance) onStreamCreated(s *stream) {
	memoryStreams.WithLabelValues(i.instanceID).Inc()
	memoryStreamsLabelsBytes.Add(float64(len(s.labels.String())))
	i.streamsCreatedTotal.Inc()
	// i.addTailersToNewStream(s)
	streamsCountStats.Add(1)
	i.ownedStreamsSvc.incOwnedStreamCount()
	if i.configs.LogStreamCreation(i.instanceID) {
		level.Debug(util_log.Logger).Log(
			"msg", "successfully created stream",
			"org_id", i.instanceID,
			"stream", s.labels.String(),
		)
	}
}
