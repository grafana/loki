package tsdb

import (
	"context"
	"fmt"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/tsdb/chunks"
	"github.com/prometheus/prometheus/tsdb/record"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
)

type noopTSDBManager struct{ NoopIndex }

func (noopTSDBManager) BuildFromWALs(_ time.Time, _ []WALIdentifier) error { return nil }
func (noopTSDBManager) Start() error                                       { return nil }

func chunkMetasToChunkRefs(user string, fp uint64, xs index.ChunkMetas) (res []ChunkRef) {
	for _, x := range xs {
		res = append(res, ChunkRef{
			User:        user,
			Fingerprint: model.Fingerprint(fp),
			Start:       x.From(),
			End:         x.Through(),
			Checksum:    x.Checksum,
		})
	}
	return
}

// Test append
func Test_TenantHeads_Append(t *testing.T) {
	h := newTenantHeads(time.Now(), defaultHeadManagerStripeSize, NewMetrics(nil), log.NewNopLogger())
	ls := mustParseLabels(`{foo="bar"}`)
	chks := []index.ChunkMeta{
		{
			Checksum: 0,
			MinTime:  1,
			MaxTime:  10,
			KB:       2,
			Entries:  30,
		},
	}
	_ = h.Append("fake", ls, chks)

	found, err := h.GetChunkRefs(
		context.Background(),
		"fake",
		0,
		100,
		nil, nil,
		labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"),
	)
	require.Nil(t, err)
	require.Equal(t, chunkMetasToChunkRefs("fake", ls.Hash(), chks), found)

}

// Test multitenant reads
func Test_TenantHeads_MultiRead(t *testing.T) {
	h := newTenantHeads(time.Now(), defaultHeadManagerStripeSize, NewMetrics(nil), log.NewNopLogger())
	ls := mustParseLabels(`{foo="bar"}`)
	chks := []index.ChunkMeta{
		{
			Checksum: 0,
			MinTime:  1,
			MaxTime:  10,
			KB:       2,
			Entries:  30,
		},
	}

	tenants := []struct {
		user string
		ls   labels.Labels
	}{
		{
			user: "tenant1",
			ls: append(ls.Copy(), labels.Label{
				Name:  "tenant",
				Value: "tenant1",
			}),
		},
		{
			user: "tenant2",
			ls: append(ls.Copy(), labels.Label{
				Name:  "tenant",
				Value: "tenant2",
			}),
		},
	}

	// add data for both tenants
	for _, tenant := range tenants {
		_ = h.Append(tenant.user, tenant.ls, chks)

	}

	// ensure we're only returned the data from the correct tenant
	for _, tenant := range tenants {
		found, err := h.GetChunkRefs(
			context.Background(),
			tenant.user,
			0,
			100,
			nil, nil,
			labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"),
		)
		require.Nil(t, err)
		require.Equal(t, chunkMetasToChunkRefs(tenant.user, tenant.ls.Hash(), chks), found)
	}

}

// test head recover from wal
func Test_HeadManager_RecoverHead(t *testing.T) {
	now := time.Now()
	dir := t.TempDir()
	cases := []struct {
		Labels labels.Labels
		Chunks []index.ChunkMeta
		User   string
	}{
		{
			User:   "tenant1",
			Labels: mustParseLabels(`{foo="bar", bazz="buzz"}`),
			Chunks: []index.ChunkMeta{
				{
					MinTime:  1,
					MaxTime:  10,
					Checksum: 3,
				},
			},
		},
		{
			User:   "tenant2",
			Labels: mustParseLabels(`{foo="bard", bazz="bozz", bonk="borb"}`),
			Chunks: []index.ChunkMeta{
				{
					MinTime:  1,
					MaxTime:  7,
					Checksum: 4,
				},
			},
		},
	}

	mgr := NewHeadManager(log.NewNopLogger(), dir, nil, noopTSDBManager{})
	// This bit is normally handled by the Start() fn, but we're testing a smaller surface area
	// so ensure our dirs exist
	for _, d := range managerRequiredDirs(dir) {
		require.Nil(t, util.EnsureDirectory(d))
	}

	// Call Rotate() to ensure the new head tenant heads exist, etc
	require.Nil(t, mgr.Rotate(now))

	// now build a WAL independently to test recovery
	w, err := newHeadWAL(log.NewNopLogger(), walPath(mgr.dir, now), now)
	require.Nil(t, err)

	for i, c := range cases {
		require.Nil(t, w.Log(&WALRecord{
			UserID: c.User,
			Series: record.RefSeries{
				Ref:    chunks.HeadSeriesRef(i),
				Labels: c.Labels,
			},
			Chks: ChunkMetasRecord{
				Chks: c.Chunks,
				Ref:  uint64(i),
			},
		}))
	}

	require.Nil(t, w.Stop())

	grp, ok, err := walsForPeriod(mgr.dir, mgr.period, mgr.period.PeriodFor(now))
	require.Nil(t, err)
	require.True(t, ok)
	require.Equal(t, 1, len(grp.wals))
	require.Nil(t, recoverHead(mgr.dir, mgr.activeHeads, grp.wals))

	for _, c := range cases {
		refs, err := mgr.GetChunkRefs(
			context.Background(),
			c.User,
			0, math.MaxInt64,
			nil, nil,
			labels.MustNewMatcher(labels.MatchRegexp, "foo", ".+"),
		)
		require.Nil(t, err)
		require.Equal(t, chunkMetasToChunkRefs(c.User, c.Labels.Hash(), c.Chunks), refs)
	}

}

