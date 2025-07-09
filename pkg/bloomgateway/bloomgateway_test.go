package bloomgateway

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/user"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/plan"
	"github.com/grafana/loki/v3/pkg/storage"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
	bloomshipperconfig "github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper/config"
	"github.com/grafana/loki/v3/pkg/storage/types"
	"github.com/grafana/loki/v3/pkg/util/mempool"
	"github.com/grafana/loki/v3/pkg/validation"
)

func stringSlice[T fmt.Stringer](s []T) []string {
	res := make([]string, len(s))
	for i := range res {
		res[i] = s[i].String()
	}
	return res
}

func groupRefs(t *testing.T, chunkRefs []*logproto.ChunkRef) []*logproto.GroupedChunkRefs {
	t.Helper()
	grouped := groupChunkRefs(nil, chunkRefs, nil)
	// Put fake labels to the series
	for _, g := range grouped {
		g.Labels = &logproto.IndexSeries{
			Labels: logproto.FromLabelsToLabelAdapters(labels.FromStrings("foo", fmt.Sprintf("%d", g.Fingerprint))),
		}
	}

	return grouped
}

func newLimits() *validation.Overrides {
	limits := validation.Limits{}
	flagext.DefaultValues(&limits)
	limits.BloomGatewayEnabled = true

	overrides, _ := validation.NewOverrides(limits, nil)
	return overrides
}

func setupBloomStore(t *testing.T) *bloomshipper.BloomStore {
	logger := log.NewNopLogger()

	cm := storage.NewClientMetrics()
	t.Cleanup(cm.Unregister)

	p := config.PeriodConfig{
		From: parseDayTime("2023-09-01"),
		IndexTables: config.IndexPeriodicTableConfig{
			PeriodicTableConfig: config.PeriodicTableConfig{
				Prefix: "index_",
				Period: 24 * time.Hour,
			},
		},
		IndexType:  types.TSDBType,
		ObjectType: types.StorageTypeFileSystem,
		Schema:     "v13",
		RowShards:  16,
	}
	schemaCfg := config.SchemaConfig{
		Configs: []config.PeriodConfig{p},
	}
	storageCfg := storage.Config{
		BloomShipperConfig: bloomshipperconfig.Config{
			WorkingDirectory:    []string{t.TempDir()},
			DownloadParallelism: 1,
			BlocksCache: bloomshipperconfig.BlocksCacheConfig{
				SoftLimit: flagext.Bytes(10 << 20),
				HardLimit: flagext.Bytes(20 << 20),
				TTL:       time.Hour,
			},
		},
		FSConfig: local.FSConfig{
			Directory: t.TempDir(),
		},
	}

	reg := prometheus.NewRegistry()
	blocksCache := bloomshipper.NewFsBlocksCache(storageCfg.BloomShipperConfig.BlocksCache, nil, logger)
	store, err := bloomshipper.NewBloomStore(schemaCfg.Configs, storageCfg, cm, nil, blocksCache, &mempool.SimpleHeapAllocator{}, reg, logger)
	require.NoError(t, err)
	t.Cleanup(store.Stop)

	return store
}

func TestBloomGateway_StartStopService(t *testing.T) {
	logger := log.NewNopLogger()
	reg := prometheus.NewRegistry()

	t.Run("start and stop bloom gateway", func(t *testing.T) {
		cfg := Config{
			Enabled:                 true,
			WorkerConcurrency:       4,
			MaxOutstandingPerTenant: 1024,
		}

		store := setupBloomStore(t)
		gw, err := New(cfg, store, logger, reg)
		require.NoError(t, err)

		err = services.StartAndAwaitRunning(context.Background(), gw)
		require.NoError(t, err)

		// Wait for workers to connect to queue
		time.Sleep(50 * time.Millisecond)
		require.Equal(t, float64(cfg.WorkerConcurrency), gw.queue.GetConnectedConsumersMetric())

		err = services.StopAndAwaitTerminated(context.Background(), gw)
		require.NoError(t, err)
	})
}

