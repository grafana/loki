package ingester

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/tenant"
	"github.com/opentracing/opentracing-go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/tsdb/chunks"
	tsdb_record "github.com/prometheus/prometheus/tsdb/record"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/analytics"
	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/compactor/retention"
	"github.com/grafana/loki/v3/pkg/distributor/writefailures"
	"github.com/grafana/loki/v3/pkg/ingester/index"
	"github.com/grafana/loki/v3/pkg/ingester/wal"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/loghttp/push"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel/metadata"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/runtime"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/seriesvolume"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/grafana/loki/v3/pkg/util/deletion"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	server_util "github.com/grafana/loki/v3/pkg/util/server"
	"github.com/grafana/loki/v3/pkg/validation"
)

const (
	// ShardLbName is the internal label to be used by Loki when dividing a stream into smaller pieces.
	// Possible values are only increasing integers starting from 0.
	ShardLbName        = "__stream_shard__"
	ShardLbPlaceholder = "__placeholder__"

	queryBatchSize       = 128
	queryBatchSampleSize = 512
)

var (
	memoryStreams = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: constants.Loki,
		Name:      "ingester_memory_streams",
		Help:      "The total number of streams in memory per tenant.",
	}, []string{"tenant"})
	memoryStreamsLabelsBytes = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: constants.Loki,
		Name:      "ingester_memory_streams_labels_bytes",
		Help:      "Total bytes of labels of the streams in memory.",
	})
	streamsCreatedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: constants.Loki,
		Name:      "ingester_streams_created_total",
		Help:      "The total number of streams created per tenant.",
	}, []string{"tenant"})
	streamsRemovedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: constants.Loki,
		Name:      "ingester_streams_removed_total",
		Help:      "The total number of streams removed per tenant.",
	}, []string{"tenant"})

	streamsCountStats = analytics.NewInt("ingester_streams_count")
)

type instance struct {
	cfg *Config

	buf     []byte // buffer used to compute fps.
	streams *streamsMap

	index  *index.Multi
	mapper *FpMapper // using of mapper no longer needs mutex because reading from streams is lock-free

	instanceID string

	streamsCreatedTotal prometheus.Counter
	streamsRemovedTotal prometheus.Counter

	tailers   map[uint32]*tailer
	tailerMtx sync.RWMutex

	limiter            *Limiter
	streamCountLimiter *streamCountLimiter
	ownedStreamsSvc    *ownedStreamService

	configs *runtime.TenantConfigs

	wal WAL

	// Denotes whether the ingester should flush on shutdown.
	// Currently only used by the WAL to signal when the disk is full.
	flushOnShutdownSwitch *OnceSwitch

	metrics *ingesterMetrics

	chunkFilter          chunk.RequestChunkFilterer
	pipelineWrapper      log.PipelineWrapper
	extractorWrapper     log.SampleExtractorWrapper
	streamRateCalculator *StreamRateCalculator

	writeFailures *writefailures.Manager

	schemaconfig *config.SchemaConfig

	customStreamsTracker push.UsageTracker

	tenantsRetention *retention.TenantsRetention
}

func newInstance(
	cfg *Config,
	periodConfigs []config.PeriodConfig,
	instanceID string,
	limiter *Limiter,
	configs *runtime.TenantConfigs,
	wal WAL,
	metrics *ingesterMetrics,
	flushOnShutdownSwitch *OnceSwitch,
	chunkFilter chunk.RequestChunkFilterer,
	pipelineWrapper log.PipelineWrapper,
	extractorWrapper log.SampleExtractorWrapper,
	streamRateCalculator *StreamRateCalculator,
	writeFailures *writefailures.Manager,
	customStreamsTracker push.UsageTracker,
	tenantsRetention *retention.TenantsRetention,
) (*instance, error) {
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

		streamsCreatedTotal: streamsCreatedTotal.WithLabelValues(instanceID),
		streamsRemovedTotal: streamsRemovedTotal.WithLabelValues(instanceID),

		tailers:            map[uint32]*tailer{},
		limiter:            limiter,
		streamCountLimiter: newStreamCountLimiter(instanceID, streams.Len, limiter, ownedStreamsSvc),
		ownedStreamsSvc:    ownedStreamsSvc,
		configs:            configs,

		wal:                   wal,
		metrics:               metrics,
		flushOnShutdownSwitch: flushOnShutdownSwitch,

		chunkFilter:      chunkFilter,
		pipelineWrapper:  pipelineWrapper,
		extractorWrapper: extractorWrapper,

		streamRateCalculator: streamRateCalculator,

		writeFailures: writeFailures,
		schemaconfig:  &c,

		customStreamsTracker: customStreamsTracker,

		tenantsRetention: tenantsRetention,
	}
	i.mapper = NewFPMapper(i.getLabelsFromFingerprint)

	return i, err
}

