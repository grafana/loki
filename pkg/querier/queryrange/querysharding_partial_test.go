package queryrange

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/user"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/querier/plan"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/types"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/server"
)

// TestASTMapperware_PartialStatsOnError runs a two-leg sharded query where the
// left leg completes while the right leg fails. The completed leg's usage is not
// part of the failing Downstream call's results, so without the partial-stats
// collector it would be dropped along with the engine result.
func TestASTMapperware_PartialStatsOnError(t *testing.T) {
	const (
		bytesPerShard = 1000
		shardsPerLeg  = 2
	)

	limitErr := wrapErr(logqlmodel.ErrQuerierTooManyBytes, httpgrpc.Errorf(http.StatusBadRequest, "query too large to execute on a single querier: (query: 5GB, limit: 1GB)"))

	var (
		leftCalls   atomic.Int64
		gateTimeout atomic.Bool
	)

	handler := queryrangebase.HandlerFunc(func(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		if _, ok := req.(*logproto.IndexStatsRequest); ok {
			return &IndexStatsResponse{
				Response: &logproto.IndexStatsResponse{Bytes: 1 << 40},
			}, nil
		}

		if strings.Contains(req.GetQuery(), `app="left"`) {
			leftCalls.Add(1)
			return &LokiPromResponse{
				Response: &queryrangebase.PrometheusResponse{
					Data: queryrangebase.PrometheusData{
						ResultType: loghttp.ResultTypeVector,
						Result: []queryrangebase.SampleStream{{
							Labels:  []logproto.LabelAdapter{{Name: "foo", Value: "bar"}},
							Samples: []logproto.LegacySample{{Value: 10, TimestampMs: 10}},
						}},
					},
				},
				Statistics: stats.Result{
					Querier: stats.Querier{Store: stats.Store{Chunk: stats.Chunk{DecompressedBytes: bytesPerShard}}},
				},
			}, nil
		}

		// Right leg: fail only once the left leg's results have been folded into
		// the engine's statistics, which is the case this test is about.
		deadline := time.Now().Add(10 * time.Second)
		for stats.FromContext(ctx).Result(0, 0, 0).Querier.Store.Chunk.DecompressedBytes < shardsPerLeg*bytesPerShard {
			if time.Now().After(deadline) {
				gateTimeout.Store(true)
				break
			}
			time.Sleep(time.Millisecond)
		}
		return nil, limitErr
	})

	mware := newASTMapperware(
		ShardingConfigs{config.PeriodConfig{IndexType: types.IndexTypeTSDB}},
		testEngineOpts,
		handler,
		handler,
		handler,
		log.NewNopLogger(),
		nilShardingMetrics,
		fakeLimits{
			maxSeries:               math.MaxInt32,
			maxQueryParallelism:     4,
			tsdbMaxQueryParallelism: 4,
			queryTimeout:            time.Minute,
		},
		shardsPerLeg,
		[]string{},
	)

	query := `count_over_time({app="left"}[1h]) / count_over_time({app="right"}[1h])`
	req := defaultReq()
	req.Query = query
	req.Plan = &plan.QueryPlan{AST: syntax.MustParseExpr(query)}

	data := &queryData{}
	ctx := user.InjectOrgID(context.WithValue(context.Background(), ctxKey, data), "1")

	lines := captureFailedQueryUsage(t)
	countBefore := failedQueryUsageCount(t, server.FailureLimit)

	_, err := StatsCollectorMiddleware().Wrap(mware).Do(ctx, req)
	require.ErrorIs(t, err, logqlmodel.ErrQuerierTooManyBytes)
	require.False(t, gateTimeout.Load(), "the left leg never completed, the test did not exercise the intended path")
	require.Equal(t, int64(shardsPerLeg), leftCalls.Load())

	line := lines.only(t)
	requireFailedQueryUsageShape(t, line)
	require.Equal(t,
		util.HumanizeBytes(shardsPerLeg*bytesPerShard),
		line["total_bytes"],
		"the completed leg's usage must be kept on the failure path",
	)
	require.Equal(t, server.FailureLimit, line["failure_category"])
	require.Equal(t, "querier_too_large", line["failure_reason"])
	require.Equal(t, countBefore+1, failedQueryUsageCount(t, server.FailureLimit))
	// The failed query gets the dedicated line only, no regular stats line.
	require.False(t, data.recorded)
}