func TestBloomGateway_FilterChunkRefs(t *testing.T) {
	tenantID := "test"

	logger := log.NewNopLogger()
	cfg := Config{
		Enabled:                 true,
		WorkerConcurrency:       2,
		BlockQueryConcurrency:   2,
		MaxOutstandingPerTenant: 1024,
	}

	t.Run("request fails when providing invalid block", func(t *testing.T) {
		now := mktime("2023-10-03 10:00")

		refs, metas, queriers, data := createBlocks(t, tenantID, 10, now.Add(-1*time.Hour), now, 0x0000, 0x0fff)
		mockStore := newMockBloomStore(refs, queriers, metas)

		reg := prometheus.NewRegistry()
		gw, err := New(cfg, mockStore, logger, reg)
		require.NoError(t, err)

		err = services.StartAndAwaitRunning(context.Background(), gw)
		require.NoError(t, err)
		t.Cleanup(func() {
			err = services.StopAndAwaitTerminated(context.Background(), gw)
			require.NoError(t, err)
		})

		chunkRefs := createQueryInputFromBlockData(t, tenantID, data, 100)

		expr, err := syntax.ParseExpr(`{foo="bar"} | trace_id="nomatch"`)
		require.NoError(t, err)

		req := &logproto.FilterChunkRefRequest{
			From:    now.Add(-24 * time.Hour),
			Through: now,
			Refs:    groupRefs(t, chunkRefs),
			Plan:    plan.QueryPlan{AST: expr},
			Blocks:  []string{"bloom/invalid/block.tar"},
		}

		ctx := user.InjectOrgID(context.Background(), tenantID)
		res, err := gw.FilterChunkRefs(ctx, req)
		require.ErrorContainsf(t, err, "could not parse block key", "%+v", res)
	})

	t.Run("shipper error is propagated", func(t *testing.T) {
		now := mktime("2023-10-03 10:00")

		refs, metas, queriers, data := createBlocks(t, tenantID, 10, now.Add(-1*time.Hour), now, 0x0000, 0x0fff)
		mockStore := newMockBloomStore(refs, queriers, metas)
		mockStore.err = errors.New("request failed")

		reg := prometheus.NewRegistry()
		gw, err := New(cfg, mockStore, logger, reg)
		require.NoError(t, err)

		err = services.StartAndAwaitRunning(context.Background(), gw)
		require.NoError(t, err)
		t.Cleanup(func() {
			err = services.StopAndAwaitTerminated(context.Background(), gw)
			require.NoError(t, err)
		})

		chunkRefs := createQueryInputFromBlockData(t, tenantID, data, 100)

		// saturate workers
		// then send additional request
		for i := 0; i < gw.cfg.WorkerConcurrency+1; i++ {
			expr, err := syntax.ParseExpr(`{foo="bar"} | trace_id="nomatch"`)
			require.NoError(t, err)

			req := &logproto.FilterChunkRefRequest{
				From:    now.Add(-24 * time.Hour),
				Through: now,
				Refs:    groupRefs(t, chunkRefs),
				Plan:    plan.QueryPlan{AST: expr},
				Blocks:  stringSlice(refs),
			}

			ctx, cancelFn := context.WithTimeout(context.Background(), 10*time.Second)
			ctx = user.InjectOrgID(ctx, tenantID)
			t.Cleanup(cancelFn)

			res, err := gw.FilterChunkRefs(ctx, req)
			require.ErrorContainsf(t, err, "request failed", "%+v", res)
		}
	})

	t.Run("request cancellation does not result in channel locking", func(t *testing.T) {
		now := mktime("2024-01-25 10:00")

		// replace store implementation and re-initialize workers and sub-services
		refs, metas, queriers, data := createBlocks(t, tenantID, 10, now.Add(-1*time.Hour), now, 0x0000, 0x0fff)
		mockStore := newMockBloomStore(refs, queriers, metas)
		mockStore.delay = 2000 * time.Millisecond

		reg := prometheus.NewRegistry()
		gw, err := New(cfg, mockStore, logger, reg)
		require.NoError(t, err)

		err = services.StartAndAwaitRunning(context.Background(), gw)
		require.NoError(t, err)
		t.Cleanup(func() {
			err = services.StopAndAwaitTerminated(context.Background(), gw)
			require.NoError(t, err)
		})

		chunkRefs := createQueryInputFromBlockData(t, tenantID, data, 100)

		// saturate workers
		// then send additional request
		for i := 0; i < gw.cfg.WorkerConcurrency+1; i++ {
			expr, err := syntax.ParseExpr(`{foo="bar"} | trace_id="nomatch"`)
			require.NoError(t, err)

			req := &logproto.FilterChunkRefRequest{
				From:    now.Add(-24 * time.Hour),
				Through: now,
				Refs:    groupRefs(t, chunkRefs),
				Plan:    plan.QueryPlan{AST: expr},
				Blocks:  stringSlice(refs),
			}

			ctx, cancelFn := context.WithTimeout(context.Background(), 500*time.Millisecond)
			ctx = user.InjectOrgID(ctx, tenantID)
			t.Cleanup(cancelFn)

			res, err := gw.FilterChunkRefs(ctx, req)
			require.ErrorContainsf(t, err, context.DeadlineExceeded.Error(), "%+v", res)
		}
	})

	t.Run("returns unfiltered chunk refs if no filters provided", func(t *testing.T) {
		now := mktime("2023-10-03 10:00")

		reg := prometheus.NewRegistry()
		gw, err := New(cfg, newMockBloomStore(nil, nil, nil), logger, reg)
		require.NoError(t, err)

		err = services.StartAndAwaitRunning(context.Background(), gw)
		require.NoError(t, err)
		t.Cleanup(func() {
			err = services.StopAndAwaitTerminated(context.Background(), gw)
			require.NoError(t, err)
		})

		// input chunks need to be sorted by their fingerprint
		chunkRefs := []*logproto.ChunkRef{
			{Fingerprint: 1000, UserID: tenantID, From: now.Add(-22 * time.Hour), Through: now.Add(-21 * time.Hour), Checksum: 2},
			{Fingerprint: 1000, UserID: tenantID, From: now.Add(-23 * time.Hour), Through: now.Add(-22 * time.Hour), Checksum: 4},
			{Fingerprint: 2000, UserID: tenantID, From: now.Add(-20 * time.Hour), Through: now.Add(-19 * time.Hour), Checksum: 3},
			{Fingerprint: 3000, UserID: tenantID, From: now.Add(-24 * time.Hour), Through: now.Add(-23 * time.Hour), Checksum: 1},
		}
		req := &logproto.FilterChunkRefRequest{
			From:    now.Add(-24 * time.Hour),
			Through: now,
			Refs:    groupRefs(t, chunkRefs),
		}

		ctx := user.InjectOrgID(context.Background(), tenantID)
		res, err := gw.FilterChunkRefs(ctx, req)
		require.NoError(t, err)
		require.Equal(t, &logproto.FilterChunkRefResponse{
			ChunkRefs: []*logproto.GroupedChunkRefs{
				{Fingerprint: 1000, Tenant: tenantID, Refs: []*logproto.ShortRef{
					{From: 1696248000000, Through: 1696251600000, Checksum: 2},
					{From: 1696244400000, Through: 1696248000000, Checksum: 4},
				}, Labels: &logproto.IndexSeries{
					Labels: logproto.FromLabelsToLabelAdapters(labels.FromStrings("foo", "1000")),
				}},
				{Fingerprint: 2000, Tenant: tenantID, Refs: []*logproto.ShortRef{
					{From: 1696255200000, Through: 1696258800000, Checksum: 3},
				}, Labels: &logproto.IndexSeries{
					Labels: logproto.FromLabelsToLabelAdapters(labels.FromStrings("foo", "2000")),
				}},
				{Fingerprint: 3000, Tenant: tenantID, Refs: []*logproto.ShortRef{
					{From: 1696240800000, Through: 1696244400000, Checksum: 1},
				}, Labels: &logproto.IndexSeries{
					Labels: logproto.FromLabelsToLabelAdapters(labels.FromStrings("foo", "3000")),
				}},
			},
		}, res)
	})

	t.Run("gateway tracks active users", func(t *testing.T) {
		now := mktime("2023-10-03 10:00")

		reg := prometheus.NewRegistry()
		gw, err := New(cfg, newMockBloomStore(nil, nil, nil), logger, reg)
		require.NoError(t, err)

		err = services.StartAndAwaitRunning(context.Background(), gw)
		require.NoError(t, err)
		t.Cleanup(func() {
			err = services.StopAndAwaitTerminated(context.Background(), gw)
			require.NoError(t, err)
		})

		tenants := []string{"tenant-a", "tenant-b", "tenant-c"}
		for idx, tenantID := range tenants {
			chunkRefs := []*logproto.ChunkRef{
				{
					Fingerprint: uint64(1000 + 100*idx),
					UserID:      tenantID,
					From:        now.Add(-4 * time.Hour),
					Through:     now,
					Checksum:    uint32(idx),
				},
			}
			ref := bloomshipper.BlockRef{
				Ref: bloomshipper.Ref{
					TenantID:       tenantID,
					TableName:      "table_1",
					Bounds:         v1.NewBounds(0, 10000),
					StartTimestamp: now.Add(-4 * time.Hour),
					EndTimestamp:   now,
					Checksum:       uint32(idx),
				},
			}
			expr, err := syntax.ParseExpr(`{foo="bar"} | trace_id="nomatch"`)
			require.NoError(t, err)
			req := &logproto.FilterChunkRefRequest{
				From:    now.Add(-4 * time.Hour),
				Through: now,
				Refs:    groupRefs(t, chunkRefs),
				Plan:    plan.QueryPlan{AST: expr},
				Blocks:  stringSlice([]bloomshipper.BlockRef{ref}),
			}
			ctx := user.InjectOrgID(context.Background(), tenantID)
			_, err = gw.FilterChunkRefs(ctx, req)
			require.NoError(t, err)
		}
		require.ElementsMatch(t, tenants, gw.activeUsers.ActiveUsers())
	})

	t.Run("use fuse queriers to filter chunks", func(t *testing.T) {
		now := mktime("2023-10-03 10:00")

		// replace store implementation and re-initialize workers and sub-services
		refs, metas, queriers, data := createBlocks(t, tenantID, 10, now.Add(-1*time.Hour), now, 0x0000, 0x0fff)

		reg := prometheus.NewRegistry()
		store := newMockBloomStore(refs, queriers, metas)

		gw, err := New(cfg, store, logger, reg)
		require.NoError(t, err)

		err = services.StartAndAwaitRunning(context.Background(), gw)
		require.NoError(t, err)
		t.Cleanup(func() {
			err = services.StopAndAwaitTerminated(context.Background(), gw)
			require.NoError(t, err)
		})

		chunkRefs := createQueryInputFromBlockData(t, tenantID, data, 10)

		t.Run("no match - return empty response", func(t *testing.T) {
			inputChunkRefs := groupRefs(t, chunkRefs)
			expr, err := syntax.ParseExpr(`{foo="bar"} | trace_id="nomatch"`)
			require.NoError(t, err)
			req := &logproto.FilterChunkRefRequest{
				From:    now.Add(-8 * time.Hour),
				Through: now,
				Refs:    inputChunkRefs,
				Plan:    plan.QueryPlan{AST: expr},
				Blocks:  stringSlice(refs),
			}
			ctx := user.InjectOrgID(context.Background(), tenantID)
			res, err := gw.FilterChunkRefs(ctx, req)
			require.NoError(t, err)

			expectedResponse := &logproto.FilterChunkRefResponse{
				ChunkRefs: []*logproto.GroupedChunkRefs{},
			}
			require.Equal(t, expectedResponse, res)
		})

		t.Run("match - return filtered", func(t *testing.T) {
			inputChunkRefs := groupRefs(t, chunkRefs)
			// Hack to get search string for a specific series
			// see MkBasicSeriesWithBlooms() in pkg/storage/bloom/v1/test_util.go
			rnd := rand.Intn(len(inputChunkRefs))
			fp := inputChunkRefs[rnd].Fingerprint
			lbs := inputChunkRefs[rnd].Labels
			chks := inputChunkRefs[rnd].Refs
			key := fmt.Sprintf("%s:%04x", model.Fingerprint(fp), 0)

			t.Log("rnd=", rnd, "fp=", fp, "key=", key)

			expr, err := syntax.ParseExpr(fmt.Sprintf(`{foo="bar"} | trace_id="%s"`, key))
			require.NoError(t, err)

			req := &logproto.FilterChunkRefRequest{
				From:    now.Add(-8 * time.Hour),
				Through: now,
				Refs:    inputChunkRefs,
				Plan:    plan.QueryPlan{AST: expr},
				Blocks:  stringSlice(refs),
			}
			ctx := user.InjectOrgID(context.Background(), tenantID)
			res, err := gw.FilterChunkRefs(ctx, req)
			require.NoError(t, err)

			expectedResponse := &logproto.FilterChunkRefResponse{
				ChunkRefs: []*logproto.GroupedChunkRefs{
					{
						Fingerprint: fp,
						Labels:      lbs,
						Refs:        chks,
						Tenant:      tenantID,
					},
				},
			}
			require.Equal(t, expectedResponse, res)
		})

	})
}

