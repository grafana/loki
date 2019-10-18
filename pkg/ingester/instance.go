package ingester

import (
	"context"
	"net/http"
	"sync"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/weaveworks/common/httpgrpc"

	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/cortexproject/cortex/pkg/ingester/index"
	cutil "github.com/cortexproject/cortex/pkg/util"

	"github.com/grafana/loki/pkg/helpers"
	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/util"
	"github.com/grafana/loki/pkg/util/validation"
)

const queryBatchSize = 128

// Errors returned on Query.
var (
	ErrStreamMissing = errors.New("Stream missing")
)

var (
	memoryStreams = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "loki_ingester_memory_streams",
		Help: "The total number of streams in memory.",
	})
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
)

type instance struct {
	streamsMtx sync.RWMutex
	streams    map[model.Fingerprint]*stream
	index      *index.InvertedIndex

	instanceID string

	streamsCreatedTotal prometheus.Counter
	streamsRemovedTotal prometheus.Counter

	blockSize int
	tailers   map[uint32]*tailer
	tailerMtx sync.RWMutex

	limits *validation.Overrides
}

func newInstance(instanceID string, blockSize int, limits *validation.Overrides) *instance {
	return &instance{
		streams:    map[model.Fingerprint]*stream{},
		index:      index.New(),
		instanceID: instanceID,

		streamsCreatedTotal: streamsCreatedTotal.WithLabelValues(instanceID),
		streamsRemovedTotal: streamsRemovedTotal.WithLabelValues(instanceID),

		blockSize: blockSize,
		tailers:   map[uint32]*tailer{},
		limits:    limits,
	}
}

// consumeChunk manually adds a chunk that was received during ingester chunk
// transfer.
func (i *instance) consumeChunk(ctx context.Context, labels []client.LabelAdapter, chunk *logproto.Chunk) error {
	i.streamsMtx.Lock()
	defer i.streamsMtx.Unlock()

	fp := client.FastFingerprint(labels)
	stream, ok := i.streams[fp]
	if !ok {
		stream = newStream(fp, labels, i.blockSize)
		i.index.Add(labels, fp)
		i.streams[fp] = stream
		i.streamsCreatedTotal.Inc()
		memoryStreams.Inc()
		i.addTailersToNewStream(stream)
	}

	err := stream.consumeChunk(ctx, chunk)
	if err == nil {
		memoryChunks.Inc()
	}

	return err
}

func (i *instance) Push(ctx context.Context, req *logproto.PushRequest) error {
	i.streamsMtx.Lock()
	defer i.streamsMtx.Unlock()

	var appendErr error
	for _, s := range req.Streams {
		labels, err := util.ToClientLabels(s.Labels)
		if err != nil {
			appendErr = err
			continue
		}

		stream, err := i.getOrCreateStream(labels)
		if err != nil {
			appendErr = err
			continue
		}

		prevNumChunks := len(stream.chunks)
		if err := stream.Push(ctx, s.Entries); err != nil {
			appendErr = err
			continue
		}

		memoryChunks.Add(float64(len(stream.chunks) - prevNumChunks))
	}

	return appendErr
}

func (i *instance) getOrCreateStream(labels []client.LabelAdapter) (*stream, error) {
	fp := client.FastFingerprint(labels)

	stream, ok := i.streams[fp]
	if ok {
		return stream, nil
	}

	if len(i.streams) >= i.limits.MaxStreamsPerUser(i.instanceID) {
		return nil, httpgrpc.Errorf(http.StatusTooManyRequests, "per-user streams limit (%d) exceeded", i.limits.MaxStreamsPerUser(i.instanceID))
	}
	stream = newStream(fp, labels, i.blockSize)
	i.index.Add(labels, fp)
	i.streams[fp] = stream
	memoryStreams.Inc()
	i.streamsCreatedTotal.Inc()
	i.addTailersToNewStream(stream)

	return stream, nil
}

