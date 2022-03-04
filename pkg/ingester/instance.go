package ingester

import (
	"context"
	"net/http"
	"os"
	"sync"
	"syscall"

	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/tsdb/chunks"
	tsdb_record "github.com/prometheus/prometheus/tsdb/record"
	"github.com/weaveworks/common/httpgrpc"
	"go.uber.org/atomic"

	"github.com/grafana/loki/pkg/ingester/index"
	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
	"github.com/grafana/loki/pkg/querier/astmapper"
	"github.com/grafana/loki/pkg/runtime"
	"github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/usagestats"
	"github.com/grafana/loki/pkg/util"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/util/math"
	"github.com/grafana/loki/pkg/validation"
)

const (
	queryBatchSize       = 128
	queryBatchSampleSize = 512
)

var (
	memoryStreams = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "loki",
		Name:      "ingester_memory_streams",
		Help:      "The total number of streams in memory per tenant.",
	}, []string{"tenant"})
	streamsCreatedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "ingester_streams_created_total",
		Help:      "The total number of streams created per tenant.",
	}, []string{"tenant"})
	streamsRemovedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "ingester_streams_removed_total",
		Help:      "The total number of streams removed per tenant.",
	}, []string{"tenant"})

	streamsCountStats = usagestats.NewInt("ingester_streams_count")
)

type instance struct {
	cfg *Config

	buf     []byte // buffer used to compute fps.
	streams *streamsMap

	index  *index.InvertedIndex
	mapper *fpMapper // using of mapper no longer needs mutex because reading from streams is lock-free

	instanceID string

	streamsCreatedTotal prometheus.Counter
	streamsRemovedTotal prometheus.Counter

	tailers   map[uint32]*tailer
	tailerMtx sync.RWMutex

	limiter *Limiter
	configs *runtime.TenantConfigs

	wal WAL

	// Denotes whether the ingester should flush on shutdown.
	// Currently only used by the WAL to signal when the disk is full.
	flushOnShutdownSwitch *OnceSwitch

	metrics *ingesterMetrics

	chunkFilter storage.RequestChunkFilterer
}

func newInstance(cfg *Config, instanceID string, limiter *Limiter, configs *runtime.TenantConfigs, wal WAL, metrics *ingesterMetrics, flushOnShutdownSwitch *OnceSwitch, chunkFilter storage.RequestChunkFilterer) *instance {
	i := &instance{
		cfg:        cfg,
		streams:    newStreamsMap(),
		buf:        make([]byte, 0, 1024),
		index:      index.NewWithShards(uint32(cfg.IndexShards)),
		instanceID: instanceID,

		streamsCreatedTotal: streamsCreatedTotal.WithLabelValues(instanceID),
		streamsRemovedTotal: streamsRemovedTotal.WithLabelValues(instanceID),

		tailers: map[uint32]*tailer{},
		limiter: limiter,
		configs: configs,

		wal:                   wal,
		metrics:               metrics,
		flushOnShutdownSwitch: flushOnShutdownSwitch,

		chunkFilter: chunkFilter,
	}
	i.mapper = newFPMapper(i.getLabelsFromFingerprint)
	return i
}