func TestFilterChunkRefsForSeries(t *testing.T) {
	mkInput := func(xs []uint32) *logproto.GroupedChunkRefs {
		out := &logproto.GroupedChunkRefs{Refs: make([]*logproto.ShortRef, len(xs))}
		for i, x := range xs {
			out.Refs[i] = &logproto.ShortRef{Checksum: x}
		}
		return out
	}
	mkRemovals := func(xs []uint32) v1.ChunkRefs {
		out := make(v1.ChunkRefs, len(xs))
		for i, x := range xs {
			out[i] = v1.ChunkRef{Checksum: x}
		}
		return out
	}

	for _, tc := range []struct {
		desc                      string
		input, removals, expected []uint32
	}{
		{
			desc:     "no matches",
			input:    []uint32{0, 1},
			expected: []uint32{0, 1},
		},
		{
			desc:     "remove all",
			input:    []uint32{0, 1, 2, 3, 4},
			removals: []uint32{0, 1, 2, 3, 4},
			expected: []uint32{},
		},
		{
			desc:     "remove every other",
			input:    []uint32{0, 1, 2, 3, 4},
			removals: []uint32{0, 2, 4},
			expected: []uint32{1, 3},
		},
		{
			desc:     "remove middle section",
			input:    []uint32{0, 1, 2, 3, 4, 5, 6, 7, 8, 9},
			removals: []uint32{3, 4, 5},
			expected: []uint32{0, 1, 2, 6, 7, 8, 9},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			input := mkInput(tc.input)
			expected := mkInput(tc.expected)

			filterChunkRefsForSeries(input, mkRemovals(tc.removals))

			require.Equal(t, expected, input)
		})
	}
}

