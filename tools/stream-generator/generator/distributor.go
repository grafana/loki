package generator

import (
	"context"
	"fmt"

	"github.com/grafana/dskit/user"
	"github.com/grafana/loki/v3/pkg/distributor"
	"github.com/grafana/loki/v3/pkg/logproto"
)

func (s *Generator) sendStreams(ctx context.Context, tenant string, batch []distributor.KeyedStream, errCh chan<- error) {
	batchSize := len(batch)

	userCtx, err := user.InjectIntoGRPCRequest(user.InjectOrgID(ctx, tenant))
	if err != nil {
		errCh <- fmt.Errorf("failed to inject user context (tenant: %s, batch_size: %d): %w", tenant, batchSize, err)
		return
	}

	pushStreams := make([]logproto.Stream, len(batch))
	for i, stream := range batch {
		pushStreams[i] = logproto.Stream{
			Labels:  stream.Stream.Labels,
			Entries: stream.Stream.Entries,
		}
	}

	pushReq := &logproto.PushRequest{
		Streams: pushStreams,
	}

	_, err = s.distributorClient.Push(userCtx, pushReq)
	if err != nil {
		errCh <- fmt.Errorf("failed to push streams to distributor (tenant: %s, batch_size: %d): %w", tenant, batchSize, err)
		return
	}
}
