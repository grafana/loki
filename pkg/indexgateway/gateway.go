package indexgateway

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/c2h5oh/datasize"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/tenant"
	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	iter "github.com/grafana/loki/v3/pkg/iter/v2"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/plan"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores"
	"github.com/grafana/loki/v3/pkg/storage/stores/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/seriesvolume"
	seriesindex "github.com/grafana/loki/v3/pkg/storage/stores/series/index"
	tsdb_index "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/sharding"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/util/spanlogger"
)

const (
	maxIndexEntriesPerResponse = 1000
)

type IndexQuerier interface {
	stores.ChunkFetcher
	index.BaseReader
	index.StatsReader
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

type BloomQuerier interface {
	FilterChunkRefs(ctx context.Context, tenant string, from, through model.Time, series map[uint64]labels.Labels, chunks []*logproto.ChunkRef, plan plan.QueryPlan) ([]*logproto.ChunkRef, bool, error)
}

type Gateway struct {
	services.Service

	indexQuerier IndexQuerier
	indexClients []IndexClientWithRange
	bloomQuerier BloomQuerier
	metrics      *Metrics

	cfg    Config
	limits Limits
	log    log.Logger
}

// NewIndexGateway instantiates a new Index Gateway and start its services.
//
// In case it is configured to be in ring mode, a Basic Service wrapping the ring client is started.
// Otherwise, it starts an Idle Service that doesn't have lifecycle hooks.
func NewIndexGateway(cfg Config, limits Limits, log log.Logger, r prometheus.Registerer, indexQuerier IndexQuerier, indexClients []IndexClientWithRange, bloomQuerier BloomQuerier) (*Gateway, error) {
	g := &Gateway{
		indexQuerier: indexQuerier,
		bloomQuerier: bloomQuerier,
		cfg:          cfg,
		limits:       limits,
		log:          log,
		indexClients: indexClients,
		metrics:      NewMetrics(r),
	}

	// query newer periods first
	sort.Slice(g.indexClients, func(i, j int) bool {
		return g.indexClients[i].TableRange.Start > g.indexClients[j].TableRange.Start
	})

	g.Service = services.NewIdleService(nil, func(_ error) error {
		g.indexQuerier.Stop()
		for _, indexClient := range g.indexClients {
			indexClient.Stop()
		}
		return nil
	})

	return g, nil
}

func (g *Gateway) QueryIndex(request *logproto.QueryIndexRequest, server logproto.IndexGateway_QueryIndexServer) error {
	log, _ := spanlogger.New(context.Background(), "IndexGateway.QueryIndex")
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

		outerErr = indexClient.QueryPages(server.Context(), queries[start:end], func(query seriesindex.Query, batch seriesindex.ReadBatchResult) bool {
			innerErr = buildResponses(query, batch, func(response *logproto.QueryIndexResponse) error {
				// do not send grpc responses concurrently. See https://github.com/grpc/grpc-go/blob/master/stream.go#L120-L123.
				sendBatchMtx.Lock()
				defer sendBatchMtx.Unlock()

				return server.Send(response)
			})

			return innerErr == nil
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
				QueryKey: seriesindex.QueryKey(query),
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
			QueryKey: seriesindex.QueryKey(query),
			Rows:     resp,
		})
		if err != nil {
			return err
		}
	}

	return nil
}

