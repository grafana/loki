package indexgateway

import (
	"github.com/cortexproject/cortex/pkg/util/services"

	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/stores/shipper"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexgateway/indexgatewaypb"
	"github.com/grafana/loki/pkg/storage/stores/shipper/util"
)

const maxIndexEntriesPerResponse = 1000

type gateway struct {
	services.Service

	shipper chunk.IndexClient
}

func NewIndexGateway(shipperIndexClient *shipper.Shipper) *gateway {
	g := &gateway{
		shipper: shipperIndexClient,
	}
	g.Service = services.NewIdleService(nil, func(failureCase error) error {
		g.shipper.Stop()
		return nil
	})
	return g
}

func (g gateway) QueryIndex(request *indexgatewaypb.QueryIndexRequest, server indexgatewaypb.IndexGateway_QueryIndexServer) error {
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
	outerErr = g.shipper.QueryPages(server.Context(), queries, func(query chunk.IndexQuery, batch chunk.ReadBatch) bool {
		innerErr = g.sendBatch(server, query, batch)
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

func (g *gateway) sendBatch(server indexgatewaypb.IndexGateway_QueryIndexServer, query chunk.IndexQuery, batch chunk.ReadBatch) error {
	itr := batch.Iterator()
	var resp []*indexgatewaypb.Row

	for itr.Next() {
		if len(resp) == maxIndexEntriesPerResponse {
			err := server.Send(&indexgatewaypb.QueryIndexResponse{
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
		err := server.Send(&indexgatewaypb.QueryIndexResponse{
			QueryKey: util.QueryKey(query),
			Rows:     resp,
		})
		if err != nil {
			return err
		}
	}

	return nil
}
