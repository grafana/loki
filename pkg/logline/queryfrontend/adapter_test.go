package queryfrontend

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RoaringBitmap/roaring"
	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/logline"
	"github.com/grafana/loki/v3/pkg/logline/format"
	"github.com/grafana/loki/v3/pkg/logline/hintprovider"
	"github.com/grafana/loki/v3/pkg/logline/store"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/queryrange"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
)

func TestFrontendHintAdapter_ProvideHints(t *testing.T) {
	needle := "9fA81cD2Ef0077aa"
	docMin := time.Date(2026, 2, 26, 10, 0, 50, 0, time.UTC)
	docMax := time.Date(2026, 2, 26, 10, 1, 10, 0, time.UTC)
	from := model.TimeFromUnixNano(docMin.Add(-time.Minute).UnixNano())
	through := model.TimeFromUnixNano(docMax.Add(time.Minute).UnixNano())
	expr := mustParseAdapterExpr(t, `{job="api"} |= "9fA81cD2Ef0077aa"`)

	t.Run("packs request and uses remote ranges and stats", func(t *testing.T) {
		indexStore := newAdapterTestStore(t)
		writeAdapterTestIndex(t, indexStore, "aaaaaaaaaaaaaaaa", needle, docMin, docMax)
		provider := newAdapterTestProvider(t, indexStore, 32)

		remoteStart := time.Date(2026, 2, 26, 10, 0, 55, 0, time.UTC)
		remoteEnd := time.Date(2026, 2, 26, 10, 1, 0, 0, time.UTC)
		var gotReq *logproto.LoglineIndexRequest
		adapter := &frontendHintAdapter{
			inner: provider,
			next: queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
				var ok bool
				gotReq, ok = req.(*logproto.LoglineIndexRequest)
				require.True(t, ok)
				return &queryrange.LoglineIndexResponse{
					Response: &logproto.LoglineIndexResponse{
						TimeRanges: []logproto.HintTimeRange{{Start: remoteStart, End: remoteEnd}},
						Stats:      &logproto.HintQueryStats{PeakConcurrency: 7, IndexQueriesPositive: 3},
					},
				}, nil
			}),
		}

		hints, stats, err := adapter.ProvideHints(context.Background(), "test-tenant", expr, from, through)
		require.NoError(t, err)
		require.NotNil(t, gotReq)
		require.Equal(t, from, gotReq.From)
		require.Equal(t, through, gotReq.Through)
		require.Equal(t, expr.String(), gotReq.Expr)
		require.Equal(t, int32(6), gotReq.NgramLength)
		require.Equal(t, int32(32), gotReq.MaxParallel)
		require.Equal(t, hintprovider.ToProtoIndexMetas(indexStore.Snapshot().Active()), gotReq.Indexes)
		require.Equal(t, []hintprovider.HintTimeRange{{Start: remoteStart, End: remoteEnd}}, hints.TimeRanges)
		require.Equal(t, int32(7), stats.Snapshot().PeakConcurrency)
		require.Equal(t, int64(3), stats.Snapshot().IndexQueriesPositive)
	})

	t.Run("skips next when the query is unsupported", func(t *testing.T) {
		indexStore := newAdapterTestStore(t)
		writeAdapterTestIndex(t, indexStore, "aaaaaaaaaaaaaaaa", needle, docMin, docMax)
		adapter := &frontendHintAdapter{
			inner: newAdapterTestProvider(t, indexStore, 32),
			next: queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
				t.Fatal("next should not run for an unsupported query")
				return nil, nil
			}),
		}
		hints, _, err := adapter.ProvideHints(
			context.Background(),
			"test-tenant",
			mustParseAdapterExpr(t, `{job="api"} |~ "error.*"`),
			from,
			through,
		)
		require.ErrorIs(t, err, hintprovider.ErrUnsupported)
		require.NotNil(t, hints)
		require.Empty(t, hints.TimeRanges)
	})

	t.Run("skips next when no indexes overlap", func(t *testing.T) {
		indexStore := newAdapterTestStore(t)
		adapter := &frontendHintAdapter{
			inner: newAdapterTestProvider(t, indexStore, 32),
			next: queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
				t.Fatal("next should not run when the plan has no indexes")
				return nil, nil
			}),
		}
		hints, _, err := adapter.ProvideHints(context.Background(), "test-tenant", expr, from, through)
		require.NoError(t, err)
		require.NotNil(t, hints)
		require.Empty(t, hints.TimeRanges)
	})

	t.Run("errors on unexpected response type", func(t *testing.T) {
		indexStore := newAdapterTestStore(t)
		writeAdapterTestIndex(t, indexStore, "aaaaaaaaaaaaaaaa", needle, docMin, docMax)
		adapter := &frontendHintAdapter{
			inner: newAdapterTestProvider(t, indexStore, 32),
			next: queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
				return &queryrange.LokiResponse{}, nil
			}),
		}
		_, _, err := adapter.ProvideHints(context.Background(), "test-tenant", expr, from, through)
		require.Error(t, err)
		require.Contains(t, err.Error(), "unexpected hint response type")
	})

	t.Run("keeps pre-min-date passthrough with remote ranges", func(t *testing.T) {
		indexStore := newAdapterTestStoreWithMinDate(t, "2026-02-26")
		writeAdapterTestIndex(t, indexStore, "cccccccccccccccc", needle, docMin, docMax)
		remoteStart := time.Date(2026, 2, 26, 10, 0, 55, 0, time.UTC)
		remoteEnd := time.Date(2026, 2, 26, 10, 1, 0, 0, time.UTC)
		adapter := &frontendHintAdapter{
			inner: newAdapterTestProvider(t, indexStore, 32),
			next: queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
				got, ok := req.(*logproto.LoglineIndexRequest)
				require.True(t, ok)
				require.NotEmpty(t, got.Indexes)
				return &queryrange.LoglineIndexResponse{
					Response: &logproto.LoglineIndexResponse{
						TimeRanges: []logproto.HintTimeRange{{Start: remoteStart, End: remoteEnd}},
					},
				}, nil
			}),
		}

		windowFrom := time.Date(2026, 2, 25, 18, 0, 0, 0, time.UTC)
		windowThrough := time.Date(2026, 2, 26, 11, 0, 0, 0, time.UTC)
		hints, _, err := adapter.ProvideHints(
			context.Background(),
			"test-tenant",
			expr,
			model.TimeFromUnixNano(windowFrom.UnixNano()),
			model.TimeFromUnixNano(windowThrough.UnixNano()),
		)
		require.NoError(t, err)
		require.Len(t, hints.TimeRanges, 2)
		require.True(t, hints.TimeRanges[0].IsPassthrough())
		require.Equal(t, time.Date(2026, 2, 26, 0, 0, 0, 0, time.UTC), hints.TimeRanges[0].End)
		require.Equal(t, remoteStart, hints.TimeRanges[1].Start)
		require.Equal(t, remoteEnd, hints.TimeRanges[1].End)
	})

	t.Run("runs QueryHints through next like the querier handler", func(t *testing.T) {
		indexStore := newAdapterTestStore(t)
		writeAdapterTestIndex(t, indexStore, "aaaaaaaaaaaaaaaa", needle, docMin, docMax)
		provider := newAdapterTestProvider(t, indexStore, 32)

		local, _, err := provider.ProvideHints(context.Background(), "test-tenant", expr, from, through)
		require.NoError(t, err)

		adapter := &frontendHintAdapter{inner: provider, next: queryHintsNext(t, provider)}
		remote, _, err := adapter.ProvideHints(context.Background(), "test-tenant", expr, from, through)
		require.NoError(t, err)
		requireEqualAdapterHintBounds(t, local.TimeRanges, remote.TimeRanges)
	})

	t.Run("QueryHints next keeps pre-min-date passthrough with looked-up ranges", func(t *testing.T) {
		indexStore := newAdapterTestStoreWithMinDate(t, "2026-02-26")
		writeAdapterTestIndex(t, indexStore, "cccccccccccccccc", needle, docMin, docMax)
		provider := newAdapterTestProvider(t, indexStore, 32)

		windowFrom := model.TimeFromUnixNano(time.Date(2026, 2, 25, 18, 0, 0, 0, time.UTC).UnixNano())
		windowThrough := model.TimeFromUnixNano(time.Date(2026, 2, 26, 11, 0, 0, 0, time.UTC).UnixNano())
		local, _, err := provider.ProvideHints(context.Background(), "test-tenant", expr, windowFrom, windowThrough)
		require.NoError(t, err)

		adapter := &frontendHintAdapter{inner: provider, next: queryHintsNext(t, provider)}
		remote, _, err := adapter.ProvideHints(context.Background(), "test-tenant", expr, windowFrom, windowThrough)
		require.NoError(t, err)
		requireEqualAdapterHintBounds(t, local.TimeRanges, remote.TimeRanges)
		require.True(t, remote.TimeRanges[0].IsPassthrough())
	})
}