// consumeChunk manually adds a chunk that was received during ingester chunk
// transfer.
func (i *instance) consumeChunk(ctx context.Context, ls labels.Labels, chunk *logproto.Chunk) error {
	fp := i.getHashForLabels(ls)

	s, _, _ := i.streams.LoadOrStoreNewByFP(fp,
		func() (*stream, error) {
			s, err := i.createStreamByFP(ls, fp)
			s.chunkMtx.Lock() // Lock before return, because we have defer that unlocks it.
			if err != nil {
				return nil, err
			}
			return s, nil
		},
		func(s *stream) error {
			s.chunkMtx.Lock()
			return nil
		},
	)
	defer s.chunkMtx.Unlock()

	err := s.consumeChunk(ctx, chunk)
	if err == nil {
		i.metrics.memoryChunks.Inc()
	}

	return err
}

// Push will iterate over the given streams present in the PushRequest and attempt to store them.
//
// Although multiple streams are part of the PushRequest, the returned error only reflects what
// happened to *the last stream in the request*. Ex: if three streams are part of the PushRequest
// and all three failed, the returned error only describes what happened to the last processed stream.
func (i *instance) Push(ctx context.Context, req *logproto.PushRequest) error {
	record := recordPool.GetRecord()
	record.UserID = i.instanceID
	defer recordPool.PutRecord(record)
	rateLimitWholeStream := i.limiter.limits.ShardStreams(i.instanceID).Enabled

	var appendErr error
	for _, reqStream := range req.Streams {

		s, _, err := i.streams.LoadOrStoreNew(reqStream.Labels,
			func() (*stream, error) {
				s, err := i.createStream(ctx, reqStream, record)
				// Lock before adding to maps
				if err == nil {
					s.chunkMtx.Lock()
				}
				return s, err
			},
			func(s *stream) error {
				s.chunkMtx.Lock()
				return nil
			},
		)
		if err != nil {
			appendErr = err
			continue
		}

		_, appendErr = s.Push(ctx, reqStream.Entries, record, 0, false, rateLimitWholeStream, i.customStreamsTracker)
		s.chunkMtx.Unlock()
	}

	if !record.IsEmpty() {
		if err := i.wal.Log(record); err != nil {
			if e, ok := err.(*os.PathError); ok && e.Err == syscall.ENOSPC {
				i.metrics.walDiskFullFailures.Inc()
				i.flushOnShutdownSwitch.TriggerAnd(func() {
					level.Error(util_log.Logger).Log(
						"msg",
						"Error writing to WAL, disk full, no further messages will be logged for this error",
					)
				})
			} else {
				return err
			}
		}
	}

	return appendErr
}

func (i *instance) createStream(ctx context.Context, pushReqStream logproto.Stream, record *wal.Record) (*stream, error) {
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
		return nil, httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error())
	}

	retentionHours := util.RetentionHours(i.tenantsRetention.RetentionPeriodFor(i.instanceID, labels))
	mapping := i.limiter.limits.PoliciesStreamMapping(i.instanceID)
	policies := mapping.PolicyFor(labels)
	if record != nil {
		err = i.streamCountLimiter.AssertNewStreamAllowed(i.instanceID)
	}

	// NOTE: We previously resolved the policy on distributors and logged when multiple policies were matched.
	// As on distributors, we use the first policy by alphabetical order.
	var policy string
	if len(policies) > 0 {
		policy = policies[0]
		if len(policies) > 1 {
			level.Warn(util_log.Logger).Log(
				"msg", "multiple policies matched for the same stream",
				"org_id", i.instanceID,
				"stream", pushReqStream.Labels,
				"policy", policy,
				"policies", strings.Join(policies, ","),
			)
		}
	}

	if err != nil {
		return i.onStreamCreationError(ctx, pushReqStream, err, labels, retentionHours, policy)
	}

	fp := i.getHashForLabels(labels)

	sortedLabels := i.index.Add(logproto.FromLabelsToLabelAdapters(labels), fp)

	chunkfmt, headfmt, err := i.chunkFormatAt(minTs(&pushReqStream))
	if err != nil {
		return nil, fmt.Errorf("failed to create stream: %w", err)
	}

	s := newStream(chunkfmt, headfmt, i.cfg, i.limiter.rateLimitStrategy, i.instanceID, fp, sortedLabels, i.limiter.UnorderedWrites(i.instanceID), i.streamRateCalculator, i.metrics, i.writeFailures, i.configs, retentionHours)

	// record will be nil when replaying the wal (we don't want to rewrite wal entries as we replay them).
	if record != nil {
		record.Series = append(record.Series, tsdb_record.RefSeries{
			Ref:    chunks.HeadSeriesRef(fp),
			Labels: sortedLabels,
		})
	} else {
		// If the record is nil, this is a WAL recovery.
		i.metrics.recoveredStreamsTotal.Inc()
	}

	i.onStreamCreated(s)

	return s, nil
}

