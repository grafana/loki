package indexgateway

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/tenant"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores"
	"github.com/grafana/loki/pkg/storage/stores/index"
	seriesindex "github.com/grafana/loki/pkg/storage/stores/series/index"
	"github.com/grafana/loki/pkg/storage/stores/shipper/util"
	"github.com/grafana/loki/pkg/util/spanlogger"
)

const (
	maxIndexEntriesPerResponse = 1000
)

type IndexQuerier interface {
	stores.ChunkFetcher
	index.BaseReader
	Stop()
}

type IndexClient interface {
	seriesindex.ReadClient
	Stop()
}

type IndexClientWithRange struct {
	IndexClient
	TableRange config.TableRange
}

type Gateway struct {
	services.Service

	indexQuerier IndexQuerier
	indexClients []IndexClientWithRange

	cfg Config
	log log.Logger

	shipper IndexQuerier
}

// NewIndexGateway instantiates a new Index Gateway and start its services.
//
// In case it is configured to be in ring mode, a Basic Service wrapping the ring client is started.
// Otherwise, it starts an Idle Service that doesn't have lifecycle hooks.
func NewIndexGateway(cfg Config, log log.Logger, registerer prometheus.Registerer, indexQuerier IndexQuerier, indexClients []IndexClientWithRange) (*Gateway, error) {
	g := &Gateway{
		indexQuerier: indexQuerier,
		cfg:          cfg,
		log:          log,
		indexClients: indexClients,
	}

	// query newer periods first
	sort.Slice(g.indexClients, func(i, j int) bool {
		return g.indexClients[i].TableRange.Start > g.indexClients[j].TableRange.Start
	})

	g.Service = services.NewIdleService(nil, func(failureCase error) error {
		g.indexQuerier.Stop()
		for _, indexClient := range g.indexClients {
			indexClient.Stop()
		}
		return nil
	})

	return g, nil
}