func (g *Gateway) GetChunkRef(ctx context.Context, req *logproto.GetChunkRefRequest) (result *logproto.GetChunkRefResponse, err error) {
	logger := util_log.WithContext(ctx, g.log)
	sp, ctx := opentracing.StartSpanFromContext(ctx, "indexgateway.GetChunkRef")
	defer sp.Finish()

	instanceID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}
	matchers, err := syntax.ParseMatchers(req.Matchers, true)
	if err != nil {
		return nil, err
	}

	predicate := chunk.NewPredicate(matchers, &req.Plan)
	chunks, _, err := g.indexQuerier.GetChunks(ctx, instanceID, req.From, req.Through, predicate, nil)
	if err != nil {
		return nil, err
	}

	result = &logproto.GetChunkRefResponse{
		Refs: make([]*logproto.ChunkRef, 0, len(chunks)),
	}
	for _, cs := range chunks {
		for i := range cs {
			result.Refs = append(result.Refs, &cs[i].ChunkRef)
		}
	}

	initialChunkCount := len(result.Refs)
	result.Stats.TotalChunks = int64(initialChunkCount)
	result.Stats.PostFilterChunks = int64(initialChunkCount) // populate early for error reponses

	defer func() {
		if err == nil {
			g.metrics.preFilterChunks.WithLabelValues(routeChunkRefs).Observe(float64(initialChunkCount))
			g.metrics.postFilterChunks.WithLabelValues(routeChunkRefs).Observe(float64(len(result.Refs)))
		}
	}()

	// Return unfiltered results if there is no bloom querier (Bloom Gateway disabled)
	if g.bloomQuerier == nil {
		return result, nil
	}

	// Extract testable LabelFilters from the plan. If there is none, we can
	// short-circuit and return before making a req to the bloom-gateway (through
	// the g.bloomQuerier)
	if len(v1.ExtractTestableLabelMatchers(req.Plan.AST)) == 0 {
		return result, nil
	}

	// Doing a "duplicate" index lookup is not ideal,
	// however, modifying the GetChunkRef() response, which contains the logproto.ChunkRef is neither.
	start := time.Now()
	series, err := g.indexQuerier.GetSeries(ctx, instanceID, req.From, req.Through, matchers...)
	seriesMap := make(map[uint64]labels.Labels, len(series))
	for _, s := range series {
		seriesMap[s.Hash()] = s
	}
	sp.LogKV("msg", "indexQuerier.GetSeries", "duration", time.Since(start), "count", len(series))

	start = time.Now()
	chunkRefs, used, err := g.bloomQuerier.FilterChunkRefs(ctx, instanceID, req.From, req.Through, seriesMap, result.Refs, req.Plan)
	if err != nil {
		return nil, err
	}
	sp.LogKV("msg", "bloomQuerier.FilterChunkRefs", "duration", time.Since(start))

	result.Refs = chunkRefs
	level.Info(logger).Log("msg", "return filtered chunk refs", "unfiltered", initialChunkCount, "filtered", len(result.Refs), "used_blooms", used)
	result.Stats.PostFilterChunks = int64(len(result.Refs))
	result.Stats.UsedBloomFilters = used
	return result, nil
}

func (g *Gateway) GetSeries(ctx context.Context, req *logproto.GetSeriesRequest) (*logproto.GetSeriesResponse, error) {
	instanceID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

	matchers, err := syntax.ParseMatchers(req.Matchers, true)
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
	var matchers []*labels.Matcher
	// An empty matchers string cannot be parsed,
	// therefore we check the string representation of the matchers.
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
	names, err := g.indexQuerier.LabelNamesForMetricName(ctx, instanceID, req.From, req.Through, req.MetricName, matchers...)
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
	// therefore we check the string representation of the matchers.
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
	matchers, err := syntax.ParseMatchers(req.Matchers, true)
	if err != nil {
		return nil, err
	}

	return g.indexQuerier.Stats(ctx, instanceID, req.From, req.Through, matchers...)
}

func (g *Gateway) GetVolume(ctx context.Context, req *logproto.VolumeRequest) (*logproto.VolumeResponse, error) {
	instanceID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

	matchers, err := syntax.ParseMatchers(req.Matchers, true)
	if err != nil && req.Matchers != seriesvolume.MatchAny {
		return nil, err
	}

	return g.indexQuerier.Volume(ctx, instanceID, req.From, req.Through, req.GetLimit(), req.TargetLabels, req.AggregateBy, matchers...)
}

func (g *Gateway) GetShards(request *logproto.ShardsRequest, server logproto.IndexGateway_GetShardsServer) error {
	ctx := server.Context()
	sp, ctx := opentracing.StartSpanFromContext(ctx, "indexgateway.GetShards")
	defer sp.Finish()

	instanceID, err := tenant.TenantID(ctx)
	if err != nil {
		return err
	}

	p, err := ExtractShardRequestMatchersAndAST(request.Query)
	if err != nil {
		return err
	}

	forSeries, ok := g.indexQuerier.HasForSeries(request.From, request.Through)
	if !ok {
		sp.LogKV(
			"msg", "index does not support forSeries",
			"action", "falling back to indexQuerier.GetShards impl",
		)
		shards, err := g.indexQuerier.GetShards(
			ctx,
			instanceID,
			request.From, request.Through,
			request.TargetBytesPerShard,
			p,
		)

		if err != nil {
			return err
		}

		return server.Send(shards)
	}

	return g.boundedShards(ctx, request, server, instanceID, p, forSeries)
}

