package indexgateway

import (
	"context"
	"fmt"
	"math"
	"testing"

	"github.com/grafana/dskit/user"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/astmapper"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

// shardOwnerAddrs returns the addresses of the owners of any of shards of the
// tenant's index in table.
func (f *fanoutFixture) shardOwnerAddrs(t *testing.T, table string, shards ...index.ShardAnnotation) map[string]struct{} {
	t.Helper()
	addrs := map[string]struct{}{}
	for _, shard := range shards {
		rs, err := f.ownership.Owners("tenant", table, shard, IndexOwnersRead)
		require.NoError(t, err)
		require.Len(t, rs.Instances, ReplicationFactor)
		for _, a := range rs.GetAddresses() {
			addrs[a] = struct{}{}
		}
	}
	return addrs
}

// shardLabelsOf returns the shard label value each call's matchers carry.
func shardLabelsOf(t *testing.T, calls []fanoutCall) []string {
	t.Helper()
	var values []string
	for _, c := range calls {
		values = append(values, requestShardLabel(t, c.matchers))
	}
	return values
}

// requestShardLabel returns the value of the shard label in matchers, or "".
func requestShardLabel(t *testing.T, matchers string) string {
	t.Helper()
	ms, err := syntax.ParseMatchers(matchers, true)
	require.NoError(t, err, matchers)
	for _, m := range ms {
		if m.Name == astmapper.ShardLabel {
			return m.Value
		}
	}
	return ""
}

// callsByTable groups the recorded calls by the table their range falls in.
func callsByTable(calls []fanoutCall) map[string][]fanoutCall {
	byTable := map[string][]fanoutCall{}
	for _, c := range calls {
		table := fmt.Sprintf("index_%d", int64(c.from/day))
		byTable[table] = append(byTable[table], c)
	}
	return byTable
}

func TestPerIndexShards_FanOutByShard(t *testing.T) {
	const (
		k = 4
		n = 20000
	)
	f := newShardedFanoutFixture(t, nil, k)
	ctx := user.InjectOrgID(context.Background(), "tenant")
	tables := []string{"index_20000", "index_20001"}
	from, through := dayStart(n), dayStart(n+2)-1

	t.Run("without a shard annotation every shard of every table is read", func(t *testing.T) {
		f.gateways.reset()
		resp, err := f.client.GetChunkRef(ctx, &logproto.GetChunkRefRequest{From: from, Through: through, Matchers: `{a="b"}`})
		require.NoError(t, err)

		byTable := callsByTable(f.gateways.recorded())
		require.Len(t, byTable, len(tables))
		for _, table := range tables {
			calls := byTable[table]
			require.Len(t, calls, k, table)
			// each part carries its shard, and goes to an owner of that shard
			require.ElementsMatch(t, []string{"0_of_4", "1_of_4", "2_of_4", "3_of_4"}, shardLabelsOf(t, calls))
			for _, c := range calls {
				shard, err := index.ParseShardString(requestShardLabel(t, c.matchers))
				require.NoError(t, err)
				require.Contains(t, f.shardOwnerAddrs(t, table, shard), c.addr, "%s %s sent to %s, which does not own it", table, shard, c.addr)
			}
		}
		// Every part of a table returns the same refs, so one ref per table
		// plus the one shared by every part.
		require.Len(t, resp.Refs, len(tables)+1)

		parts := findMetric(t, f.reg, "loki_index_gateway_client_request_parts", map[string]string{"operation": "GetChunkRef"})
		require.NotNil(t, parts)
		require.Equal(t, uint64(1), parts.GetHistogram().GetSampleCount())
		require.Equal(t, float64(len(tables)*k), parts.GetHistogram().GetSampleSum())
		tbls := findMetric(t, f.reg, "loki_index_gateway_client_request_tables", map[string]string{"operation": "GetChunkRef"})
		require.NotNil(t, tbls)
		require.Equal(t, float64(len(tables)), tbls.GetHistogram().GetSampleSum())
	})

	t.Run("a power of two shard annotation reads the shards it overlaps", func(t *testing.T) {
		f.gateways.reset()
		// 1_of_2 is the upper half of the fingerprint space: shards 2 and 3 of 4.
		_, err := f.client.GetStats(ctx, &logproto.IndexStatsRequest{From: from, Through: through, Matchers: `{a="b", __cortex_shard__="1_of_2"}`})
		require.NoError(t, err)

		byTable := callsByTable(f.gateways.recorded())
		require.Len(t, byTable, len(tables))
		for _, table := range tables {
			calls := byTable[table]
			require.Len(t, calls, 2, table)
			// the coarser request shard is narrowed to each part's shard
			require.ElementsMatch(t, []string{"2_of_4", "3_of_4"}, shardLabelsOf(t, calls))
			for _, c := range calls {
				shard, err := index.ParseShardString(requestShardLabel(t, c.matchers))
				require.NoError(t, err)
				require.Contains(t, f.shardOwnerAddrs(t, table, shard), c.addr)
			}
		}
	})

	t.Run("a finer power of two shard annotation reads the one shard holding it", func(t *testing.T) {
		f.gateways.reset()
		// 5_of_8 lies within 2_of_4.
		_, err := f.client.GetSeries(ctx, &logproto.GetSeriesRequest{From: from, Through: through, Matchers: `{a="b", __cortex_shard__="5_of_8"}`})
		require.NoError(t, err)

		byTable := callsByTable(f.gateways.recorded())
		for _, table := range tables {
			calls := byTable[table]
			require.Len(t, calls, 1, table)
			require.Contains(t, f.shardOwnerAddrs(t, table, index.NewShard(2, 4)), calls[0].addr)
			// the finer request shard is kept
			require.Equal(t, "5_of_8", requestShardLabel(t, calls[0].matchers))
		}
	})

	t.Run("a bounded shard annotation reads the shards it overlaps", func(t *testing.T) {
		f.gateways.reset()
		// a bounded shard across the boundary between shards 0 and 1 of 4
		bounded := logql.NewBoundedShard(logproto.Shard{Bounds: logproto.FPBounds{Min: 1<<62 - 10, Max: 1<<62 + 10}})
		_, err := f.client.GetStats(ctx, &logproto.IndexStatsRequest{From: from, Through: through, Matchers: fmt.Sprintf(`{a="b", __cortex_shard__=%q}`, bounded.String())})
		require.NoError(t, err)

		byTable := callsByTable(f.gateways.recorded())
		for _, table := range tables {
			calls := byTable[table]
			require.Len(t, calls, 2, table)
			// the bounded range is clipped to each part's shard
			require.ElementsMatch(t, []string{
				logql.NewBoundedShard(logproto.Shard{Bounds: logproto.FPBounds{Min: 1<<62 - 10, Max: 1<<62 - 1}}).String(),
				logql.NewBoundedShard(logproto.Shard{Bounds: logproto.FPBounds{Min: 1 << 62, Max: 1<<62 + 10}}).String(),
			}, shardLabelsOf(t, calls))
			owners := f.shardOwnerAddrs(t, table, index.NewShard(0, 4), index.NewShard(1, 4))
			for _, c := range calls {
				require.Contains(t, owners, c.addr)
			}
		}

		f.gateways.reset()
		// a bounded shard within shard 3 of 4
		bounded = logql.NewBoundedShard(logproto.Shard{Bounds: logproto.FPBounds{Min: 3<<62 + 1, Max: math.MaxUint64}})
		_, err = f.client.GetStats(ctx, &logproto.IndexStatsRequest{From: from, Through: through, Matchers: fmt.Sprintf(`{a="b", __cortex_shard__=%q}`, bounded.String())})
		require.NoError(t, err)
		byTable = callsByTable(f.gateways.recorded())
		for _, table := range tables {
			calls := byTable[table]
			require.Len(t, calls, 1, table)
			require.Contains(t, f.shardOwnerAddrs(t, table, index.NewShard(3, 4)), calls[0].addr)
		}
	})

	t.Run("a single table request with a shard annotation is sent unchanged", func(t *testing.T) {
		f.gateways.reset()
		req := &logproto.IndexStatsRequest{From: dayStart(n) + 10, Through: dayStart(n) + 20, Matchers: `{a="b", __cortex_shard__="3_of_4"}`}
		_, err := f.client.GetStats(ctx, req)
		require.NoError(t, err)
		calls := f.gateways.recorded()
		require.Len(t, calls, 1)
		require.Equal(t, req.From, calls[0].from)
		require.Equal(t, req.Through, calls[0].through)
		require.Contains(t, f.shardOwnerAddrs(t, "index_20000", index.NewShard(3, 4)), calls[0].addr)
	})

}

func TestNarrowMatchers(t *testing.T) {
	bounded := func(minFP, maxFP model.Fingerprint) string {
		return logql.NewBoundedShard(logproto.Shard{Bounds: logproto.FPBounds{Min: minFP, Max: maxFP}}).String()
	}
	for _, tc := range []struct {
		desc     string
		matchers string
		shard    index.ShardAnnotation
		want     string
	}{
		{desc: "unsplit index leaves the matchers alone", matchers: `{a="b"}`, shard: WholeIndexShard, want: `{a="b"}`},
		{desc: "no request shard: the part's shard", matchers: `{a="b"}`, shard: index.NewShard(1, 4), want: `{a="b", __cortex_shard__="1_of_4"}`},
		{desc: "empty matchers: the part's shard", matchers: ``, shard: index.NewShard(1, 4), want: `{__cortex_shard__="1_of_4"}`},
		{desc: "coarser request shard: the part's shard", matchers: `{a="b", __cortex_shard__="0_of_2"}`, shard: index.NewShard(1, 4), want: `{a="b", __cortex_shard__="1_of_4"}`},
		{desc: "same request shard: unchanged", matchers: `{a="b", __cortex_shard__="1_of_4"}`, shard: index.NewShard(1, 4), want: `{a="b", __cortex_shard__="1_of_4"}`},
		{desc: "finer request shard: kept", matchers: `{a="b", __cortex_shard__="3_of_8"}`, shard: index.NewShard(1, 4), want: `{a="b", __cortex_shard__="3_of_8"}`},
		{desc: "bounded inside the shard: kept", matchers: fmt.Sprintf(`{a="b", __cortex_shard__=%q}`, bounded(1<<62+5, 1<<62+10)), shard: index.NewShard(1, 4), want: fmt.Sprintf(`{a="b", __cortex_shard__=%q}`, bounded(1<<62+5, 1<<62+10))},
		{desc: "bounded across the start of the shard: clipped", matchers: fmt.Sprintf(`{a="b", __cortex_shard__=%q}`, bounded(1<<62-5, 1<<62+10)), shard: index.NewShard(1, 4), want: fmt.Sprintf(`{a="b", __cortex_shard__=%q}`, bounded(1<<62, 1<<62+10))},
		{desc: "bounded covering the last shard: the part's shard", matchers: fmt.Sprintf(`{a="b", __cortex_shard__=%q}`, bounded(3<<62-5, math.MaxUint64)), shard: index.NewShard(3, 4), want: `{a="b", __cortex_shard__="3_of_4"}`},
		{desc: "bounded across the start of the last shard: clipped", matchers: fmt.Sprintf(`{a="b", __cortex_shard__=%q}`, bounded(3<<62-5, math.MaxUint64-100)), shard: index.NewShard(3, 4), want: fmt.Sprintf(`{a="b", __cortex_shard__=%q}`, bounded(3<<62, math.MaxUint64-100))},
		{desc: "bounded inside the last shard up to the maximum fingerprint: kept whole", matchers: fmt.Sprintf(`{a="b", __cortex_shard__=%q}`, bounded(3<<62+5, math.MaxUint64)), shard: index.NewShard(3, 4), want: fmt.Sprintf(`{a="b", __cortex_shard__=%q}`, bounded(3<<62+5, math.MaxUint64))},
		{desc: "bounded covering the shard: the part's shard", matchers: fmt.Sprintf(`{a="b", __cortex_shard__=%q}`, bounded(0, math.MaxUint64)), shard: index.NewShard(1, 4), want: `{a="b", __cortex_shard__="1_of_4"}`},
		{desc: "unparsable matchers: unchanged", matchers: `nope`, shard: index.NewShard(1, 4), want: `nope`},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			got := narrowMatchers(tc.matchers, requestShard(tc.matchers), tc.shard)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestRequestShard(t *testing.T) {
	require.Nil(t, requestShard(""))
	require.Nil(t, requestShard(`{a="b"}`))
	require.Nil(t, requestShard(`not matchers`))
	require.Nil(t, requestShard(`{a="b", __cortex_shard__="bogus"}`))

	shard := requestShard(`{a="b", __cortex_shard__="1_of_2"}`)
	require.NotNil(t, shard)
	from, through := shard.GetFromThrough()
	require.Equal(t, model.Fingerprint(1<<63), from)
	require.Equal(t, model.Fingerprint(math.MaxUint64), through)

	bounded := logql.NewBoundedShard(logproto.Shard{Bounds: logproto.FPBounds{Min: 10, Max: 20}})
	shard = requestShard(fmt.Sprintf(`{a="b", __cortex_shard__=%q}`, bounded.String()))
	require.NotNil(t, shard)
	from, through = shard.GetFromThrough()
	require.Equal(t, model.Fingerprint(10), from)
	require.Equal(t, model.Fingerprint(21), through)
}

func TestShardsFor(t *testing.T) {
	f := newShardedFanoutFixture(t, nil, 4)
	unsplit := newFanoutFixture(t, nil)

	require.Equal(t, []index.ShardAnnotation{WholeIndexShard}, unsplit.client.shardsFor(nil))
	require.Equal(t, []index.ShardAnnotation{WholeIndexShard}, unsplit.client.shardsFor(requestShard(`{__cortex_shard__="3_of_4"}`)))

	require.Equal(t, IndexShards(4), f.client.shardsFor(nil))
	require.Equal(t, []index.ShardAnnotation{index.NewShard(3, 4)}, f.client.shardsFor(requestShard(`{__cortex_shard__="3_of_4"}`)))
	require.Equal(t, []index.ShardAnnotation{index.NewShard(0, 4), index.NewShard(1, 4)}, f.client.shardsFor(requestShard(`{__cortex_shard__="0_of_2"}`)))
	// an empty bounded range selects nothing; one owner answers it
	empty := emptyFilter{}
	require.Equal(t, []index.ShardAnnotation{index.NewShard(0, 4)}, f.client.shardsFor(empty))
}

type emptyFilter struct{}

func (emptyFilter) Match(model.Fingerprint) bool                           { return false }
func (emptyFilter) GetFromThrough() (model.Fingerprint, model.Fingerprint) { return 5, 5 }