// consumeChunk manually adds a chunk that was received during ingester chunk
// transfer.
func (i *instance) consumeChunk(ctx context.Context, ls labels.Labels, chunk *logproto.Chunk) error {
	fp := i.getHashForLabels(ls)

	s, _, _ := i.streams.LoadOrStoreNewByFP(fp,
		func() (*stream, error) {
			s := i.createStreamByFP(ls, fp)
			s.chunkMtx.Lock()
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
		memoryChunks.Inc()
	}

	return err
}

func (i *instance) Push(ctx context.Context, req *logproto.PushRequest) error {
	record := recordPool.GetRecord()
	record.UserID = i.instanceID
	defer recordPool.PutRecord(record)

	var appendErr error
	for _, reqStream := range req.Streams {

		s, _, err := i.streams.LoadOrStoreNew(reqStream.Labels,
			func() (*stream, error) {
				s, err := i.createStream(reqStream, record)
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

		_, err = s.Push(ctx, reqStream.Entries, record, 0, false)
		if err != nil {
			appendErr = err
		}
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

func (i *instance) createStream(pushReqStream logproto.Stream, record *WALRecord) (*stream, error) {
	// record is only nil when replaying WAL. We don't want to drop data when replaying a WAL after
	// reducing the stream limits, for instance.
	var err error
	if record != nil {
		err = i.limiter.AssertMaxStreamsPerUser(i.instanceID, i.streams.Len())
	}

	if err != nil {
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
		return nil, httpgrpc.Errorf(http.StatusTooManyRequests, validation.StreamLimitErrorMsg)
	}

	labels, err := logql.ParseLabels(pushReqStream.Labels)
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
	fp := i.getHashForLabels(labels)

	sortedLabels := i.index.Add(logproto.FromLabelsToLabelAdapters(labels), fp)
	s := newStream(i.cfg, i.limiter, i.instanceID, fp, sortedLabels, i.limiter.UnorderedWrites(i.instanceID), i.metrics)

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

	memoryStreams.WithLabelValues(i.instanceID).Inc()
	i.streamsCreatedTotal.Inc()
	i.addTailersToNewStream(s)
	streamsCountStats.Add(1)

	if i.configs.LogStreamCreation(i.instanceID) {
		level.Debug(util_log.Logger).Log(
			"msg", "successfully created stream",
			"org_id", i.instanceID,
			"stream", pushReqStream.Labels,
		)
	}

	return s, nil
}

func (i *instance) createStreamByFP(ls labels.Labels, fp model.Fingerprint) *stream {
	sortedLabels := i.index.Add(logproto.FromLabelsToLabelAdapters(ls), fp)
	s := newStream(i.cfg, i.limiter, i.instanceID, fp, sortedLabels, i.limiter.UnorderedWrites(i.instanceID), i.metrics)

	i.streamsCreatedTotal.Inc()
	memoryStreams.WithLabelValues(i.instanceID).Inc()
	i.addTailersToNewStream(s)

	return s
}

// getOrCreateStream returns the stream or creates it.
// It's safe to use this function if returned stream is not consistency sensitive to streamsMap(e.g. ingesterRecoverer),
// otherwise use streamsMap.LoadOrStoreNew with locking stream's chunkMtx inside.
func (i *instance) getOrCreateStream(pushReqStream logproto.Stream, record *WALRecord) (*stream, error) {
	s, _, err := i.streams.LoadOrStoreNew(pushReqStream.Labels, func() (*stream, error) {
		return i.createStream(pushReqStream, record)
	}, nil)

	return s, err
}

// removeStream removes a stream from the instance.
func (i *instance) removeStream(s *stream) {
	if i.streams.Delete(s) {
		i.index.Delete(s.labels, s.fp)
		i.streamsRemovedTotal.Inc()
		memoryStreams.WithLabelValues(i.instanceID).Dec()
		streamsCountStats.Add(-1)
	}
}

func (i *instance) getHashForLabels(ls labels.Labels) model.Fingerprint {
	var fp uint64
	fp, i.buf = ls.HashWithoutLabels(i.buf, []string(nil)...)
	return i.mapper.mapFP(model.Fingerprint(fp), ls)
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
	expr, err := req.LogSelector()
	if err != nil {
		return nil, err
	}
	pipeline, err := expr.Pipeline()
	if err != nil {
		return nil, err
	}

	stats := stats.FromContext(ctx)
	var iters []iter.EntryIterator

	shard, err := parseShardFromRequest(req.Shards)
	if err != nil {
		return nil, err
	}

	err = i.forMatchingStreams(
		ctx,
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
	expr, err := req.Expr()
	if err != nil {
		return nil, err
	}
	extractor, err := expr.Extractor()
	if err != nil {
		return nil, err
	}

	stats := stats.FromContext(ctx)
	var iters []iter.SampleIterator

	var shard *astmapper.ShardAnnotation
	shards, err := logql.ParseShards(req.Shards)
	if err != nil {
		return nil, err
	}
	if len(shards) > 1 {
		return nil, errors.New("only one shard per ingester query is supported")
	}
	if len(shards) == 1 {
		shard = &shards[0]
	}

	err = i.forMatchingStreams(
		ctx,
		expr.Selector().Matchers(),
		shard,
		func(stream *stream) error {
			iter, err := stream.SampleIterator(ctx, stats, req.Start, req.End, extractor.ForStream(stream.labels))
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
	if len(matchers) == 0 {
		var labels []string
		if req.Values {
			values, err := i.index.LabelValues(req.Name, nil)
			if err != nil {
				return nil, err
			}
			labels = make([]string, len(values))
			for i := 0; i < len(values); i++ {
				labels[i] = values[i]
			}
			return &logproto.LabelResponse{
				Values: labels,
			}, nil
		}
		names, err := i.index.LabelNames(nil)
		if err != nil {
			return nil, err
		}
		labels = make([]string, len(names))
		for i := 0; i < len(names); i++ {
			labels[i] = names[i]
		}
		return &logproto.LabelResponse{
			Values: labels,
		}, nil
	}

	labels := make([]string, 0)
	err := i.forMatchingStreams(ctx, matchers, nil, func(s *stream) error {
		for _, label := range s.labels {
			if req.Values && label.Name == req.Name {
				labels = append(labels, label.Value)
				continue
			}
			if !req.Values {
				labels = append(labels, label.Name)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &logproto.LabelResponse{
		Values: labels,
	}, nil
}

func (i *instance) Series(ctx context.Context, req *logproto.SeriesRequest) (*logproto.SeriesResponse, error) {
	groups, err := logql.Match(req.GetGroups())
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
		err = i.forMatchingStreams(ctx, nil, shard, func(stream *stream) error {
			// consider the stream only if it overlaps the request time range
			if shouldConsiderStream(stream, req) {
				series = append(series, logproto.SeriesIdentifier{
					Labels: stream.labels.Map(),
				})
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	} else {
		dedupedSeries := make(map[uint64]logproto.SeriesIdentifier)
		for _, matchers := range groups {
			err = i.forMatchingStreams(ctx, matchers, shard, func(stream *stream) error {
				// consider the stream only if it overlaps the request time range
				if shouldConsiderStream(stream, req) {
					// exit early when this stream was added by an earlier group
					key := stream.labels.Hash()
					if _, found := dedupedSeries[key]; found {
						return nil
					}

					dedupedSeries[key] = logproto.SeriesIdentifier{
						Labels: stream.labels.Map(),
					}
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

func (i *instance) numStreams() int {
	return i.streams.Len()
}

// forAllStreams will execute a function for all streams in the instance.
// It uses a function in order to enable generic stream access without accidentally leaking streams under the mutex.
func (i *instance) forAllStreams(ctx context.Context, fn func(*stream) error) error {
	var chunkFilter storage.ChunkFilterer
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
	matchers []*labels.Matcher,
	shards *astmapper.ShardAnnotation,
	fn func(*stream) error,
) error {
	filters, matchers := util.SplitFiltersAndMatchers(matchers)
	ids, err := i.index.Lookup(matchers, shards)
	if err != nil {
		return err
	}
	var chunkFilter storage.ChunkFilterer
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
	if err := i.forMatchingStreams(ctx, t.matchers, nil, func(s *stream) error {
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
		var chunkFilter storage.ChunkFilterer
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

func parseShardFromRequest(reqShards []string) (*astmapper.ShardAnnotation, error) {
	var shard *astmapper.ShardAnnotation
	shards, err := logql.ParseShards(reqShards)
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
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

// QuerierQueryServer is the GRPC server stream we use to send batch of entries.
type QuerierQueryServer interface {
	Context() context.Context
	Send(res *logproto.QueryResponse) error
}

func sendBatches(ctx context.Context, i iter.EntryIterator, queryServer QuerierQueryServer, limit uint32) error {
	stats := stats.FromContext(ctx)
	if limit == 0 {
		// send all batches.
		for !isDone(ctx) {
			batch, size, err := iter.ReadBatch(i, queryBatchSize)
			if err != nil {
				return err
			}
			if len(batch.Streams) == 0 {
				return nil
			}
			stats.AddIngesterBatch(int64(size))
			batch.Stats = stats.Ingester()

			if err := queryServer.Send(batch); err != nil {
				return err
			}

			stats.Reset()

		}
		return nil
	}
	// send until the limit is reached.
	sent := uint32(0)
	for sent < limit && !isDone(queryServer.Context()) {
		batch, batchSize, err := iter.ReadBatch(i, math.MinUint32(queryBatchSize, limit-sent))
		if err != nil {
			return err
		}
		sent += batchSize

		if len(batch.Streams) == 0 {
			return nil
		}

		stats.AddIngesterBatch(int64(batchSize))
		batch.Stats = stats.Ingester()

		if err := queryServer.Send(batch); err != nil {
			return err
		}
		stats.Reset()
	}
	return nil
}

func sendSampleBatches(ctx context.Context, it iter.SampleIterator, queryServer logproto.Querier_QuerySampleServer) error {
	stats := stats.FromContext(ctx)
	for !isDone(ctx) {
		batch, size, err := iter.ReadSampleBatch(it, queryBatchSampleSize)
		if err != nil {
			return err
		}
		if len(batch.Series) == 0 {
			return nil
		}

		stats.AddIngesterBatch(int64(size))
		batch.Stats = stats.Ingester()

		if err := queryServer.Send(batch); err != nil {
			return err
		}

		stats.Reset()

	}
	return nil
}

func shouldConsiderStream(stream *stream, req *logproto.SeriesRequest) bool {
	from, to := stream.Bounds()

	if req.End.UnixNano() > from.UnixNano() && req.Start.UnixNano() <= to.UnixNano() {
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