func TestFilterChunkRefs(t *testing.T) {
	mkInput := func(nSeries, chunksPerSeries int) *logproto.FilterChunkRefRequest {
		res := &logproto.FilterChunkRefRequest{}
		refs := make([]*logproto.GroupedChunkRefs, nSeries)
		for i := range refs {
			chks := make([]*logproto.ShortRef, chunksPerSeries)
			for j := range chks {
				chks[j] = &logproto.ShortRef{Checksum: uint32(j)}
			}
			refs[i] = &logproto.GroupedChunkRefs{
				Fingerprint: uint64(i),
				Refs:        chks,
			}
		}
		res.Refs = refs
		return res
	}

	type instruction struct {
		fp        uint64
		checksums []uint32
	}
	mkRemovals := func(xs []instruction) []v1.Output {
		out := make([]v1.Output, len(xs))
		for i, x := range xs {
			out[i] = v1.Output{
				Fp:       model.Fingerprint(x.fp),
				Removals: make(v1.ChunkRefs, len(x.checksums)),
			}
			for k, chk := range x.checksums {
				out[i].Removals[k] = v1.ChunkRef{Checksum: chk}
			}
		}
		return out
	}

	mkResult := func(xs []instruction) *logproto.FilterChunkRefRequest {
		out := &logproto.FilterChunkRefRequest{Refs: make([]*logproto.GroupedChunkRefs, len(xs))}
		for i, x := range xs {
			out.Refs[i] = &logproto.GroupedChunkRefs{
				Fingerprint: x.fp,
				Refs:        make([]*logproto.ShortRef, len(x.checksums)),
			}
			for j, c := range x.checksums {
				out.Refs[i].Refs[j] = &logproto.ShortRef{Checksum: c}
			}
		}
		return out
	}

	for _, tc := range []struct {
		desc     string
		input    *logproto.FilterChunkRefRequest
		removals []instruction
		expected *logproto.FilterChunkRefRequest
	}{
		{
			desc:     "no removals",
			input:    mkInput(2, 2),
			expected: mkInput(2, 2),
		},
		{
			desc:  "remove all",
			input: mkInput(2, 2),
			removals: []instruction{
				{fp: 0, checksums: []uint32{0, 1}},
				{fp: 1, checksums: []uint32{0, 1}},
			},
			expected: mkInput(0, 0),
		},
		{
			desc:  "remove every other series",
			input: mkInput(4, 2),
			removals: []instruction{
				{fp: 0, checksums: []uint32{0, 1}},
				{fp: 2, checksums: []uint32{0, 1}},
			},
			expected: mkResult([]instruction{
				{fp: 1, checksums: []uint32{0, 1}},
				{fp: 3, checksums: []uint32{0, 1}},
			}),
		},
		{
			desc:  "remove the last chunk for each series",
			input: mkInput(4, 2),
			removals: []instruction{
				{fp: 0, checksums: []uint32{1}},
				{fp: 1, checksums: []uint32{1}},
				{fp: 2, checksums: []uint32{1}},
				{fp: 3, checksums: []uint32{1}},
			},
			expected: mkResult([]instruction{
				{fp: 0, checksums: []uint32{0}},
				{fp: 1, checksums: []uint32{0}},
				{fp: 2, checksums: []uint32{0}},
				{fp: 3, checksums: []uint32{0}},
			}),
		},
		{
			desc:  "remove the middle chunk for every other series",
			input: mkInput(4, 3),
			removals: []instruction{
				{fp: 0, checksums: []uint32{1}},
				{fp: 2, checksums: []uint32{1}},
			},
			expected: mkResult([]instruction{
				{fp: 0, checksums: []uint32{0, 2}},
				{fp: 1, checksums: []uint32{0, 1, 2}},
				{fp: 2, checksums: []uint32{0, 2}},
				{fp: 3, checksums: []uint32{0, 1, 2}},
			}),
		},
		{
			desc:  "remove the first chunk of the last series",
			input: mkInput(4, 3),
			removals: []instruction{
				{fp: 3, checksums: []uint32{0}},
			},
			expected: mkResult([]instruction{
				{fp: 0, checksums: []uint32{0, 1, 2}},
				{fp: 1, checksums: []uint32{0, 1, 2}},
				{fp: 2, checksums: []uint32{0, 1, 2}},
				{fp: 3, checksums: []uint32{1, 2}},
			}),
		},
		{
			desc:  "duplicate removals",
			input: mkInput(4, 3),
			removals: []instruction{
				{fp: 0, checksums: []uint32{0, 1}},
				{fp: 0, checksums: []uint32{0, 1, 2}},
				{fp: 1, checksums: []uint32{0, 2}},
				{fp: 2, checksums: []uint32{1}},
			},
			expected: mkResult([]instruction{
				{fp: 1, checksums: []uint32{1}},
				{fp: 2, checksums: []uint32{0, 2}},
				{fp: 3, checksums: []uint32{0, 1, 2}},
			}),
		},
		{
			desc:  "unordered fingerprints",
			input: mkInput(4, 3),
			removals: []instruction{
				{fp: 3, checksums: []uint32{2}},
				{fp: 0, checksums: []uint32{1, 2}},
				{fp: 2, checksums: []uint32{1, 2}},
			},
			expected: mkResult([]instruction{
				{fp: 0, checksums: []uint32{0}},
				{fp: 1, checksums: []uint32{0, 1, 2}},
				{fp: 2, checksums: []uint32{0}},
				{fp: 3, checksums: []uint32{0, 1}},
			}),
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			res := filterChunkRefs(tc.input, mkRemovals(tc.removals))
			require.Equal(t, tc.expected.Refs, res)
		})
	}

}

