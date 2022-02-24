package indexgateway

import (
	"context"
	"sync"

	"github.com/grafana/dskit/services"

	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexgateway/indexgatewaypb"
	"github.com/grafana/loki/pkg/storage/stores/shipper/util"
)

const maxIndexEntriesPerResponse = 1000

type IndexQuerier interface {
	QueryPages(ctx context.Context, queries []chunk.IndexQuery, callback chunk.QueryPagesCallback) error
	Stop()
}

type gateway struct {
	services.Service

	indexQuerier IndexQuerier
}

func NewIndexGateway(indexQuerier IndexQuerier) *gateway {
	g := &gateway{
		indexQuerier: indexQuerier,
	}
	g.Service = services.NewIdleService(nil, func(failureCase error) error {
		g.indexQuerier.Stop()
		return nil
	})
	return g
}

func (g *gateway) QueryIndex(request *indexgatewaypb.QueryIndexRequest, server indexgatewaypb.IndexGateway_QueryIndexServer) error {
	var outerErr error
	var innerErr error

	queries := make([]chunk.IndexQuery, 0, len(request.Queries))
	for _, query := range request.Queries {
		queries = append(queries, chunk.IndexQuery{
			TableName:        query.TableName,
			HashValue:        query.HashValue,
			RangeValuePrefix: query.RangeValuePrefix,
			RangeValueStart:  query.RangeValueStart,
			ValueEqual:       query.ValueEqual,
		})
	}

	sendBatchMtx := sync.Mutex{}
	outerErr = g.indexQuerier.QueryPages(server.Context(), queries, func(query chunk.IndexQuery, batch chunk.ReadBatch) bool {
		innerErr = buildResponses(query, batch, func(response *indexgatewaypb.QueryIndexResponse) error {
			// do not send grpc responses concurrently. See https://github.com/grpc/grpc-go/blob/master/stream.go#L120-L123.
			sendBatchMtx.Lock()
			defer sendBatchMtx.Unlock()

			return server.Send(response)
		})

		if innerErr != nil {
			return false
		}

		return true
	})

	if innerErr != nil {
		return innerErr
	}

	return outerErr
}

func buildResponses(query chunk.IndexQuery, batch chunk.ReadBatch, callback func(*indexgatewaypb.QueryIndexResponse) error) error {
	itr := batch.Iterator()
	var resp []*indexgatewaypb.Row

	for itr.Next() {
		if len(resp) == maxIndexEntriesPerResponse {
			err := callback(&indexgatewaypb.QueryIndexResponse{
				QueryKey: util.QueryKey(query),
				Rows:     resp,
			})
			if err != nil {
				return err
			}
			resp = []*indexgatewaypb.Row{}
		}

		resp = append(resp, &indexgatewaypb.Row{
			RangeValue: itr.RangeValue(),
			Value:      itr.Value(),
		})
	}

	if len(resp) != 0 {
		err := callback(&indexgatewaypb.QueryIndexResponse{
			QueryKey: util.QueryKey(query),
			Rows:     resp,
		})
		if err != nil {
			return err
		}
	}

	return nil
}
