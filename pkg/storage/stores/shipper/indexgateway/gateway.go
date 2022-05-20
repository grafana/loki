package indexgateway

import (
	"context"
	"sync"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/tenant"
	"github.com/prometheus/client_golang/prometheus"
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

const (
	maxIndexEntriesPerResponse = 1000
)

type IndexQuerier interface {
	GetChunkRefs(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([][]chunk.Chunk, []*fetcher.Fetcher, error)
	GetSeries(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]labels.Labels, error)
	LabelValuesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string, labelName string, matchers ...*labels.Matcher) ([]string, error)
	LabelNamesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string) ([]string, error)
	Stop()
}

type IndexClient interface {
	QueryPages(ctx context.Context, queries []index.Query, callback index.QueryPagesCallback) error
	Stop()
}

type Gateway struct {
	services.Service

	indexQuerier IndexQuerier
	indexClient  IndexClient

	cfg Config
	log log.Logger

	shipper IndexQuerier
}

// NewIndexGateway instantiates a new Index Gateway and start its services.
//
// In case it is configured to be in ring mode, a Basic Service wrapping the ring client is started.
// Otherwise, it starts an Idle Service that doesn't have lifecycle hooks.
func NewIndexGateway(cfg Config, log log.Logger, registerer prometheus.Registerer, indexQuerier IndexQuerier, indexClient IndexClient) (*Gateway, error) {
	g := &Gateway{
		indexQuerier: indexQuerier,
		cfg:          cfg,
		log:          log,
		indexClient:  indexClient,
	}

	g.Service = services.NewIdleService(nil, func(failureCase error) error {
		g.indexQuerier.Stop()
		g.indexClient.Stop()
		return nil
	})

	return g, nil
}

func (g *Gateway) QueryIndex(request *indexgatewaypb.QueryIndexRequest, server indexgatewaypb.IndexGateway_QueryIndexServer) error {
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

func (g *Gateway) GetChunkRef(ctx context.Context, req *indexgatewaypb.GetChunkRefRequest) (*indexgatewaypb.GetChunkRefResponse, error) {
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
		for i := range cs {
			result.Refs = append(result.Refs, &cs[i].ChunkRef)
		}
	}
	return result, nil
}

func (g *Gateway) GetSeries(ctx context.Context, req *indexgatewaypb.GetSeriesRequest) (*indexgatewaypb.GetSeriesResponse, error) {
	instanceID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

	matchers, err := syntax.ParseMatchers(req.Matchers)
	if err != nil {
		return nil, err
	}
	series, err := g.indexQuerier.GetSeries(ctx, instanceID, req.From, req.Through, matchers...)
	if err != nil {
		return nil, err
	}

	resp := &indexgatewaypb.GetSeriesResponse{
		Series: make([]indexgatewaypb.Series, len(series)),
	}
	for i := range series {
		resp.Series[i] = indexgatewaypb.Series{
			Labels: logproto.FromLabelsToLabelAdapters(series[i]),
		}
	}
	return resp, nil
}

func (g *Gateway) LabelNamesForMetricName(ctx context.Context, req *indexgatewaypb.LabelNamesForMetricNameRequest) (*indexgatewaypb.LabelResponse, error) {
	instanceID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}
	names, err := g.indexQuerier.LabelNamesForMetricName(ctx, instanceID, req.From, req.Through, req.MetricName)
	if err != nil {
		return nil, err
	}
	return &indexgatewaypb.LabelResponse{
		Values: names,
	}, nil
}

func (g *Gateway) LabelValuesForMetricName(ctx context.Context, req *indexgatewaypb.LabelValuesForMetricNameRequest) (*indexgatewaypb.LabelResponse, error) {
	instanceID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}
	var matchers []*labels.Matcher
	// An empty matchers string cannot be parsed,
	// therefore we check the string representation of the the matchers.
	if req.Matchers != syntax.EmptyMatchers {
		matchers, err = syntax.ParseMatchers(req.Matchers)
		if err != nil {
			return nil, err
		}
	}
	names, err := g.indexQuerier.LabelValuesForMetricName(ctx, instanceID, req.From, req.Through, req.MetricName, req.LabelName, matchers...)
	if err != nil {
		return nil, err
	}
	return &indexgatewaypb.LabelResponse{
		Values: names,
	}, nil
}