func BenchmarkFilterChunkRefs(b *testing.B) {
	nSeries := 1024
	chunksPerSeries := 10

	mkInput := func() *logproto.FilterChunkRefRequest {
		res := &logproto.FilterChunkRefRequest{}

		refs := make([]*logproto.GroupedChunkRefs, nSeries)
		for i := range refs {
			chks := make([]*logproto.ShortRef, chunksPerSeries)
			for j := range chks {
				chks[j] = &logproto.ShortRef{Checksum: uint32(j)}
			}
			refs[i] = &logproto.GroupedChunkRefs{
				Fingerprint: uint64(i),
				Refs:        chks,
			}
		}
		res.Refs = refs
		return res
	}

	// responses aren't mutated, so we add a pool to mitigate the alloc
	// effect on the benchmark
	var responseP sync.Pool
	mkOutputs := func() *[]v1.Output {
		// remove half the chunks from half the series, so 25% of the volume
		outputs := make([]v1.Output, nSeries/2)
		for i := range outputs {
			output := v1.Output{
				Fp: model.Fingerprint(i * 2),
			}
			for j := 0; j < chunksPerSeries/2; j++ {
				output.Removals = append(output.Removals, v1.ChunkRef{Checksum: uint32(j * 2)})
			}

			outputs[i] = output
		}
		return &outputs
	}
	responseP.New = func() interface{} {
		return mkOutputs()
	}

	// Add comparison functions here to bench side by side
	for _, tc := range []struct {
		desc string
		f    func(req *logproto.FilterChunkRefRequest, responses []v1.Output)
	}{
		{
			desc: "filterChunkRefs",
			f: func(req *logproto.FilterChunkRefRequest, responses []v1.Output) {
				filterChunkRefs(req, responses)
			},
		},
	} {
		b.Run(tc.desc, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				req := mkInput()
				ptr := responseP.Get().(*[]v1.Output)
				resps := *ptr

				tc.f(req, resps)

				responseP.Put(ptr)
			}
		})
	}

}