func queryHintsNext(t *testing.T, p *hintprovider.LoglineHintProvider) queryrangebase.Handler {
	t.Helper()
	return queryrangebase.HandlerFunc(func(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		indexReq, ok := req.(*logproto.LoglineIndexRequest)
		require.True(t, ok)
		parsed, err := syntax.ParseExpr(indexReq.Expr)
		if err != nil {
			return nil, err
		}
		resp, err := p.QueryHints(ctx, parsed, indexReq)
		if err != nil {
			return nil, err
		}
		return &queryrange.LoglineIndexResponse{Response: resp}, nil
	})
}

func requireEqualAdapterHintBounds(t *testing.T, want, got []hintprovider.HintTimeRange) {
	t.Helper()
	require.Len(t, got, len(want))
	for i := range want {
		require.Equal(t, want[i].Start, got[i].Start, "range %d start", i)
		require.Equal(t, want[i].End, got[i].End, "range %d end", i)
	}
}

func mustParseAdapterExpr(t *testing.T, query string) syntax.Expr {
	t.Helper()
	expr, err := syntax.ParseExpr(query)
	require.NoError(t, err)
	return expr
}

func newAdapterTestProvider(t *testing.T, indexStore *store.Store, maxParallel int) *hintprovider.LoglineHintProvider {
	t.Helper()
	provider, err := hintprovider.NewLoglineHintProvider(indexStore, 6, maxParallel, nil, log.NewNopLogger(), nil)
	require.NoError(t, err)
	return provider
}