func (i *instance) onStreamCreationError(ctx context.Context, pushReqStream logproto.Stream, err error, labels labels.Labels, retentionHours, policy string) (*stream, error) {
	if i.configs.LogStreamCreation(i.instanceID) || i.cfg.KafkaIngestion.Enabled {
		l := level.Debug(util_log.Logger)

		if i.cfg.KafkaIngestion.Enabled {
			l = level.Warn(util_log.Logger)
		}

		l.Log(
			"msg", "failed to create stream, exceeded limit",
			"org_id", i.instanceID,
			"err", err,
			"stream", pushReqStream.Labels,
		)
	}

	validation.DiscardedSamples.WithLabelValues(validation.StreamLimit, i.instanceID, retentionHours, policy).Add(float64(len(pushReqStream.Entries)))
	bytes := util.EntriesTotalSize(pushReqStream.Entries)
	validation.DiscardedBytes.WithLabelValues(validation.StreamLimit, i.instanceID, retentionHours, policy).Add(float64(bytes))
	if i.customStreamsTracker != nil {
		i.customStreamsTracker.DiscardedBytesAdd(ctx, i.instanceID, validation.StreamLimit, labels, float64(bytes))
	}
	return nil, httpgrpc.Errorf(http.StatusTooManyRequests, validation.StreamLimitErrorMsg, labels, i.instanceID)
}

func (i *instance) onStreamCreated(s *stream) {
	memoryStreams.WithLabelValues(i.instanceID).Inc()
	memoryStreamsLabelsBytes.Add(float64(len(s.labels.String())))
	i.streamsCreatedTotal.Inc()
	i.addTailersToNewStream(s)
	streamsCountStats.Add(1)
	// we count newly created stream as owned
	i.ownedStreamsSvc.trackStreamOwnership(s.fp, true)
	if i.configs.LogStreamCreation(i.instanceID) {
		level.Debug(util_log.Logger).Log(
			"msg", "successfully created stream",
			"org_id", i.instanceID,
			"stream", s.labels.String(),
		)
	}
}

