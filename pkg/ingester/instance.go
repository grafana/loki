package ingester

import (
	"context"
	"sync"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"

	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/cortexproject/cortex/pkg/ingester/index"

	"github.com/grafana/loki/pkg/helpers"
	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/parser"
	"github.com/grafana/loki/pkg/querier"
	"github.com/grafana/loki/pkg/util"
)

const queryBatchSize = 128

// Errors returned on Query.
var (
	ErrStreamMissing = errors.New("Stream missing")
)

var (
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
}

func newInstance(instanceID string) *instance {
	return &instance{
		streams:    map[model.Fingerprint]*stream{},
		index:      index.New(),
		instanceID: instanceID,

		streamsCreatedTotal: streamsCreatedTotal.WithLabelValues(instanceID),
		streamsRemovedTotal: streamsRemovedTotal.WithLabelValues(instanceID),
	}
}

func (i *instance) Push(ctx context.Context, req *logproto.PushRequest, blockSize int) error {
	i.streamsMtx.Lock()
	defer i.streamsMtx.Unlock()

	var appendErr error
	for _, s := range req.Streams {
		labels, err := util.ToClientLabels(s.Labels)
		if err != nil {
			appendErr = err
			continue
		}

		fp := client.FastFingerprint(labels)
		stream, ok := i.streams[fp]
		if !ok {
			stream = newStream(fp, labels)
			i.index.Add(labels, fp)
			i.streams[fp] = stream
			i.streamsCreatedTotal.Inc()
		}

		if err := stream.Push(ctx, s.Entries, blockSize); err != nil {
			appendErr = err
			continue
		}
	}

	return appendErr
}

func (i *instance) Query(req *logproto.QueryRequest, queryServer logproto.Querier_QueryServer) error {
	matchers, err := parser.Matchers(req.Query)
	if err != nil {
		return err
	}

	iterators, err := i.lookupStreams(req, matchers)
	if err != nil {
		return err
	}

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

func (i *instance) lookupStreams(req *logproto.QueryRequest, matchers []*labels.Matcher) ([]iter.EntryIterator, error) {
	i.streamsMtx.RLock()
	defer i.streamsMtx.RUnlock()

	var err error
	ids := i.index.Lookup(matchers)
	iterators := make([]iter.EntryIterator, len(ids))
	for j := range ids {
		stream, ok := i.streams[ids[j]]
		if !ok {
			return nil, ErrStreamMissing
		}
		iterators[j], err = stream.Iterator(req.Start, req.End, req.Direction)
		if err != nil {
			return nil, err
		}
	}
	return iterators, nil
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