// boundedShards handles bounded shard requests, optionally returning precomputed chunks.
func (g *Gateway) boundedShards(
	ctx context.Context,
	req *logproto.ShardsRequest,
	server logproto.IndexGateway_GetShardsServer,
	instanceID string,
	p chunk.Predicate,
	forSeries sharding.ForSeries,
) error {
	// TODO(owen-d): instead of using GetChunks which buffers _all_ the chunks
	// (expensive when looking at the full fingerprint space), we should
	// use the `ForSeries` implementation to accumulate batches of chunks to dedupe,
	// but I'm leaving this as a future improvement. This may be difficult considering
	// fingerprints aren't necessarily iterated in order because multiple underlying TSDBs
	// can be queried independently. This could also result in the same chunks being present in
	// multiple batches. However, this is all OK because we can dedupe them post-blooms and in
	// many cases the majority of chunks will only be present in a single post-compacted TSDB,
	// making this more of an edge case than a common occurrence (make sure to check this assumption
	// as getting it _very_ wrong could harm some cache locality benefits on the bloom-gws by
	// sending multiple requests to the entire keyspace).

	logger := util_log.WithContext(ctx, g.log)
	sp, ctx := opentracing.StartSpanFromContext(ctx, "indexgateway.boundedShards")
	defer sp.Finish()

	// 1) for all bounds, get chunk refs
	grps, _, err := g.indexQuerier.GetChunks(ctx, instanceID, req.From, req.Through, p, nil)
	if err != nil {
		return err
	}

	var ct int
	for _, g := range grps {
		ct += len(g)
	}

	sp.LogKV(
		"stage", "queried local index",
		"index_chunks_resolved", ct,
	)
	// TODO(owen-d): pool
	refs := make([]*logproto.ChunkRef, 0, ct)

	for _, cs := range grps {
		for j := range cs {
			refs = append(refs, &cs[j].ChunkRef)
		}
	}

	filtered := refs

	// 2) filter via blooms if enabled
	filters := v1.ExtractTestableLabelMatchers(p.Plan().AST)
	// NOTE(chaudum): Temporarily disable bloom filtering of chunk refs,
	// as this doubles the load on bloom gateways.
	// if g.bloomQuerier != nil && len(filters) > 0 {
	// 	xs, err := g.bloomQuerier.FilterChunkRefs(ctx, instanceID, req.From, req.Through, refs, p.Plan())
	// 	if err != nil {
	// 		level.Error(logger).Log("msg", "failed to filter chunk refs", "err", err)
	// 	} else {
	// 		filtered = xs
	// 	}
	// 	sp.LogKV(
	// 		"stage", "queried bloom gateway",
	// 		"err", err,
	// 	)
	// }

	g.metrics.preFilterChunks.WithLabelValues(routeShards).Observe(float64(ct))
	g.metrics.postFilterChunks.WithLabelValues(routeShards).Observe(float64(len(filtered)))

	resp := &logproto.ShardsResponse{}

	// Edge case: if there are no chunks after filtering, we still need to return a single shard
	if len(filtered) == 0 {
		resp.Shards = []logproto.Shard{
			{
				Bounds: logproto.FPBounds{Min: 0, Max: math.MaxUint64},
				Stats:  &logproto.IndexStatsResponse{},
			},
		}

	} else {
		shards, chunkGrps, err := accumulateChunksToShards(ctx, instanceID, forSeries, req, p, filtered)
		if err != nil {
			return err
		}
		resp.Shards = shards

		// If the index gateway is configured to precompute chunks, we can return the chunk groups
		// alongside the shards, otherwise discarding them
		if g.limits.TSDBPrecomputeChunks(instanceID) {
			resp.ChunkGroups = chunkGrps
		}
	}

	sp.LogKV("msg", "send shards response", "shards", len(resp.Shards))

	var refCt int
	for _, grp := range resp.ChunkGroups {
		refCt += len(grp.Refs)
	}

	ms := syntax.MatchersExpr{Mts: p.Matchers}
	level.Debug(logger).Log(
		"msg", "send shards response",
		"total_chunks", ct,
		"post_filter_chunks", len(filtered),
		"shards", len(resp.Shards),
		"query", req.Query,
		"target_bytes_per_shard", datasize.ByteSize(req.TargetBytesPerShard).HumanReadable(),
		"precomputed_refs", refCt,
		"matchers", ms.String(),
		"from", req.From.Time().String(),
		"through", req.Through.Time().String(),
		"length", req.Through.Time().Sub(req.From.Time()).String(),
		"end_delta", time.Since(req.Through.Time()).String(),
		"filters", len(filters),
	)

	// 3) build shards
	return server.Send(resp)
}

// ExtractShardRequestMatchersAndAST extracts the matchers and AST from a query string.
// It errors if there is more than one matcher group in the AST as this is supposed to be
// split out during query planning before reaching this point.
func ExtractShardRequestMatchersAndAST(query string) (chunk.Predicate, error) {
	expr, err := syntax.ParseExpr(query)
	if err != nil {
		return chunk.Predicate{}, err
	}

	ms, err := syntax.MatcherGroups(expr)
	if err != nil {
		return chunk.Predicate{}, err
	}

	var matchers []*labels.Matcher
	switch len(ms) {
	case 0:
		// nothing to do
	case 1:
		matchers = ms[0].Matchers
	default:
		return chunk.Predicate{}, fmt.Errorf(
			"multiple matcher groups are not supported in GetShards. This is likely an internal bug as binary operations should be dispatched separately in planning",
		)
	}

	return chunk.NewPredicate(matchers, &plan.QueryPlan{
		AST: expr,
	}), nil
}