func newAdapterTestStore(t *testing.T) *store.Store {
	t.Helper()
	return newAdapterTestStoreWithMinDate(t, "0001-01-01")
}

func newAdapterTestStoreWithMinDate(t *testing.T, minDate string) *store.Store {
	t.Helper()
	bucket := objstore.NewInMemBucket()
	indexStore, err := store.NewStore(bucket, store.Config{MinDate: minDate}, log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)
	return indexStore
}

func writeAdapterTestIndex(t *testing.T, indexStore *store.Store, hash, needle string, docMin, docMax time.Time) {
	t.Helper()
	indexBytes, headerInfo := buildAdapterIndexBytes(t, needle, docMin, docMax)
	meta := store.Meta{
		Date:        docMin.UTC().Format("2006-01-02"),
		Hash:        hash,
		Version:     logline.CurrentVersion,
		MinLogTs:    docMin.UTC(),
		MaxLogTs:    docMax.UTC(),
		MinRecordTs: docMin.UTC(),
		MaxRecordTs: docMax.UTC(),
		IndexHeader: headerInfo,
		SizeBytes:   int64(len(indexBytes)),
	}
	require.NoError(t, indexStore.PutIndex(context.Background(), bytes.NewReader(indexBytes), meta))
	require.NoError(t, indexStore.Poll(context.Background()))
}

func buildAdapterIndexBytes(t *testing.T, needle string, docMin, docMax time.Time) ([]byte, *format.HeaderInfo) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.lidx")
	docs := []format.DocumentMetadata{
		{ID: 0, MinTimeUnix: docMin.UnixMilli(), MaxTimeUnix: docMax.UnixMilli()},
	}
	writer, err := logline.NewWriter(logline.CurrentVersion, path, docs, nil)
	require.NoError(t, err)

	terms, err := hintprovider.ExtractQueryNgrams(needle, 6, logline.CurrentVersion)
	require.NoError(t, err)
	require.NotEmpty(t, terms)

	bm := roaring.New()
	bm.Add(0)
	for _, term := range terms {
		var key [8]byte
		copy(key[:], term)
		require.NoError(t, writer.WriteTermBitmap(key, format.Bitmap{Roaring: bm}))
	}
	require.NoError(t, writer.Close())

	reader, _, err := logline.OpenFile(path)
	require.NoError(t, err)
	info := reader.ReadHeader()
	require.NoError(t, reader.Close())

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return data, &info
}
