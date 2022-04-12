package indexgateway

import (
	"context"
	"sync"

	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/tenant"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/fetcher"
	"github.com/grafana/loki/pkg/storage/stores/series/index"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexgateway/indexgatewaypb"
	"github.com/grafana/loki/pkg/storage/stores/shipper/util"
)

const maxIndexEntriesPerResponse = 1000

type IndexQuerier interface {
	GetChunkRefs(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([][]chunk.Chunk, []*fetcher.Fetcher, error)
	LabelValuesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string, labelName string, matchers ...*labels.Matcher) ([]string, error)
	LabelNamesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string) ([]string, error)
	Stop()
}

type IndexClient interface {
	QueryPages(ctx context.Context, queries []index.Query, callback index.QueryPagesCallback) error
	Stop()
}

type gateway struct {
	services.Service

	indexQuerier IndexQuerier
	indexClient  IndexClient
}

func NewIndexGateway(indexQuerier IndexQuerier, indexClient IndexClient) *gateway {
	g := &gateway{
		indexQuerier: indexQuerier,
		indexClient:  indexClient,
	}
	g.Service = services.NewIdleService(nil, func(failureCase error) error {
		g.indexQuerier.Stop()
		g.indexClient.Stop()
		return nil
	})
	return g
}

func (g *gateway) QueryIndex(request *indexgatewaypb.QueryIndexRequest, server indexgatewaypb.IndexGateway_QueryIndexServer) error {
	var outerErr error
	var innerErr error

	queries := make([]index.Query, 0, len(request.Queries))
	for _, query := range request.Queries {
		queries = append(queries, index.Query{
			TableName:        query.TableName,
			HashValue:        query.HashValue,
			RangeValuePrefix: query.RangeValuePrefix,
			RangeValueStart:  query.RangeValueStart,
			ValueEqual:       query.ValueEqual,
		})
	}

	sendBatchMtx := sync.Mutex{}
	outerErr = g.indexClient.QueryPages(server.Context(), queries, func(query index.Query, batch index.ReadBatchResult) bool {
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

func buildResponses(query index.Query, batch index.ReadBatchResult, callback func(*indexgatewaypb.QueryIndexResponse) error) error {
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

func (g *gateway) GetChunkRef(ctx context.Context, req *indexgatewaypb.GetChunkRefRequest) (*indexgatewaypb.GetChunkRefResponse, error) {
	instanceID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}
	matchers, err := syntax.ParseMatchers(req.Matchers)
	if err != nil {
		return nil, err
	}
	chunks, _, err := g.indexQuerier.GetChunkRefs(ctx, instanceID, req.From, req.Through, matchers...)
	if err != nil {
		return nil, err
	}
	result := &indexgatewaypb.GetChunkRefResponse{
		Refs: make([]*logproto.ChunkRef, 0, len(chunks)),
	}
	for _, cs := range chunks {
		for _, c := range cs {
			result.Refs = append(result.Refs, &c.ChunkRef)
		}
	}
	return result, nil
}

func (g *gateway) LabelNamesForMetricName(ctx context.Context, req *indexgatewaypb.LabelNamesForMetricNameRequest) (*indexgatewaypb.LabelResponse, error) {
	instanceID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}
	names, err := g.indexQuerier.LabelNamesForMetricName(ctx, instanceID, req.From, req.From, req.MetricName)
	if err != nil {
		return nil, err
	}
	return &indexgatewaypb.LabelResponse{
		Values: names,
	}, nil
}

func (g *gateway) LabelValuesForMetricName(ctx context.Context, req *indexgatewaypb.LabelValuesForMetricNameRequest) (*indexgatewaypb.LabelResponse, error) {
	instanceID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}
	matchers, err := syntax.ParseMatchers(req.Matchers)
	if err != nil {
		return nil, err
	}
	names, err := g.indexQuerier.LabelValuesForMetricName(ctx, instanceID, req.From, req.From, req.MetricName, req.LabelName, matchers...)
	if err != nil {
		return nil, err
	}
	return &indexgatewaypb.LabelResponse{
		Values: names,
	}, nil
}
