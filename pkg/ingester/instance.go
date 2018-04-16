package ingester

import (
	"context"
	"sync"

	"github.com/grafana/logish/pkg/logproto"
)

type instance struct {
	streamsMtx sync.Mutex
	streams    map[string]*stream
}

func newInstance() *instance {
	return &instance{
		streams: map[string]*stream{},
	}
}

func (i *instance) Push(ctx context.Context, req *logproto.WriteRequest) error {
	i.streamsMtx.Lock()
	defer i.streamsMtx.Unlock()

	for _, s := range req.Streams {
		stream, ok := i.streams[s.Labels]
		if !ok {
			stream = newStream()
			i.streams[s.Labels] = stream
		}

		if err := stream.Push(ctx, s.Entries); err != nil {
			return err
		}
	}

	return nil
}