// test mgr recover from multiple wals across multiple periods
func Test_HeadManager_Lifecycle(t *testing.T) {
	dir := t.TempDir()
	curPeriod := time.Now()
	cases := []struct {
		Labels labels.Labels
		Chunks []index.ChunkMeta
		User   string
	}{
		{
			User:   "tenant1",
			Labels: mustParseLabels(`{foo="bar", bazz="buzz"}`),
			Chunks: []index.ChunkMeta{
				{
					MinTime:  1,
					MaxTime:  10,
					Checksum: 3,
				},
			},
		},
		{
			User:   "tenant2",
			Labels: mustParseLabels(`{foo="bard", bazz="bozz", bonk="borb"}`),
			Chunks: []index.ChunkMeta{
				{
					MinTime:  1,
					MaxTime:  7,
					Checksum: 4,
				},
			},
		},
	}

	mgr := NewHeadManager(log.NewNopLogger(), dir, nil, noopTSDBManager{})
	w, err := newHeadWAL(log.NewNopLogger(), walPath(mgr.dir, curPeriod), curPeriod)
	require.Nil(t, err)

	// Write old WALs
	for i, c := range cases {
		require.Nil(t, w.Log(&WALRecord{
			UserID: c.User,
			Series: record.RefSeries{
				Ref:    chunks.HeadSeriesRef(i),
				Labels: c.Labels,
			},
			Chks: ChunkMetasRecord{
				Chks: c.Chunks,
				Ref:  uint64(i),
			},
		}))
	}

	require.Nil(t, w.Stop())

	// Start, ensuring recovery from old WALs
	require.Nil(t, mgr.Start())
	// Ensure old WAL data is queryable
	for _, c := range cases {
		refs, err := mgr.GetChunkRefs(
			context.Background(),
			c.User,
			0, math.MaxInt64,
			nil, nil,
			labels.MustNewMatcher(labels.MatchRegexp, "foo", ".+"),
		)
		require.Nil(t, err)

		lbls := labels.NewBuilder(c.Labels)
		lbls.Set(TenantLabel, c.User)
		require.Equal(t, chunkMetasToChunkRefs(c.User, c.Labels.Hash(), c.Chunks), refs)
	}

	// Add data
	newCase := struct {
		Labels labels.Labels
		Chunks []index.ChunkMeta
		User   string
	}{
		User:   "tenant3",
		Labels: mustParseLabels(`{foo="bard", other="hi"}`),
		Chunks: []index.ChunkMeta{
			{
				MinTime:  1,
				MaxTime:  7,
				Checksum: 4,
			},
		},
	}

	require.Nil(t, mgr.Append(newCase.User, newCase.Labels, newCase.Chunks))

	// Ensure old + new data is queryable
	for _, c := range append(cases, newCase) {
		refs, err := mgr.GetChunkRefs(
			context.Background(),
			c.User,
			0, math.MaxInt64,
			nil, nil,
			labels.MustNewMatcher(labels.MatchRegexp, "foo", ".+"),
		)
		require.Nil(t, err)

		lbls := labels.NewBuilder(c.Labels)
		lbls.Set(TenantLabel, c.User)
		require.Equal(t, chunkMetasToChunkRefs(c.User, c.Labels.Hash(), c.Chunks), refs)
	}
}

func BenchmarkTenantHeads(b *testing.B) {
	for _, tc := range []struct {
		readers, writers int
	}{
		{
			readers: 10,
		},
		{
			readers: 100,
		},
		{
			readers: 1000,
		},
	} {
		b.Run(fmt.Sprintf("%d", tc.readers), func(b *testing.B) {
			heads := newTenantHeads(time.Now(), defaultHeadManagerStripeSize, NewMetrics(nil), log.NewNopLogger())
			// 1000 series across 100 tenants
			nTenants := 10
			for i := 0; i < 1000; i++ {
				tenant := i % nTenants
				ls := mustParseLabels(fmt.Sprintf(`{foo="bar", i="%d"}`, i))
				heads.Append(fmt.Sprint(tenant), ls, index.ChunkMetas{
					{},
				})
			}

			for n := 0; n < b.N; n++ {
				var wg sync.WaitGroup
				for r := 0; r < tc.readers; r++ {
					wg.Add(1)
					go func(r int) {
						defer wg.Done()
						var res []ChunkRef
						tenant := r % nTenants

						// nolint:ineffassign,staticcheck
						res, _ = heads.GetChunkRefs(
							context.Background(),
							fmt.Sprint(tenant),
							0, math.MaxInt64,
							res,
							nil,
							labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"),
						)
					}(r)
				}

				wg.Wait()
			}

		})
	}
}