func (i *instance) Query(req *logproto.QueryRequest, queryServer logproto.Querier_QueryServer) error {
	expr, err := (logql.SelectParams{QueryRequest: req}).LogSelector()
	if err != nil {
		return err
	}
	filter, err := expr.Filter()
	if err != nil {
		return err
	}
	iters, err := i.lookupStreams(req, expr.Matchers(), filter)
	if err != nil {
		return err
	}

	iter := iter.NewHeapIterator(iters, req.Direction)
	defer helpers.LogError("closing iterator", iter.Close)

	return sendBatches(iter, queryServer, req.Limit)
}

func (i *instance) Label(_ context.Context, req *logproto.LabelRequest) (*logproto.LabelResponse, error) {
	var labels []string
	if req.Values {
		values := i.index.LabelValues(req.Name)
		labels = make([]string, len(values))
		for i := 0; i < len(values); i++ {
			labels[i] = values[i]
		}
	} else {
		names := i.index.LabelNames()
		labels = make([]string, len(names))
		for i := 0; i < len(names); i++ {
			labels[i] = names[i]
		}
	}
	return &logproto.LabelResponse{
		Values: labels,
	}, nil
}

func (i *instance) lookupStreams(req *logproto.QueryRequest, matchers []*labels.Matcher, filter logql.Filter) ([]iter.EntryIterator, error) {
	i.streamsMtx.RLock()
	defer i.streamsMtx.RUnlock()

	filters, matchers := cutil.SplitFiltersAndMatchers(matchers)
	ids := i.index.Lookup(matchers)
	iterators := make([]iter.EntryIterator, 0, len(ids))

outer:
	for _, streamID := range ids {
		stream, ok := i.streams[streamID]
		if !ok {
			return nil, ErrStreamMissing
		}
		lbs := client.FromLabelAdaptersToLabels(stream.labels)
		for _, filter := range filters {
			if !filter.Matches(lbs.Get(filter.Name)) {
				continue outer
			}
		}
		iter, err := stream.Iterator(req.Start, req.End, req.Direction, filter)
		if err != nil {
			return nil, err
		}
		iterators = append(iterators, iter)
	}
	return iterators, nil
}

func (i *instance) addNewTailer(t *tailer) {
	i.streamsMtx.RLock()
	for _, stream := range i.streams {
		if stream.matchesTailer(t) {
			stream.addTailer(t)
		}
	}
	i.streamsMtx.RUnlock()

	i.tailerMtx.Lock()
	defer i.tailerMtx.Unlock()
	i.tailers[t.getID()] = t
}

func (i *instance) addTailersToNewStream(stream *stream) {
	closedTailers := []uint32{}

	i.tailerMtx.RLock()
	for _, t := range i.tailers {
		if t.isClosed() {
			closedTailers = append(closedTailers, t.getID())
			continue
		}

		if stream.matchesTailer(t) {
			stream.addTailer(t)
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

func isDone(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

func sendBatches(i iter.EntryIterator, queryServer logproto.Querier_QueryServer, limit uint32) error {
	if limit == 0 {
		return sendAllBatches(i, queryServer)
	}
	sent := uint32(0)
	for sent < limit && !isDone(queryServer.Context()) {
		batch, batchSize, err := iter.ReadBatch(i, helpers.MinUint32(queryBatchSize, limit-sent))
		if err != nil {
			return err
		}
		sent += batchSize

		if len(batch.Streams) == 0 {
			return nil
		}

		if err := queryServer.Send(batch); err != nil {
			return err
		}
	}
	return nil
}

func sendAllBatches(i iter.EntryIterator, queryServer logproto.Querier_QueryServer) error {
	for !isDone(queryServer.Context()) {
		batch, _, err := iter.ReadBatch(i, queryBatchSize)
		if err != nil {
			return err
		}
		if len(batch.Streams) == 0 {
			return nil
		}

		if err := queryServer.Send(batch); err != nil {
			return err
		}
	}
	return nil
}
