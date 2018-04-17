package ingester

import (
	"context"
	"sync"

	"github.com/pkg/errors"

	"github.com/grafana/logish/pkg/logproto"
	"github.com/grafana/logish/pkg/parser"
	"github.com/grafana/logish/pkg/querier"
)

var (
	ErrStreamMissing = errors.New("Stream missing")
)

type instance struct {
	streamsMtx sync.Mutex
	streams    map[string]*stream
	index      *invertedIndex
}

func newInstance() *instance {
	return &instance{
		streams: map[string]*stream{},
		index:   newInvertedIndex(),
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
	iterators := make([]querier.EntryIterator, len(ids))
	for j := range ids {
		stream, ok := i.streams[ids[j]]
		if !ok {
			i.streamsMtx.Unlock()
			return ErrStreamMissing
		}
		iterators[j] = stream.Iterator()
	}
	i.streamsMtx.Unlock()

	iterator := querier.NewHeapIterator(iterators)
	defer iterator.Close()

	return querier.SendBatches(iterator, queryServer)
}
