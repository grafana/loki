package ingester

import (
	"context"
	"sort"
	"sync"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/pkg/helpers"
	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/parser"
	"github.com/grafana/loki/pkg/querier"
)

const queryBatchSize = 128

// Errors returned on Query.
var (
	ErrStreamMissing = errors.New("Stream missing")
)

var (
	streamsCreatedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "ingester_streams_created_total",
		Help:      "The total number of streams created in the ingester.",
	}, []string{"org"})
	streamsRemovedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "ingester_streams_removed_total",
		Help:      "The total number of streams removed by the ingester.",
	}, []string{"org"})
)

func init() {
	prometheus.MustRegister(streamsCreatedTotal)
	prometheus.MustRegister(streamsRemovedTotal)
}

type instance struct {
	streamsMtx sync.Mutex
	streams    map[string]*stream
	index      *invertedIndex

	instanceID string
}

func newInstance(instanceID string) *instance {
	streamsCreatedTotal.WithLabelValues(instanceID).Inc()
	return &instance{
		streams:    map[string]*stream{},
		index:      newInvertedIndex(),
		instanceID: instanceID,
	}
}

func (i *instance) Push(ctx context.Context, req *logproto.PushRequest) error {
	i.streamsMtx.Lock()
	defer i.streamsMtx.Unlock()

	for _, s := range req.Streams {
		labels, err := parser.Labels(s.Labels)
		if err != nil {
			return err
		}

		stream, ok := i.streams[s.Labels]
		if !ok {
			stream = newStream(labels)
			i.index.add(labels, s.Labels)
			i.streams[s.Labels] = stream
		}

		if err := stream.Push(ctx, s.Entries); err != nil {
			return err
		}
	}

	return nil
}

func (i *instance) Query(req *logproto.QueryRequest, queryServer logproto.Querier_QueryServer) error {
	matchers, err := parser.Matchers(req.Query)
	if err != nil {
		return err
	}

	// TODO: lock smell
	i.streamsMtx.Lock()
	ids := i.index.lookup(matchers)
	iterators := make([]iter.EntryIterator, len(ids))
	for j := range ids {
		stream, ok := i.streams[ids[j]]
		if !ok {
			i.streamsMtx.Unlock()
			return ErrStreamMissing
		}
		iterators[j], err = stream.Iterator(req.Start, req.End, req.Direction)
		if err != nil {
			return err
		}
	}
	i.streamsMtx.Unlock()

	iterator := iter.NewHeapIterator(iterators, req.Direction)
	defer helpers.LogError("closing iterator", iterator.Close)

	if req.Regex != "" {
		var err error
		iterator, err = iter.NewRegexpFilter(req.Regex, iterator)
		if err != nil {
			return err
		}
	}

	return sendBatches(iterator, queryServer, req.Limit)
}

func (i *instance) Label(ctx context.Context, req *logproto.LabelRequest) (*logproto.LabelResponse, error) {
	var labels []string
	if req.Values {
		labels = i.index.lookupLabelValues(req.Name)
	} else {
		labels = i.index.labelNames()
	}
	sort.Strings(labels)
	return &logproto.LabelResponse{
		Values: labels,
	}, nil
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
	sent := uint32(0)
	for sent < limit && !isDone(queryServer.Context()) {
		batch, batchSize, err := querier.ReadBatch(i, helpers.MinUint32(queryBatchSize, limit-sent))
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