// TODO(owen-d): consider extending index impl to support returning chunkrefs _with_ sizing info
// TODO(owen-d): perf, this is expensive :(
func accumulateChunksToShards(
	ctx context.Context,
	user string,
	forSeries sharding.ForSeries,
	req *logproto.ShardsRequest,
	p chunk.Predicate,
	filtered []*logproto.ChunkRef,
) ([]logproto.Shard, []logproto.ChunkRefGroup, error) {
	// map for looking up post-filtered chunks in O(n) while iterating the index again for sizing info
	filteredM := make(map[model.Fingerprint][]refWithSizingInfo, 1024)
	for _, ref := range filtered {
		x := refWithSizingInfo{ref: ref}
		filteredM[model.Fingerprint(ref.Fingerprint)] = append(filteredM[model.Fingerprint(ref.Fingerprint)], x)
	}

	var mtx sync.Mutex

	if err := forSeries.ForSeries(
		ctx,
		user,
		v1.NewBounds(filtered[0].FingerprintModel(), filtered[len(filtered)-1].FingerprintModel()),
		req.From, req.Through,
		func(l labels.Labels, fp model.Fingerprint, chks []tsdb_index.ChunkMeta) (stop bool) {
			mtx.Lock()
			defer mtx.Unlock()

			// check if this is a fingerprint we need
			if _, ok := filteredM[fp]; !ok {
				return false
			}

			filteredChks := filteredM[fp]
			var j int

		outer:
			for i := range filteredChks {
				for j < len(chks) {
					switch filteredChks[i].Cmp(chks[j]) {
					case iter.Less:
						// this chunk is not in the queried index, continue checking other chunks
						continue outer
					case iter.Greater:
						// next chunk in index but didn't pass filter; continue
						j++
						continue
					case iter.Eq:
						// a match; set the sizing info
						filteredChks[i].KB = chks[j].KB
						filteredChks[i].Entries = chks[j].Entries
						j++
						continue outer
					}
				}

				// we've finished this index's chunks; no need to keep checking filtered chunks
				break
			}

			return false
		},
		p.Matchers...,
	); err != nil {
		return nil, nil, err
	}

	collectedSeries := sharding.SizedFPs(sharding.SizedFPsPool.Get(len(filteredM)))
	defer sharding.SizedFPsPool.Put(collectedSeries)

	for fp, chks := range filteredM {
		x := sharding.SizedFP{Fp: fp}
		x.Stats.Chunks = uint64(len(chks))

		for _, chk := range chks {
			x.Stats.Entries += uint64(chk.Entries)
			x.Stats.Bytes += uint64(chk.KB << 10)
		}
		collectedSeries = append(collectedSeries, x)
	}
	sort.Sort(collectedSeries)

	shards := collectedSeries.ShardsFor(req.TargetBytesPerShard)
	chkGrps := make([]logproto.ChunkRefGroup, 0, len(shards))
	for _, s := range shards {
		from := sort.Search(len(filtered), func(i int) bool {
			return filtered[i].Fingerprint >= uint64(s.Bounds.Min)
		})
		through := sort.Search(len(filtered), func(i int) bool {
			return filtered[i].Fingerprint > uint64(s.Bounds.Max)
		})
		chkGrps = append(chkGrps, logproto.ChunkRefGroup{
			Refs: filtered[from:through],
		})
	}

	return shards, chkGrps, nil
}

type refWithSizingInfo struct {
	ref     *logproto.ChunkRef
	KB      uint32
	Entries uint32
}

// careful: only checks from,through,checksum
func (r refWithSizingInfo) Cmp(chk tsdb_index.ChunkMeta) iter.Ord {
	ref := *r.ref
	chkFrom := model.Time(chk.MinTime)
	if ref.From != chkFrom {
		if ref.From < chkFrom {
			return iter.Less
		}
		return iter.Greater
	}

	chkThrough := model.Time(chk.MaxTime)
	if ref.Through != chkThrough {
		if ref.Through < chkThrough {
			return iter.Less
		}
		return iter.Greater
	}

	if ref.Checksum != chk.Checksum {
		if ref.Checksum < chk.Checksum {
			return iter.Less
		}
		return iter.Greater
	}

	return iter.Eq
}

type failingIndexClient struct{}

func (f failingIndexClient) QueryPages(_ context.Context, _ []seriesindex.Query, _ seriesindex.QueryPagesCallback) error {
	return errors.New("index client is not initialized likely due to boltdb-shipper not being used")
}

func (f failingIndexClient) Stop() {}