func (i *instance) createStreamByFP(ls labels.Labels, fp model.Fingerprint) (*stream, error) {
	sortedLabels := i.index.Add(logproto.FromLabelsToLabelAdapters(ls), fp)

	chunkfmt, headfmt, err := i.chunkFormatAt(model.Now())
	if err != nil {
		return nil, fmt.Errorf("failed to create stream for fingerprint: %w", err)
	}

	retentionHours := util.RetentionHours(i.tenantsRetention.RetentionPeriodFor(i.instanceID, ls))
	s := newStream(chunkfmt, headfmt, i.cfg, i.limiter.rateLimitStrategy, i.instanceID, fp, sortedLabels, i.limiter.UnorderedWrites(i.instanceID), i.streamRateCalculator, i.metrics, i.writeFailures, i.configs, retentionHours)

	i.onStreamCreated(s)

	return s, nil
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

// getOrCreateStream returns the stream or creates it.
// It's safe to use this function if returned stream is not consistency sensitive to streamsMap(e.g. ingesterRecoverer),
// otherwise use streamsMap.LoadOrStoreNew with locking stream's chunkMtx inside.
func (i *instance) getOrCreateStream(ctx context.Context, pushReqStream logproto.Stream, record *wal.Record) (*stream, error) {
	s, _, err := i.streams.LoadOrStoreNew(pushReqStream.Labels, func() (*stream, error) {
		return i.createStream(ctx, pushReqStream, record)
	}, nil)

	return s, err
}

// removeStream removes a stream from the instance.
func (i *instance) removeStream(s *stream) {
	if i.streams.Delete(s) {
		i.index.Delete(s.labels, s.fp)
		i.streamsRemovedTotal.Inc()
		memoryStreams.WithLabelValues(i.instanceID).Dec()
		memoryStreamsLabelsBytes.Sub(float64(len(s.labels.String())))
		streamsCountStats.Add(-1)
		i.ownedStreamsSvc.trackRemovedStream(s.fp)
	}
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

func (i *instance) Query(ctx context.Context, req logql.SelectLogParams) (iter.EntryIterator, error) {
	it, err := i.query(ctx, req)
	err = server_util.ClientGrpcStatusAndError(err)
	return it, err
}

func (i *instance) query(ctx context.Context, req logql.SelectLogParams) (iter.EntryIterator, error) {
	expr, err := req.LogSelector()
	if err != nil {
		return nil, err
	}

	pipeline, err := expr.Pipeline()
	if err != nil {
		return nil, err
	}

	pipeline, err = deletion.SetupPipeline(req, pipeline)
	if err != nil {
		return nil, err
	}

	if i.pipelineWrapper != nil && httpreq.ExtractHeader(ctx, httpreq.LokiDisablePipelineWrappersHeader) != "true" {
		userID, err := tenant.TenantID(ctx)
		if err != nil {
			return nil, err
		}

		pipeline = i.pipelineWrapper.Wrap(ctx, pipeline, req.Plan.String(), userID)
	}

	stats := stats.FromContext(ctx)
	var iters []iter.EntryIterator

	shard, err := parseShardFromRequest(req.Shards)
	if err != nil {
		return nil, err
	}

	err = i.forMatchingStreams(
		ctx,
		req.Start,
		expr.Matchers(),
		shard,
		func(stream *stream) error {
			iter, err := stream.Iterator(ctx, stats, req.Start, req.End, req.Direction, pipeline.ForStream(stream.labels))
			if err != nil {
				return err
			}
			iters = append(iters, iter)
			return nil
		},
	)
	if err != nil {
		return nil, err
	}

	return iter.NewSortEntryIterator(iters, req.Direction), nil
}

func (i *instance) QuerySample(ctx context.Context, req logql.SelectSampleParams) (iter.SampleIterator, error) {
	it, err := i.querySample(ctx, req)
	err = server_util.ClientGrpcStatusAndError(err)
	return it, err
}

func (i *instance) querySample(ctx context.Context, req logql.SelectSampleParams) (iter.SampleIterator, error) {
	expr, err := req.Expr()
	if err != nil {
		return nil, err
	}

	extractors, err := expr.Extractors()
	if err != nil {
		return nil, err
	}

	for j, extractor := range extractors {
		extractor, err = deletion.SetupExtractor(req, extractor)
		if err != nil {
			return nil, err
		}

		if i.extractorWrapper != nil &&
			httpreq.ExtractHeader(ctx, httpreq.LokiDisablePipelineWrappersHeader) != "true" {
			userID, err := tenant.TenantID(ctx)
			if err != nil {
				return nil, err
			}

			extractor = i.extractorWrapper.Wrap(ctx, extractor, req.Plan.String(), userID)
		}

		extractors[j] = extractor
	}

	stats := stats.FromContext(ctx)
	var iters []iter.SampleIterator

	shard, err := parseShardFromRequest(req.Shards)
	if err != nil {
		return nil, err
	}
	selector, err := expr.Selector()
	if err != nil {
		return nil, err
	}
	err = i.forMatchingStreams(
		ctx,
		req.Start,
		selector.Matchers(),
		shard,
		func(stream *stream) error {
			streamExtractors := make([]log.StreamSampleExtractor, 0, len(extractors))
			for _, extractor := range extractors {
				streamExtractors = append(streamExtractors, extractor.ForStream(stream.labels))
			}

			iter, err := stream.SampleIterator(
				ctx,
				stats,
				req.Start,
				req.End,
				streamExtractors...,
			)
			if err != nil {
				return err
			}
			iters = append(iters, iter)
			return nil
		},
	)
	if err != nil {
		return nil, err
	}

	return iter.NewSortSampleIterator(iters), nil
}

// Label returns the label names or values depending on the given request
// Without label matchers the label names and values are retrieved from the index directly.
// If label matchers are given only the matching streams are fetched from the index.
// The label names or values are then retrieved from those matching streams.
func (i *instance) Label(ctx context.Context, req *logproto.LabelRequest, matchers ...*labels.Matcher) (*logproto.LabelResponse, error) {
	lr, err := i.label(ctx, req, matchers...)
	err = server_util.ClientGrpcStatusAndError(err)
	return lr, err
}

func (i *instance) label(ctx context.Context, req *logproto.LabelRequest, matchers ...*labels.Matcher) (*logproto.LabelResponse, error) {
	if len(matchers) == 0 {
		var labels []string
		if req.Values {
			values, err := i.index.LabelValues(*req.Start, req.Name, nil)
			if err != nil {
				return nil, err
			}
			labels = make([]string, len(values))
			copy(labels, values)
			return &logproto.LabelResponse{
				Values: labels,
			}, nil
		}
		names, err := i.index.LabelNames(*req.Start, nil)
		if err != nil {
			return nil, err
		}
		labels = make([]string, len(names))
		copy(labels, names)
		return &logproto.LabelResponse{
			Values: labels,
		}, nil
	}

	labels := util.NewUniqueStrings(0)
	err := i.forMatchingStreams(ctx, *req.Start, matchers, nil, func(s *stream) error {
		for _, label := range s.labels {
			if req.Values && label.Name == req.Name {
				labels.Add(label.Value)
				continue
			}
			if !req.Values {
				labels.Add(label.Name)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &logproto.LabelResponse{
		Values: labels.Strings(),
	}, nil
}

type UniqueValues map[string]struct{}

// LabelsWithValues returns the label names with all the unique values depending on the request
func (i *instance) LabelsWithValues(ctx context.Context, startTime time.Time, matchers ...*labels.Matcher) (map[string]UniqueValues, error) {
	labelMap := make(map[string]UniqueValues)
	if len(matchers) == 0 {
		labelsFromIndex, err := i.index.LabelNames(startTime, nil)
		if err != nil {
			return nil, err
		}

		for _, label := range labelsFromIndex {
			values, err := i.index.LabelValues(startTime, label, nil)
			if err != nil {
				return nil, err
			}
			existingValues, exists := labelMap[label]
			if !exists {
				existingValues = make(map[string]struct{})
			}
			for _, v := range values {
				existingValues[v] = struct{}{}
			}
			labelMap[label] = existingValues
		}

		return labelMap, nil
	}

	err := i.forMatchingStreams(ctx, startTime, matchers, nil, func(s *stream) error {
		for _, label := range s.labels {
			v, exists := labelMap[label.Name]
			if !exists {
				v = make(map[string]struct{})
			}
			if label.Value != "" {
				v[label.Value] = struct{}{}
			}
			labelMap[label.Name] = v
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return labelMap, nil
}

func (i *instance) Series(ctx context.Context, req *logproto.SeriesRequest) (*logproto.SeriesResponse, error) {
	groups, err := logql.MatchForSeriesRequest(req.GetGroups())
	if err != nil {
		return nil, err
	}
	shard, err := parseShardFromRequest(req.Shards)
	if err != nil {
		return nil, err
	}

	var series []logproto.SeriesIdentifier

	// If no matchers were supplied we include all streams.
	if len(groups) == 0 {
		series = make([]logproto.SeriesIdentifier, 0, i.streams.Len())
		err = i.forMatchingStreams(ctx, req.Start, nil, shard, func(stream *stream) error {
			// consider the stream only if it overlaps the request time range
			if shouldConsiderStream(stream, req.Start, req.End) {
				series = append(series, logproto.SeriesIdentifierFromLabels(stream.labels))
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	} else {
		dedupedSeries := make(map[uint64]logproto.SeriesIdentifier)
		for _, matchers := range groups {
			err = i.forMatchingStreams(ctx, req.Start, matchers, shard, func(stream *stream) error {
				// consider the stream only if it overlaps the request time range
				if shouldConsiderStream(stream, req.Start, req.End) {
					// exit early when this stream was added by an earlier group
					key := stream.labelHash

					// TODO(karsten): Due to key collision this check is not
					// enough. Ideally there is a comparison between the
					// potentail duplicated series.
					if _, found := dedupedSeries[key]; found {
						return nil
					}

					dedupedSeries[key] = logproto.SeriesIdentifierFromLabels(stream.labels)
				}
				return nil
			})
			if err != nil {
				return nil, err
			}
		}
		series = make([]logproto.SeriesIdentifier, 0, len(dedupedSeries))
		for _, v := range dedupedSeries {
			series = append(series, v)
		}
	}

	return &logproto.SeriesResponse{Series: series}, nil
}

func (i *instance) GetStats(ctx context.Context, req *logproto.IndexStatsRequest) (*logproto.IndexStatsResponse, error) {
	isr, err := i.getStats(ctx, req)
	err = server_util.ClientGrpcStatusAndError(err)
	return isr, err
}

func (i *instance) getStats(ctx context.Context, req *logproto.IndexStatsRequest) (*logproto.IndexStatsResponse, error) {
	matchers, err := syntax.ParseMatchers(req.Matchers, true)
	if err != nil {
		return nil, err
	}

	res := &logproto.IndexStatsResponse{}
	from, through := req.From.Time(), req.Through.Time()

	if err = i.forMatchingStreams(ctx, from, matchers, nil, func(s *stream) error {
		// Consider streams which overlap our time range
		if shouldConsiderStream(s, from, through) {
			s.chunkMtx.RLock()
			var hasChunkOverlap bool
			for _, chk := range s.chunks {
				// Consider chunks which overlap our time range
				// and haven't been flushed.
				// Flushed chunks will already be counted
				// by the TSDB manager+shipper
				chkFrom, chkThrough := chk.chunk.Bounds()

				if chk.flushed.IsZero() && from.Before(chkThrough) && through.After(chkFrom) {
					hasChunkOverlap = true
					res.Chunks++
					factor := util.GetFactorOfTime(from.UnixNano(), through.UnixNano(), chkFrom.UnixNano(), chkThrough.UnixNano())
					res.Entries += uint64(factor * float64(chk.chunk.Size()))
					res.Bytes += uint64(factor * float64(chk.chunk.UncompressedSize()))
				}

			}
			if hasChunkOverlap {
				res.Streams++
			}
			s.chunkMtx.RUnlock()
		}
		return nil
	}); err != nil {
		return nil, err
	}

	if sp := opentracing.SpanFromContext(ctx); sp != nil {
		sp.LogKV(
			"function", "instance.GetStats",
			"from", from,
			"through", through,
			"matchers", syntax.MatchersString(matchers),
			"streams", res.Streams,
			"chunks", res.Chunks,
			"bytes", res.Bytes,
			"entries", res.Entries,
		)
	}

	return res, nil
}

func (i *instance) GetVolume(ctx context.Context, req *logproto.VolumeRequest) (*logproto.VolumeResponse, error) {
	vr, err := i.getVolume(ctx, req)
	err = server_util.ClientGrpcStatusAndError(err)
	return vr, err
}

func (i *instance) getVolume(ctx context.Context, req *logproto.VolumeRequest) (*logproto.VolumeResponse, error) {
	matchers, err := syntax.ParseMatchers(req.Matchers, true)
	if err != nil && req.Matchers != seriesvolume.MatchAny {
		return nil, err
	}

	targetLabels := req.TargetLabels
	labelsToMatch, matchers, matchAny := util.PrepareLabelsAndMatchers(targetLabels, matchers)
	matchAny = matchAny || len(matchers) == 0

	seriesNames := make(map[uint64]string)
	seriesLabels := labels.Labels(make([]labels.Label, 0, len(labelsToMatch)))

	from, through := req.From.Time(), req.Through.Time()
	volumes := make(map[string]uint64)
	aggregateBySeries := seriesvolume.AggregateBySeries(req.AggregateBy) || req.AggregateBy == ""

	if err = i.forMatchingStreams(ctx, from, matchers, nil, func(s *stream) error {
		// Consider streams which overlap our time range
		if shouldConsiderStream(s, from, through) {
			s.chunkMtx.RLock()

			var size uint64
			for _, chk := range s.chunks {
				// Consider chunks which overlap our time range
				// and haven't been flushed.
				// Flushed chunks will already be counted
				// by the TSDB manager+shipper
				chkFrom, chkThrough := chk.chunk.Bounds()

				if chk.flushed.IsZero() && from.Before(chkThrough) && through.After(chkFrom) {
					factor := util.GetFactorOfTime(from.UnixNano(), through.UnixNano(), chkFrom.UnixNano(), chkThrough.UnixNano())
					size += uint64(float64(chk.chunk.UncompressedSize()) * factor)
				}
			}

			var labelVolumes map[string]uint64
			if aggregateBySeries {
				seriesLabels = seriesLabels[:0]
				for _, l := range s.labels {
					if _, ok := labelsToMatch[l.Name]; matchAny || ok {
						seriesLabels = append(seriesLabels, l)
					}
				}
			} else {
				labelVolumes = make(map[string]uint64, len(s.labels))
				for _, l := range s.labels {
					if len(targetLabels) > 0 {
						if _, ok := labelsToMatch[l.Name]; matchAny || ok {
							labelVolumes[l.Name] += size
						}
					} else {
						labelVolumes[l.Name] += size
					}
				}
			}

			// If the labels are < 1k, this does not alloc
			// https://github.com/prometheus/prometheus/pull/8025
			hash := seriesLabels.Hash()
			if _, ok := seriesNames[hash]; !ok {
				seriesNames[hash] = seriesLabels.String()
			}

			if aggregateBySeries {
				volumes[seriesNames[hash]] += size
			} else {
				for k, v := range labelVolumes {
					volumes[k] += v
				}
			}
			s.chunkMtx.RUnlock()
		}
		return nil
	}); err != nil {
		return nil, err
	}

	res := seriesvolume.MapToVolumeResponse(volumes, int(req.Limit))
	return res, nil
}

func (i *instance) numStreams() int {
	return i.streams.Len()
}

// forAllStreams will execute a function for all streams in the instance.
// It uses a function in order to enable generic stream access without accidentally leaking streams under the mutex.
func (i *instance) forAllStreams(ctx context.Context, fn func(*stream) error) error {
	var chunkFilter chunk.Filterer
	if i.chunkFilter != nil {
		chunkFilter = i.chunkFilter.ForRequest(ctx)
	}

	err := i.streams.ForEach(func(s *stream) (bool, error) {
		if chunkFilter != nil && chunkFilter.ShouldFilter(s.labels) {
			return true, nil
		}
		err := fn(s)
		if err != nil {
			return false, err
		}
		return true, nil
	})
	if err != nil {
		return err
	}
	return nil
}

// forMatchingStreams will execute a function for each stream that satisfies a set of requirements (time range, matchers, etc).
// It uses a function in order to enable generic stream access without accidentally leaking streams under the mutex.
func (i *instance) forMatchingStreams(
	ctx context.Context,
	// ts denotes the beginning of the request
	// and is used to select the correct inverted index
	ts time.Time,
	matchers []*labels.Matcher,
	shard *logql.Shard,
	fn func(*stream) error,
) error {
	filters, matchers := util.SplitFiltersAndMatchers(matchers)
	ids, err := i.index.Lookup(ts, matchers, shard)
	if err != nil {
		return err
	}
	var chunkFilter chunk.Filterer
	if i.chunkFilter != nil {
		chunkFilter = i.chunkFilter.ForRequest(ctx)
	}
outer:
	for _, streamID := range ids {
		stream, ok := i.streams.LoadByFP(streamID)
		if !ok {
			// If a stream is missing here, it has already been flushed
			// and is supposed to be picked up from storage by querier
			continue
		}
		for _, filter := range filters {
			if !filter.Matches(stream.labels.Get(filter.Name)) {
				continue outer
			}
		}
		if chunkFilter != nil && chunkFilter.ShouldFilter(stream.labels) {
			continue
		}
		err := fn(stream)
		if err != nil {
			return err
		}
	}
	return nil
}

func (i *instance) addNewTailer(ctx context.Context, t *tailer) error {
	if err := i.forMatchingStreams(ctx, time.Now(), t.matchers, nil, func(s *stream) error {
		s.addTailer(t)
		return nil
	}); err != nil {
		return err
	}
	i.tailerMtx.Lock()
	defer i.tailerMtx.Unlock()
	i.tailers[t.getID()] = t
	return nil
}

func (i *instance) addTailersToNewStream(stream *stream) {
	i.tailerMtx.RLock()
	defer i.tailerMtx.RUnlock()

	for _, t := range i.tailers {
		// we don't want to watch streams for closed tailers.
		// When a new tail request comes in we will clean references to closed tailers
		if t.isClosed() {
			continue
		}
		var chunkFilter chunk.Filterer
		if i.chunkFilter != nil {
			chunkFilter = i.chunkFilter.ForRequest(t.conn.Context())
		}

		if isMatching(stream.labels, t.matchers) {
			if chunkFilter != nil && chunkFilter.ShouldFilter(stream.labels) {
				continue
			}
			stream.addTailer(t)
		}
	}
}

func (i *instance) checkClosedTailers() {
	closedTailers := []uint32{}

	i.tailerMtx.RLock()
	for _, t := range i.tailers {
		if t.isClosed() {
			closedTailers = append(closedTailers, t.getID())
			continue
		}
	}
	i.tailerMtx.RUnlock()

	if len(closedTailers) != 0 {
		i.tailerMtx.Lock()
		defer i.tailerMtx.Unlock()
		for _, closedTailer := range closedTailers {
			delete(i.tailers, closedTailer)
		}
	}
}

func (i *instance) closeTailers() {
	i.tailerMtx.Lock()
	defer i.tailerMtx.Unlock()
	for _, t := range i.tailers {
		t.close()
	}
}

func (i *instance) openTailersCount() uint32 {
	i.checkClosedTailers()

	i.tailerMtx.RLock()
	defer i.tailerMtx.RUnlock()

	return uint32(len(i.tailers))
}

func parseShardFromRequest(reqShards []string) (*logql.Shard, error) {
	var shard *logql.Shard
	shards, _, err := logql.ParseShards(reqShards)
	if err != nil {
		return nil, err
	}
	if len(shards) > 1 {
		return nil, errors.New("only one shard per ingester query is supported")
	}
	if len(shards) == 1 {
		shard = &shards[0]
	}
	return shard, nil
}

func isDone(ctx context.Context) bool {
	return ctx.Err() != nil
}

// QuerierQueryServer is the GRPC server stream we use to send batch of entries.
type QuerierQueryServer interface {
	Context() context.Context
	Send(res *logproto.QueryResponse) error
}

func sendBatches(ctx context.Context, i iter.EntryIterator, queryServer QuerierQueryServer, limit int32) error {
	stats := stats.FromContext(ctx)
	metadata := metadata.FromContext(ctx)

	// send until the limit is reached.
	for limit != 0 && !isDone(ctx) {
		fetchSize := uint32(queryBatchSize)
		if limit > 0 {
			fetchSize = min(queryBatchSize, uint32(limit))
		}
		batch, batchSize, err := iter.ReadBatch(i, fetchSize)
		if err != nil {
			return err
		}

		if limit > 0 {
			limit -= int32(batchSize)
		}

		stats.AddIngesterBatch(int64(batchSize))
		batch.Stats = stats.Ingester()
		batch.Warnings = metadata.Warnings()

		if isDone(ctx) {
			break
		}
		if err := queryServer.Send(batch); err != nil && err != context.Canceled {
			return err
		}

		// We check this after sending an empty batch to make sure stats are sent
		if len(batch.Streams) == 0 {
			return nil
		}

		stats.Reset()
		metadata.Reset()
	}
	return nil
}

func sendSampleBatches(ctx context.Context, it iter.SampleIterator, queryServer logproto.Querier_QuerySampleServer) error {
	sp := opentracing.SpanFromContext(ctx)

	stats := stats.FromContext(ctx)
	metadata := metadata.FromContext(ctx)
	for !isDone(ctx) {
		batch, size, err := iter.ReadSampleBatch(it, queryBatchSampleSize)
		if err != nil {
			return err
		}

		stats.AddIngesterBatch(int64(size))
		batch.Stats = stats.Ingester()
		batch.Warnings = metadata.Warnings()

		if isDone(ctx) {
			break
		}
		if err := queryServer.Send(batch); err != nil && err != context.Canceled {
			return err
		}

		// We check this after sending an empty batch to make sure stats are sent
		if len(batch.Series) == 0 {
			return nil
		}

		stats.Reset()
		metadata.Reset()

		if sp != nil {
			sp.LogKV("event", "sent batch", "size", size)
		}
	}

	return nil
}

func shouldConsiderStream(stream *stream, reqFrom, reqThrough time.Time) bool {
	from, to := stream.Bounds()

	if reqThrough.UnixNano() > from.UnixNano() && reqFrom.UnixNano() <= to.UnixNano() {
		return true
	}
	return false
}

// OnceSwitch is an optimized switch that can only ever be switched "on" in a concurrent environment.
type OnceSwitch struct {
	triggered atomic.Bool
}

func (o *OnceSwitch) Get() bool {
	return o.triggered.Load()
}

func (o *OnceSwitch) Trigger() {
	o.TriggerAnd(nil)
}

// TriggerAnd will ensure the switch is on and run the provided function if
// the switch was not already toggled on.
func (o *OnceSwitch) TriggerAnd(fn func()) {
	triggeredPrior := o.triggered.Swap(true)
	if !triggeredPrior && fn != nil {
		fn()
	}
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

// For each stream, we check if the stream is owned by the ingester or not and increment/decrement the owned stream count.
func (i *instance) updateOwnedStreams(isOwnedStream func(*stream) (bool, error)) error {
	start := time.Now()
	defer func() {
		i.metrics.streamsOwnershipCheck.Observe(float64(time.Since(start).Milliseconds()))
	}()

	var err error
	i.streams.WithRLock(func() {
		i.ownedStreamsSvc.resetStreamCounts()
		err = i.streams.ForEach(func(s *stream) (bool, error) {
			ownedStream, err := isOwnedStream(s)
			if err != nil {
				return false, err
			}

			i.ownedStreamsSvc.trackStreamOwnership(s.fp, ownedStream)
			return true, nil
		})
	})
	return err
}