// TestASTMapperware_QuerierBytesLimitIsClassifiedAsLimit covers the
// MaxQuerierBytesRead rejection raised by the sharding middleware: it must
// classify as a limit failure, not a generic bad request.
func TestASTMapperware_QuerierBytesLimitIsClassifiedAsLimit(t *testing.T) {
	handler := queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		if _, ok := req.(*logproto.IndexStatsRequest); ok {
			return &IndexStatsResponse{
				Response: &logproto.IndexStatsResponse{Bytes: 100},
			}, nil
		}
		return nil, fmt.Errorf("unexpected request type %T", req)
	})

	mware := newASTMapperware(
		ShardingConfigs{config.PeriodConfig{IndexType: types.IndexTypeTSDB}},
		testEngineOpts,
		handler,
		handler,
		handler,
		log.NewNopLogger(),
		nilShardingMetrics,
		fakeLimits{
			maxSeries:               math.MaxInt32,
			maxQueryParallelism:     1,
			tsdbMaxQueryParallelism: 1,
			queryTimeout:            time.Minute,
			maxQuerierBytesRead:     10,
		},
		0,
		[]string{},
	)

	query := `avg_over_time({app="foo"} | json busy="utilization" | unwrap busy [5m])`
	req := defaultReq()
	req.Query = query
	req.Plan = &plan.QueryPlan{AST: syntax.MustParseExpr(query)}

	_, err := mware.Do(user.InjectOrgID(context.Background(), "1"), req)
	require.Error(t, err)

	requireSentinelClassification(t, err, logqlmodel.ErrQuerierTooManyBytes,
		server.FailureLimit, "querier_too_large",
		http.StatusBadRequest, fmt.Sprintf(limErrQuerierTooManyBytesShardableTmpl, "100 B", "10 B"))
}

// TestQuerySizeLimitSpecs pins every byte-limit enforcement spec: each one must
// carry a sentinel, so no rejection path silently degrades to an unclassified
// bad request.
func TestQuerySizeLimitSpecs(t *testing.T) {
	for name, tc := range map[string]struct {
		spec         querySizeLimitSpec
		limitName    string
		sentinel     error
		tmpl         string
		wantCategory string
		wantReason   string
	}{
		"max_query_bytes_read": {
			maxQueryBytesReadSpec, "MaxQueryBytesRead",
			logqlmodel.ErrMaxQueryBytesRead, limErrQueryTooManyBytesTmpl,
			server.FailureLimit, "max_query_bytes_read",
		},
		"max_querier_bytes_read": {
			maxQuerierBytesReadSpec, "MaxQuerierBytesRead",
			logqlmodel.ErrQuerierTooManyBytes, limErrQuerierTooManyBytesTmpl,
			server.FailureLimit, "querier_too_large",
		},
		"max_querier_bytes_read_shardable": {
			maxQuerierBytesReadShardableSpec, "MaxQuerierBytesRead",
			logqlmodel.ErrQuerierTooManyBytes, limErrQuerierTooManyBytesShardableTmpl,
			server.FailureLimit, "querier_too_large",
		},
		"max_querier_bytes_read_unshardable": {
			maxQuerierBytesReadUnshardableSpec, "MaxQuerierBytesRead",
			logqlmodel.ErrQuerierTooManyBytes, limErrQuerierTooManyBytesUnshardableTmpl,
			server.FailureLimit, "querier_too_large",
		},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.limitName, tc.spec.limitName)
			require.Equal(t, tc.sentinel, tc.spec.sentinel)
			require.Equal(t, tc.tmpl, tc.spec.errorTmpl)

			requireSentinelClassification(t, tc.spec.exceededErr("100 B", "10 B"), tc.sentinel,
				tc.wantCategory, tc.wantReason,
				http.StatusBadRequest, fmt.Sprintf(tc.tmpl, "100 B", "10 B"))
		})
	}
}
