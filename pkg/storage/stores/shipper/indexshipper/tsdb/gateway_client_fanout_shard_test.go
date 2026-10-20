package tsdb

import (
	"context"
	"fmt"
	"math"
	"testing"

	"github.com/grafana/dskit/user"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/indexgateway"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/astmapper"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

// These tests check the per_index client against a tenant whose index is split
// into K fingerprint-range files: each part of a request goes to the owners of
// one (table, shard), and the merged answers equal one request answered by one
// gateway over the same files.

const fanoutShardFactor = 4

// fanoutShardMatchers carry the shard annotations the query planner adds:
// a power-of-two shard coarser than K, one equal to K and one finer, plus
// none.
var fanoutShardMatchers = []string{
	`{app=~".+"}`,
	`{app=~".+", __cortex_shard__="1_of_2"}`,
	`{app=~".+", __cortex_shard__="2_of_4"}`,
	`{app=~".+", __cortex_shard__="7_of_8"}`,
	`{env="prod", __cortex_shard__="0_of_2"}`,
}

// expectedShards returns the shards of the tenant's index a request with
// matchers reads, as the client splits them.
func (e *fanoutEnv) expectedShards(t *testing.T, matchers string) []index.ShardAnnotation {
	t.Helper()
	shards := indexgateway.IndexShards(e.shardFactor)
	ms, err := syntax.ParseMatchers(matchers, true)
	require.NoError(t, err)
	for _, m := range ms {
		if m.Name != astmapper.ShardLabel {
			continue
		}
		shard, err := index.ParseShardString(m.Value)
		require.NoError(t, err)
		from, through := shard.GetFromThrough()
		return indexgateway.ShardsOverlapping(e.shardFactor, from, through)
	}
	return shards
}

// checkShardCallsGoToOwners checks that the per_index client sent, for each
// table, one part per shard the request reads, each to an owner of one of
// those shards.
func (e *fanoutEnv) checkShardCallsGoToOwners(t *testing.T, matchers string) {
	t.Helper()
	shards := e.expectedShards(t, matchers)
	byTable := map[string][]fanoutCall{}
	for _, c := range e.calls.take() {
		table := fmt.Sprintf("index_%d", int64(c.from/fanoutDay))
		byTable[table] = append(byTable[table], c)
	}
	for table, calls := range byTable {
		require.Len(t, calls, len(shards), "%s should get one part per shard", table)
		owners := map[string]struct{}{}
		for _, shard := range shards {
			rs, err := e.ownership.Owners(fanoutTenant, table, shard, indexgateway.IndexOwnersRead)
			require.NoError(t, err)
			for _, a := range rs.GetAddresses() {
				owners[a] = struct{}{}
			}
		}
		for _, c := range calls {
			require.Contains(t, owners, c.addr, "%s for %s sent to %s, which owns none of its shards", c.method, table, c.addr)
		}
	}
}

func TestGatewayClientPerShard_ChunkRefsMatchSingleCall(t *testing.T) {
	e := newFanoutEnvWithShards(t, fanoutShardFactor)
	ctx := user.InjectOrgID(context.Background(), fanoutTenant)

	for name, r := range fanoutRanges() {
		for _, m := range fanoutShardMatchers {
			t.Run(name+"/"+m, func(t *testing.T) {
				req := &logproto.GetChunkRefRequest{From: r[0], Through: r[1], Matchers: m}
				want, err := e.single.GetChunkRef(ctx, req)
				require.NoError(t, err)
				e.calls.take()

				got, err := e.perIndex.GetChunkRef(ctx, req)
				require.NoError(t, err)
				require.ElementsMatch(t, want.Refs, got.Refs)
				require.Equal(t, int64(len(want.Refs)), got.Stats.TotalChunks)
				require.Equal(t, int64(len(want.Refs)), got.Stats.PostFilterChunks)
				e.checkShardCallsGoToOwners(t, m)
			})
		}
	}
}

func TestGatewayClientPerShard_SeriesAndLabelsMatchSingleCall(t *testing.T) {
	e := newFanoutEnvWithShards(t, fanoutShardFactor)
	ctx := user.InjectOrgID(context.Background(), fanoutTenant)

	for name, r := range fanoutRanges() {
		for _, m := range fanoutShardMatchers {
			t.Run(name+"/"+m, func(t *testing.T) {
				seriesReq := &logproto.GetSeriesRequest{From: r[0], Through: r[1], Matchers: m}
				wantSeries, err := e.single.GetSeries(ctx, seriesReq)
				require.NoError(t, err)
				e.calls.take()
				gotSeries, err := e.perIndex.GetSeries(ctx, seriesReq)
				require.NoError(t, err)
				require.ElementsMatch(t, wantSeries.Series, gotSeries.Series)
				e.checkShardCallsGoToOwners(t, m)

				namesReq := &logproto.LabelNamesForMetricNameRequest{From: r[0], Through: r[1], MetricName: "logs", Matchers: m}
				wantNames, err := e.single.LabelNamesForMetricName(ctx, namesReq)
				require.NoError(t, err)
				gotNames, err := e.perIndex.LabelNamesForMetricName(ctx, namesReq)
				require.NoError(t, err)
				require.ElementsMatch(t, wantNames.Values, gotNames.Values)

				valuesReq := &logproto.LabelValuesForMetricNameRequest{From: r[0], Through: r[1], MetricName: "logs", LabelName: "env", Matchers: m}
				wantValues, err := e.single.LabelValuesForMetricName(ctx, valuesReq)
				require.NoError(t, err)
				gotValues, err := e.perIndex.LabelValuesForMetricName(ctx, valuesReq)
				require.NoError(t, err)
				require.ElementsMatch(t, wantValues.Values, gotValues.Values)
				e.calls.take()
			})
		}
	}
}

// Stats and volumes are summed over parts. Shards are disjoint, so summing
// over them is exact; tables are not, so a stream found in several tables is
// counted once per table, as with per-table fan-out. The sum over shards of
// one table equals a single request over that table.
func TestGatewayClientPerShard_StatsAndVolumeSumPerTable(t *testing.T) {
	e := newFanoutEnvWithShards(t, fanoutShardFactor)
	ctx := user.InjectOrgID(context.Background(), fanoutTenant)

	for name, r := range fanoutRanges() {
		for _, m := range fanoutShardMatchers {
			t.Run(name+"/"+m, func(t *testing.T) {
				parts := fanoutParts(r[0], r[1], e.tableRange)

				var wantStats logproto.IndexStatsResponse
				for _, p := range parts {
					s, err := e.single.GetStats(ctx, &logproto.IndexStatsRequest{From: p[0], Through: p[1], Matchers: m})
					require.NoError(t, err)
					wantStats.Streams += s.Streams
					wantStats.Chunks += s.Chunks
					wantStats.Bytes += s.Bytes
					wantStats.Entries += s.Entries
				}
				e.calls.take()

				got, err := e.perIndex.GetStats(ctx, &logproto.IndexStatsRequest{From: r[0], Through: r[1], Matchers: m})
				require.NoError(t, err)
				require.Equal(t, &wantStats, got)
				e.checkShardCallsGoToOwners(t, m)

				want, err := e.single.GetVolume(ctx, &logproto.VolumeRequest{From: r[0], Through: r[1], Matchers: m, Limit: 100, AggregateBy: "series"})
				require.NoError(t, err)
				e.calls.take()
				gotVolume, err := e.perIndex.GetVolume(ctx, &logproto.VolumeRequest{From: r[0], Through: r[1], Matchers: m, Limit: 100, AggregateBy: "series"})
				require.NoError(t, err)
				if len(parts) == 1 {
					// one table: the sum over disjoint shards is exact
					require.ElementsMatch(t, want.Volumes, gotVolume.Volumes)
				}
				e.calls.take()
			})
		}
	}
}

// A single table GetShards is planned by the owners of each shard and the
// plans are concatenated: the result covers the fingerprint space once, in
// order, with the same totals as one plan over the whole table.
func TestGatewayClientPerShard_ShardsConcatenateToSinglePlanCoverage(t *testing.T) {
	e := newFanoutEnvWithShards(t, fanoutShardFactor)
	ctx := user.InjectOrgID(context.Background(), fanoutTenant)
	d := fanoutDayStart

	for _, tc := range []struct {
		name                string
		from, through       model.Time
		targetBytesPerShard uint64
	}{
		{name: "one shard per stream", from: d(1), through: d(2) - 1, targetBytesPerShard: 1},
		{name: "one big shard", from: d(1), through: d(2) - 1, targetBytesPerShard: 1 << 30},
		{name: "a few streams", from: d(3), through: d(4) - 1, targetBytesPerShard: 1},
		{name: "nothing matches", from: d(1) + 10*fanoutHour, through: d(1) + 11*fanoutHour, targetBytesPerShard: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := &logproto.ShardsRequest{From: tc.from, Through: tc.through, Query: `{app=~".+"}`, TargetBytesPerShard: tc.targetBytesPerShard}
			want, err := e.single.GetShards(ctx, req)
			require.NoError(t, err)
			e.calls.take()

			got, err := e.perIndex.GetShards(ctx, req)
			require.NoError(t, err)

			// one part per shard, each to an owner of that shard
			calls := e.calls.take()
			require.Len(t, calls, fanoutShardFactor)
			table := fmt.Sprintf("index_%d", int64(tc.from/fanoutDay))
			owners := map[string]struct{}{}
			for _, shard := range indexgateway.IndexShards(fanoutShardFactor) {
				rs, err := e.ownership.Owners(fanoutTenant, table, shard, indexgateway.IndexOwnersRead)
				require.NoError(t, err)
				for _, a := range rs.GetAddresses() {
					owners[a] = struct{}{}
				}
			}
			for _, c := range calls {
				require.Contains(t, owners, c.addr)
			}

			// the plan covers the fingerprint space once, in order
			require.NotEmpty(t, got.Shards)
			require.Equal(t, model.Fingerprint(0), got.Shards[0].Bounds.Min)
			for i := 1; i < len(got.Shards); i++ {
				require.Equal(t, got.Shards[i-1].Bounds.Max+1, got.Shards[i].Bounds.Min, "shards %d and %d are not contiguous", i-1, i)
			}
			require.Equal(t, model.Fingerprint(math.MaxUint64), got.Shards[len(got.Shards)-1].Bounds.Max)

			// with the same totals as the single plan
			require.Equal(t, sumShardStats(want.Shards), sumShardStats(got.Shards))

			// and every series in exactly one shard
			series, err := e.single.GetSeries(ctx, &logproto.GetSeriesRequest{From: tc.from, Through: tc.through, Matchers: `{app=~".+"}`})
			require.NoError(t, err)
			for _, s := range series.Series {
				fp := model.Fingerprint(labels.StableHash(logproto.FromLabelAdaptersToLabels(s.Labels)))
				n := 0
				for _, shard := range got.Shards {
					if fp >= shard.Bounds.Min && fp <= shard.Bounds.Max {
						n++
					}
				}
				require.Equal(t, 1, n, "series %v in %d shards", s.Labels, n)
			}
			e.calls.take()
		})
	}
}

func sumShardStats(shards []logproto.Shard) logproto.IndexStatsResponse {
	var sum logproto.IndexStatsResponse
	for _, s := range shards {
		if s.Stats == nil {
			continue
		}
		sum.Streams += s.Stats.Streams
		sum.Chunks += s.Stats.Chunks
		sum.Bytes += s.Stats.Bytes
		sum.Entries += s.Stats.Entries
	}
	return sum
}