func (g *Gateway) QueryIndex(request *logproto.QueryIndexRequest, server logproto.IndexGateway_QueryIndexServer) error {
	log, ctx := spanlogger.New(server.Context(), "IndexGateway.QueryIndex")
	defer log.Finish()

	var outerErr, innerErr error

	queries := make([]seriesindex.Query, 0, len(request.Queries))
	for _, query := range request.Queries {
		if _, err := config.ExtractTableNumberFromName(query.TableName); err != nil {
			level.Error(log).Log("msg", "skip querying table", "table", query.TableName, "err", err)
			continue
		}

		queries = append(queries, seriesindex.Query{
			TableName:        query.TableName,
			HashValue:        query.HashValue,
			RangeValuePrefix: query.RangeValuePrefix,
			RangeValueStart:  query.RangeValueStart,
			ValueEqual:       query.ValueEqual,
		})
	}

	sort.Slice(queries, func(i, j int) bool {
		ta, _ := config.ExtractTableNumberFromName(queries[i].TableName)
		tb, _ := config.ExtractTableNumberFromName(queries[j].TableName)
		return ta < tb
	})

	sendBatchMtx := sync.Mutex{}
	for _, indexClient := range g.indexClients {
		// find queries that can be handled by this index client.
		start := sort.Search(len(queries), func(i int) bool {
			tableNumber, _ := config.ExtractTableNumberFromName(queries[i].TableName)
			return tableNumber >= indexClient.TableRange.Start
		})
		end := sort.Search(len(queries), func(j int) bool {
			tableNumber, _ := config.ExtractTableNumberFromName(queries[j].TableName)
			return tableNumber > indexClient.TableRange.End
		})
		if end-start <= 0 {
			continue
		}

		outerErr = indexClient.QueryPages(ctx, queries[start:end], func(query seriesindex.Query, batch seriesindex.ReadBatchResult) bool {
			innerErr = buildResponses(query, batch, func(response *logproto.QueryIndexResponse) error {
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

		if outerErr != nil {
			return outerErr
		}
	}

	return nil
}

func buildResponses(query seriesindex.Query, batch seriesindex.ReadBatchResult, callback func(*logproto.QueryIndexResponse) error) error {
	itr := batch.Iterator()
	var resp []*logproto.Row

	for itr.Next() {
		if len(resp) == maxIndexEntriesPerResponse {
			err := callback(&logproto.QueryIndexResponse{
				QueryKey: util.QueryKey(query),
				Rows:     resp,
			})
			if err != nil {
				return err
			}
			resp = []*logproto.Row{}
		}

		resp = append(resp, &logproto.Row{
			RangeValue: itr.RangeValue(),
			Value:      itr.Value(),
		})
	}

	if len(resp) != 0 {
		err := callback(&logproto.QueryIndexResponse{
			QueryKey: util.QueryKey(query),
			Rows:     resp,
		})
		if err != nil {
			return err
		}
	}

	return nil
}

func (g *Gateway) GetChunkRef(ctx context.Context, req *logproto.GetChunkRefRequest) (*logproto.GetChunkRefResponse, error) {
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
	result := &logproto.GetChunkRefResponse{
		Refs: make([]*logproto.ChunkRef, 0, len(chunks)),
	}
	for _, cs := range chunks {
		for i := range cs {
			result.Refs = append(result.Refs, &cs[i].ChunkRef)
		}
	}
	return result, nil
}

func (g *Gateway) GetSeries(ctx context.Context, req *logproto.GetSeriesRequest) (*logproto.GetSeriesResponse, error) {
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

	resp := &logproto.GetSeriesResponse{
		Series: make([]logproto.IndexSeries, len(series)),
	}
	for i := range series {
		resp.Series[i] = logproto.IndexSeries{
			Labels: logproto.FromLabelsToLabelAdapters(series[i]),
		}
	}
	return resp, nil
}

func (g *Gateway) LabelNamesForMetricName(ctx context.Context, req *logproto.LabelNamesForMetricNameRequest) (*logproto.LabelResponse, error) {
	instanceID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}
	names, err := g.indexQuerier.LabelNamesForMetricName(ctx, instanceID, req.From, req.Through, req.MetricName)
	if err != nil {
		return nil, err
	}
	return &logproto.LabelResponse{
		Values: names,
	}, nil
}

func (g *Gateway) LabelValuesForMetricName(ctx context.Context, req *logproto.LabelValuesForMetricNameRequest) (*logproto.LabelResponse, error) {
	instanceID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}
	var matchers []*labels.Matcher
	// An empty matchers string cannot be parsed,
	// therefore we check the string representation of the the matchers.
	if req.Matchers != syntax.EmptyMatchers {
		expr, err := syntax.ParseExprWithoutValidation(req.Matchers)
		if err != nil {
			return nil, err
		}

		matcherExpr, ok := expr.(*syntax.MatchersExpr)
		if !ok {
			return nil, fmt.Errorf("invalid label matchers found of type %T", expr)
		}
		matchers = matcherExpr.Mts
	}
	names, err := g.indexQuerier.LabelValuesForMetricName(ctx, instanceID, req.From, req.Through, req.MetricName, req.LabelName, matchers...)
	if err != nil {
		return nil, err
	}
	return &logproto.LabelResponse{
		Values: names,
	}, nil
}

func (g *Gateway) GetStats(ctx context.Context, req *logproto.IndexStatsRequest) (*logproto.IndexStatsResponse, error) {
	instanceID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}
	matchers, err := syntax.ParseMatchers(req.Matchers)
	if err != nil {
		return nil, err
	}

	return g.indexQuerier.Stats(ctx, instanceID, req.From, req.Through, matchers...)
}

type failingIndexClient struct{}

func (f failingIndexClient) QueryPages(ctx context.Context, queries []seriesindex.Query, callback seriesindex.QueryPagesCallback) error {
	return errors.New("index client is not initialized likely due to boltdb-shipper not being used")
}

func (f failingIndexClient) Stop() {}
