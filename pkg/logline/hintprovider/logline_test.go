package hintprovider

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/RoaringBitmap/roaring"
	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/logql/syntax"

	"github.com/grafana/loki/v3/pkg/logline"
	"github.com/grafana/loki/v3/pkg/logline/format"
	"github.com/grafana/loki/v3/pkg/logline/shard"
	"github.com/grafana/loki/v3/pkg/logline/store"
)

func TestLoglineHintProvider_ProvideHints(t *testing.T) {
	indexStore := newTestStore(t)
	needle := "9fA81cD2Ef0077aa"
	docMin := time.Date(2026, 2, 26, 10, 0, 50, 0, time.UTC)
	docMax := time.Date(2026, 2, 26, 10, 1, 10, 0, time.UTC)
	writeTestIndex(t, indexStore, "aaaaaaaaaaaaaaaa", needle, docMin, docMax)

	provider, err := NewLoglineHintProvider(indexStore, 6, 0, nil, log.NewNopLogger(), nil)
	require.NoError(t, err)

	expr := mustParseExpr(t, `{job="api"} |= "9fA81cD2Ef0077aa"`)
	hints, _, err := provider.ProvideHints(
		context.Background(),
		"test-tenant",
		expr,
		model.TimeFromUnixNano(docMin.Add(-time.Minute).UnixNano()),
		model.TimeFromUnixNano(docMax.Add(time.Minute).UnixNano()),
	)
	require.NoError(t, err)
	require.NotNil(t, hints)
	require.Len(t, hints.TimeRanges, 1)
	require.Equal(t, docMin.UTC(), hints.TimeRanges[0].Start)
	require.Equal(t, docMax.UTC(), hints.TimeRanges[0].End)
	require.Contains(t, hints.TimeRanges[0].Source, "index=")
	require.Contains(t, hints.TimeRanges[0].Source, ",doc=0")
	require.Contains(t, hints.TimeRanges[0].Source, ",min=")
	require.Contains(t, hints.TimeRanges[0].Source, ",max=")
}

func TestLoglineHintProvider_ProvideHints_MatchesAllPreservesSingleTimestamp(t *testing.T) {
	// Common terms may be stored as "matches all" without listing their
	// documents. If every log in that index has the same timestamp, its first
	// and last log times are equal. That timestamp is a real match, so the hint
	// must preserve a small window around it instead of dropping it as empty.
	indexStore := newTestStore(t)
	needle := "9fA81cD2Ef0077aa"
	logTS := time.Date(2026, 2, 26, 10, 0, 50, 0, time.UTC)
	writeMatchesAllTestIndex(t, indexStore, "eeeeeeeeeeeeeeee", needle, logTS)

	provider, err := NewLoglineHintProvider(indexStore, 6, 0, nil, log.NewNopLogger(), nil)
	require.NoError(t, err)

	expr := mustParseExpr(t, `{job="api"} |= "9fA81cD2Ef0077aa"`)
	hints, _, err := provider.ProvideHints(
		context.Background(),
		"test-tenant",
		expr,
		model.TimeFromUnixNano(logTS.Add(-time.Minute).UnixNano()),
		model.TimeFromUnixNano(logTS.Add(time.Minute).UnixNano()),
	)
	require.NoError(t, err)
	require.NotNil(t, hints)
	require.Len(t, hints.TimeRanges, 1)
	require.Equal(t, logTS, hints.TimeRanges[0].Start)
	require.Equal(t, logTS.Add(time.Millisecond), hints.TimeRanges[0].End)
}

func TestLoglineHintProvider_ProvideHints_RecordsQueryStats(t *testing.T) {
	indexStore := newTestStore(t)
	needle := "9fA81cD2Ef0077aa"
	docMin := time.Date(2026, 2, 26, 10, 0, 50, 0, time.UTC)
	docMax := time.Date(2026, 2, 26, 10, 1, 10, 0, time.UTC)
	writeTestIndex(t, indexStore, "ffffffffffffffff", needle, docMin, docMax)

	provider, err := NewLoglineHintProvider(indexStore, 6, 0, nil, log.NewNopLogger(), nil)
	require.NoError(t, err)

	expr := mustParseExpr(t, `{job="api"} |= "9fA81cD2Ef0077aa"`)
	_, stats, err := provider.ProvideHints(
		context.Background(),
		"test-tenant",
		expr,
		model.TimeFromUnixNano(docMin.Add(-time.Minute).UnixNano()),
		model.TimeFromUnixNano(docMax.Add(time.Minute).UnixNano()),
	)
	require.NoError(t, err)
	require.NotNil(t, stats)

	snap := stats.Snapshot()
	require.Greater(t, snap.ObjectStorageRequests, int64(0))
	require.Greater(t, snap.TotalIOBytes, int64(0))
	require.GreaterOrEqual(t, snap.TermDictReads, int64(1))
	require.GreaterOrEqual(t, snap.BitmapReads, int64(1))
	require.GreaterOrEqual(t, snap.PeakConcurrency, int32(1))
	require.Equal(t, int64(1), snap.MetadataCacheMisses)
	require.Equal(t, int64(0), snap.HeaderCacheMisses)
}

func TestLoglineHintProvider_ExecuteQuery_ObservesQueryMultiple(t *testing.T) {
	indexStore := newTestStore(t)
	needle := "9fA81cD2Ef0077aa"
	docMin := time.Date(2026, 2, 26, 10, 0, 50, 0, time.UTC)
	docMax := time.Date(2026, 2, 26, 10, 1, 10, 0, time.UTC)
	writeTestIndex(t, indexStore, "abababababababab", needle, docMin, docMax)

	var observedReason string
	var observedTermBatches int
	var observedCalls int
	provider, err := NewLoglineHintProvider(indexStore, 6, 0, func(reason string, termBatchesProcessed int) {
		observedReason = reason
		observedTermBatches = termBatchesProcessed
		observedCalls++
	}, log.NewNopLogger(), nil)
	require.NoError(t, err)

	stats := NewQueryStats()
	active := indexStore.Snapshot().Active()
	require.Len(t, active, 1)

	shardRanges, err := provider.executeQuery(context.Background(), []string{"QQQQQQ"}, active, stats)
	require.NoError(t, err)
	require.Empty(t, shardRanges)

	snap := stats.Snapshot()
	require.Equal(t, int64(1), snap.IndexQueriesTotal)
	require.Equal(t, int64(1), snap.IndexQueriesTermMiss)
	require.Equal(t, int64(0), snap.IndexQueriesEmptyAnd)
	require.Equal(t, int64(0), snap.IndexQueriesPositive)
	require.Equal(t, int64(1), snap.TotalTermBatchesProcessed)
	require.Equal(t, 1, observedCalls)
	require.Equal(t, "term_miss", observedReason)
	require.Equal(t, 1, observedTermBatches)
}

func TestLoglineHintProvider_OpenIndexReader_ErrorWhenMetaHeaderMissing(t *testing.T) {
	indexStore := newTestStore(t)
	provider, err := NewLoglineHintProvider(indexStore, 6, 0, nil, log.NewNopLogger(), nil)
	require.NoError(t, err)

	stats := NewQueryStats()
	meta := store.Meta{
		Date:      "2026-02-26",
		Hash:      "eeeeffffffffeeee",
		Version:   "v3",
		SizeBytes: 1,
	}
	_, err = provider.openIndexReader(context.Background(), meta, stats)
	require.Error(t, err)

	snap := stats.Snapshot()
	require.Equal(t, int64(1), snap.MetadataCacheMisses)
	require.Equal(t, int64(0), snap.HeaderCacheMisses)
}

func TestLoglineHintProvider_UnsupportedQuery(t *testing.T) {
	indexStore := newTestStore(t)
	provider, err := NewLoglineHintProvider(indexStore, 6, 0, nil, log.NewNopLogger(), nil)
	require.NoError(t, err)

	expr := mustParseExpr(t, `{job="api"} |~ "error.*"`)
	hints, _, err := provider.ProvideHints(
		context.Background(),
		"test-tenant",
		expr,
		model.TimeFromUnixNano(time.Now().Add(-time.Hour).UnixNano()),
		model.TimeFromUnixNano(time.Now().UnixNano()),
	)
	require.Nil(t, hints)
	require.ErrorIs(t, err, ErrUnsupported)
}

func TestLoglineHintProvider_ProvideHints_PostParserJSONLabelFilter(t *testing.T) {
	indexStore := newTestStore(t)
	needle := "grafana_slo_app-klu4xpj1w5lmbmvi8u6ec"
	docMin := time.Date(2026, 2, 26, 10, 0, 50, 0, time.UTC)
	docMax := time.Date(2026, 2, 26, 10, 1, 10, 0, time.UTC)
	writeTestIndex(t, indexStore, "aaaaaaaaaaaaaaaa", needle, docMin, docMax)

	provider, err := NewLoglineHintProvider(indexStore, 6, 0, nil, log.NewNopLogger(), nil)
	require.NoError(t, err)

	expr := mustParseExpr(t, `{job="api"} | json | dashboardUID="grafana_slo_app-klu4xpj1w5lmbmvi8u6ec"`)
	hints, _, err := provider.ProvideHints(
		context.Background(),
		"test-tenant",
		expr,
		model.TimeFromUnixNano(docMin.Add(-time.Minute).UnixNano()),
		model.TimeFromUnixNano(docMax.Add(time.Minute).UnixNano()),
	)
	require.NoError(t, err)
	require.NotNil(t, hints)
	require.Len(t, hints.TimeRanges, 1)
	require.Equal(t, docMin.UTC(), hints.TimeRanges[0].Start)
	require.Equal(t, docMax.UTC(), hints.TimeRanges[0].End)
}

func TestLoglineHintProvider_ProvideHints_LabelFilter(t *testing.T) {
	indexStore := newTestStore(t)
	needle := "9fA81cD2Ef0077aa"
	docMin := time.Date(2026, 2, 26, 10, 0, 50, 0, time.UTC)
	docMax := time.Date(2026, 2, 26, 10, 1, 10, 0, time.UTC)
	writeTestIndex(t, indexStore, "aaaaaaaaaaaaaaaa", needle, docMin, docMax)

	provider, err := NewLoglineHintProvider(indexStore, 6, 0, nil, log.NewNopLogger(), nil)
	require.NoError(t, err)

	expr := mustParseExpr(t, `{job="api"} | trace_id="9fA81cD2Ef0077aa"`)
	hints, _, err := provider.ProvideHints(
		context.Background(),
		"test-tenant",
		expr,
		model.TimeFromUnixNano(docMin.Add(-time.Minute).UnixNano()),
		model.TimeFromUnixNano(docMax.Add(time.Minute).UnixNano()),
	)
	require.NoError(t, err)
	require.NotNil(t, hints)
	require.Len(t, hints.TimeRanges, 1)
	require.Equal(t, docMin.UTC(), hints.TimeRanges[0].Start)
	require.Equal(t, docMax.UTC(), hints.TimeRanges[0].End)
}

func TestLoglineHintProvider_ProvideHints_LabelFilterNoMatches(t *testing.T) {
	indexStore := newTestStore(t)
	needle := "9fA81cD2Ef0077aa"
	docMin := time.Date(2026, 2, 26, 10, 0, 50, 0, time.UTC)
	docMax := time.Date(2026, 2, 26, 10, 1, 10, 0, time.UTC)
	writeTestIndex(t, indexStore, "bbbbbbbbbbbbbbbb", needle, docMin, docMax)

	provider, err := NewLoglineHintProvider(indexStore, 6, 0, nil, log.NewNopLogger(), nil)
	require.NoError(t, err)

	expr := mustParseExpr(t, `{job="api"} | trace_id="differentneedlevalue"`)
	hints, _, err := provider.ProvideHints(
		context.Background(),
		"test-tenant",
		expr,
		model.TimeFromUnixNano(docMin.Add(-time.Minute).UnixNano()),
		model.TimeFromUnixNano(docMax.Add(time.Minute).UnixNano()),
	)
	require.NoError(t, err)
	require.NotNil(t, hints)
	require.Empty(t, hints.TimeRanges)
}

func TestLoglineHintProvider_ProvideHints_LineAndLabelFilterAND(t *testing.T) {
	indexStore := newTestStore(t)
	lineNeedle := "9fA81cD2Ef0077aa"
	labelNeedle := "traceidvalue99"
	docMin := time.Date(2026, 2, 26, 10, 0, 50, 0, time.UTC)
	docMax := time.Date(2026, 2, 26, 10, 1, 10, 0, time.UTC)

	// Index contains only the line needle.
	writeTestIndex(t, indexStore, "cccccccccccccccc", lineNeedle, docMin, docMax)

	provider, err := NewLoglineHintProvider(indexStore, 6, 0, nil, log.NewNopLogger(), nil)
	require.NoError(t, err)

	// Both needles required: label miss should yield no ranges.
	expr := mustParseExpr(t, fmt.Sprintf(
		`{job="api"} |= %q | trace_id=%q`,
		lineNeedle, labelNeedle,
	))
	hints, _, err := provider.ProvideHints(
		context.Background(),
		"test-tenant",
		expr,
		model.TimeFromUnixNano(docMin.Add(-time.Minute).UnixNano()),
		model.TimeFromUnixNano(docMax.Add(time.Minute).UnixNano()),
	)
	require.NoError(t, err)
	require.NotNil(t, hints)
	require.Empty(t, hints.TimeRanges)

	// Index both needles in one block (same n-gram space).
	writeTestIndex(t, indexStore, "dddddddddddddddd", lineNeedle+labelNeedle, docMin, docMax)
	require.NoError(t, indexStore.Poll(context.Background()))

	hints, _, err = provider.ProvideHints(
		context.Background(),
		"test-tenant",
		expr,
		model.TimeFromUnixNano(docMin.Add(-time.Minute).UnixNano()),
		model.TimeFromUnixNano(docMax.Add(time.Minute).UnixNano()),
	)
	require.NoError(t, err)
	require.NotNil(t, hints)
	require.NotEmpty(t, hints.TimeRanges)
}

func TestLoglineHintProvider_NoMatches(t *testing.T) {
	indexStore := newTestStore(t)
	needle := "9fA81cD2Ef0077aa"
	docMin := time.Date(2026, 2, 26, 10, 0, 50, 0, time.UTC)
	docMax := time.Date(2026, 2, 26, 10, 1, 10, 0, time.UTC)
	writeTestIndex(t, indexStore, "bbbbbbbbbbbbbbbb", needle, docMin, docMax)

	provider, err := NewLoglineHintProvider(indexStore, 6, 0, nil, log.NewNopLogger(), nil)
	require.NoError(t, err)

	expr := mustParseExpr(t, `{job="api"} |= "differentneedlevalue"`)
	hints, _, err := provider.ProvideHints(
		context.Background(),
		"test-tenant",
		expr,
		model.TimeFromUnixNano(docMin.Add(-time.Minute).UnixNano()),
		model.TimeFromUnixNano(docMax.Add(time.Minute).UnixNano()),
	)
	require.NoError(t, err)
	require.NotNil(t, hints)
	require.Empty(t, hints.TimeRanges)
}

func TestLoglineHintProvider_ProvideHints_PrependsPreMinDateRange(t *testing.T) {
	minDate := "2026-02-26"
	indexStore := newTestStoreWithMinDate(t, minDate)
	needle := "9fA81cD2Ef0077aa"
	docMin := time.Date(2026, 2, 26, 10, 0, 50, 0, time.UTC)
	docMax := time.Date(2026, 2, 26, 10, 1, 10, 0, time.UTC)
	writeTestIndex(t, indexStore, "cccccccccccccccc", needle, docMin, docMax)

	provider, err := NewLoglineHintProvider(indexStore, 6, 0, nil, log.NewNopLogger(), nil)
	require.NoError(t, err)

	expr := mustParseExpr(t, `{job="api"} |= "9fA81cD2Ef0077aa"`)
	from := time.Date(2026, 2, 25, 18, 0, 0, 0, time.UTC)
	through := time.Date(2026, 2, 26, 11, 0, 0, 0, time.UTC)
	hints, _, err := provider.ProvideHints(
		context.Background(),
		"test-tenant",
		expr,
		model.TimeFromUnixNano(from.UnixNano()),
		model.TimeFromUnixNano(through.UnixNano()),
	)
	require.NoError(t, err)
	require.NotNil(t, hints)
	require.Len(t, hints.TimeRanges, 2)

	expectedMinDate, err := time.Parse("2006-01-02", minDate)
	require.NoError(t, err)
	require.Equal(t, time.Time{}, hints.TimeRanges[0].Start)
	require.Equal(t, expectedMinDate.UTC(), hints.TimeRanges[0].End)
	require.Equal(t, HintSourcePreMinDate, hints.TimeRanges[0].Source)
	require.True(t, hints.TimeRanges[0].IsPassthrough())

	require.Equal(t, docMin.UTC(), hints.TimeRanges[1].Start)
	require.Equal(t, docMax.UTC(), hints.TimeRanges[1].End)
}

func TestNormalizeRanges(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	m := time.Minute

	tests := []struct {
		name     string
		input    []HintTimeRange
		expected []HintTimeRange
	}{
		{
			name:     "nil input",
			input:    nil,
			expected: nil,
		},
		{
			name:     "empty input",
			input:    []HintTimeRange{},
			expected: nil,
		},
		{
			name: "single range",
			input: []HintTimeRange{{
				Start: t0,
				End:   t0.Add(5 * m),
			}},
			expected: []HintTimeRange{{
				Start: t0,
				End:   t0.Add(5 * m),
			}},
		},
		{
			name: "non-overlapping",
			input: []HintTimeRange{
				{
					Start: t0,
					End:   t0.Add(5 * m),
				},
				{
					Start: t0.Add(10 * m),
					End:   t0.Add(15 * m),
				},
			},
			expected: []HintTimeRange{
				{
					Start: t0,
					End:   t0.Add(5 * m),
				},
				{
					Start: t0.Add(10 * m),
					End:   t0.Add(15 * m),
				},
			},
		},
		{
			name: "overlapping merged",
			input: []HintTimeRange{
				{
					Start: t0,
					End:   t0.Add(10 * m),
				},
				{
					Start: t0.Add(5 * m),
					End:   t0.Add(15 * m),
				},
			},
			expected: []HintTimeRange{
				{
					Start: t0,
					End:   t0.Add(15 * m),
				},
			},
		},
		{
			name: "adjacent merged",
			input: []HintTimeRange{
				{
					Start: t0,
					End:   t0.Add(5 * m),
				},
				{
					Start: t0.Add(5 * m),
					End:   t0.Add(10 * m),
				},
			},
			expected: []HintTimeRange{
				{
					Start: t0,
					End:   t0.Add(10 * m),
				},
			},
		},
		{
			name: "empty ranges dropped",
			input: []HintTimeRange{
				{Start: t0, End: t0},
				{Start: t0.Add(5 * m), End: t0.Add(10 * m)},
				{Start: t0.Add(20 * m), End: t0.Add(15 * m)}, // inverted
			},
			expected: []HintTimeRange{
				{Start: t0.Add(5 * m), End: t0.Add(10 * m)},
			},
		},
		{
			name: "contained range absorbed",
			input: []HintTimeRange{
				{
					Start: t0,
					End:   t0.Add(20 * m),
				},
				{
					Start: t0.Add(5 * m),
					End:   t0.Add(10 * m),
				},
			},
			expected: []HintTimeRange{
				{
					Start: t0,
					End:   t0.Add(20 * m),
				},
			},
		},
		{
			name: "duplicates merged",
			input: []HintTimeRange{
				{
					Start: t0,
					End:   t0.Add(5 * m),
				},
				{
					Start: t0,
					End:   t0.Add(5 * m),
				},
			},
			expected: []HintTimeRange{
				{
					Start: t0,
					End:   t0.Add(5 * m),
				},
			},
		},
		{
			name: "unsorted input",
			input: []HintTimeRange{
				{
					Start: t0.Add(10 * m),
					End:   t0.Add(15 * m),
				},
				{
					Start: t0,
					End:   t0.Add(12 * m),
				},
			},
			expected: []HintTimeRange{
				{
					Start: t0,
					End:   t0.Add(15 * m),
				},
			},
		},
		{
			name: "chain of overlapping ranges",
			input: []HintTimeRange{
				{
					Start: t0,
					End:   t0.Add(5 * m),
				},
				{
					Start: t0.Add(3 * m),
					End:   t0.Add(8 * m),
				},
				{
					Start: t0.Add(7 * m),
					End:   t0.Add(12 * m),
				},
				{
					Start: t0.Add(20 * m),
					End:   t0.Add(25 * m),
				},
			},
			expected: []HintTimeRange{
				{
					Start: t0,
					End:   t0.Add(12 * m),
				},
				{
					Start: t0.Add(20 * m),
					End:   t0.Add(25 * m),
				},
			},
		},
		{
			name: "overlapping merges source provenance",
			input: []HintTimeRange{
				{
					Start:  t0,
					End:    t0.Add(10 * m),
					Source: "index=a,doc=1",
				},
				{
					Start:  t0.Add(5 * m),
					End:    t0.Add(15 * m),
					Source: "index=b,doc=3",
				},
				{
					Start:  t0.Add(7 * m),
					End:    t0.Add(12 * m),
					Source: "index=a,doc=1",
				},
			},
			expected: []HintTimeRange{
				{
					Start:  t0,
					End:    t0.Add(15 * m),
					Source: "index=a,doc=1;index=b,doc=3;index=a,doc=1",
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeRanges(tc.input)
			require.Equal(t, tc.expected, got)
		})
	}
}

func mustParseExpr(t *testing.T, query string) syntax.Expr {
	t.Helper()
	expr, err := syntax.ParseExpr(query)
	require.NoError(t, err)
	return expr
}

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	bucket := objstore.NewInMemBucket()
	indexStore, err := store.NewStore(bucket, store.Config{MinDate: "0001-01-01"}, log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)
	return indexStore
}

func newTestStoreWithMinDate(t *testing.T, minDate string) *store.Store {
	t.Helper()
	bucket := objstore.NewInMemBucket()
	indexStore, err := store.NewStore(bucket, store.Config{MinDate: minDate}, log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)
	return indexStore
}

func writeTestIndex(t *testing.T, indexStore *store.Store, hash, needle string, docMin, docMax time.Time) {
	t.Helper()
	indexBytes, headerInfo := buildIndexBytes(t, needle, docMin, docMax)

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

func writeShardedTestIndex(t *testing.T, indexStore *store.Store, hash, needle string, docMin, docMax time.Time, shardCount int, shardAlgorithm string, shardValue int) {
	t.Helper()
	indexBytes, headerInfo := buildIndexBytes(t, needle, docMin, docMax)

	meta := store.Meta{
		Date:           docMin.UTC().Format("2006-01-02"),
		Hash:           hash,
		Version:        logline.CurrentVersion,
		MinLogTs:       docMin.UTC(),
		MaxLogTs:       docMax.UTC(),
		MinRecordTs:    docMin.UTC(),
		MaxRecordTs:    docMax.UTC(),
		IndexHeader:    headerInfo,
		SizeBytes:      int64(len(indexBytes)),
		ShardCount:     shardCount,
		ShardAlgorithm: shardAlgorithm,
		ShardValue:     shardValue,
	}

	require.NoError(t, indexStore.PutIndex(context.Background(), bytes.NewReader(indexBytes), meta))
	require.NoError(t, indexStore.Poll(context.Background()))
}

func writeMatchesAllTestIndex(t *testing.T, indexStore *store.Store, hash, needle string, logTS time.Time) {
	t.Helper()
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "matches-all.lidx")

	docs := []format.DocumentMetadata{{
		ID:          0,
		MinTimeUnix: logTS.UnixMilli(),
		MaxTimeUnix: logTS.Add(100 * time.Millisecond).UnixMilli(),
	}}
	writer, err := logline.NewWriter(logline.CurrentVersion, path, docs, nil)
	require.NoError(t, err)

	terms, err := ExtractQueryNgrams(needle, 6, logline.CurrentVersion)
	require.NoError(t, err)
	require.NotEmpty(t, terms)
	for _, term := range terms {
		var key [8]byte
		copy(key[:], term)
		require.NoError(t, writer.WriteTermBitmap(key, format.Bitmap{MatchesAll: true}))
	}
	require.NoError(t, writer.Close())

	reader, _, err := logline.OpenFile(path)
	require.NoError(t, err)
	headerInfo := reader.ReadHeader()
	require.NoError(t, reader.Close())

	indexBytes, err := os.ReadFile(path)
	require.NoError(t, err)
	meta := store.Meta{
		Date:        logTS.UTC().Format("2006-01-02"),
		Hash:        hash,
		Version:     logline.CurrentVersion,
		MinLogTs:    logTS,
		MaxLogTs:    logTS,
		MinRecordTs: logTS,
		MaxRecordTs: logTS,
		IndexHeader: &headerInfo,
		SizeBytes:   int64(len(indexBytes)),
	}
	require.NoError(t, indexStore.PutIndex(context.Background(), bytes.NewReader(indexBytes), meta))
	require.NoError(t, indexStore.Poll(context.Background()))
}

func TestLoglineHintProvider_ProvideHints_CrossShardIntersection(t *testing.T) {
	// Build two sharded indexes covering different time ranges.
	// Shard 0 covers [t0, t0+20m], shard 1 covers [t0+10m, t0+30m].
	// The intersection should be [t0+10m, t0+20m].
	needle := "9fA81cD2Ef0077aa"
	t0 := time.Date(2026, 2, 26, 10, 0, 0, 0, time.UTC)

	indexStore := newTestStore(t)
	writeShardedTestIndex(t, indexStore, "1111111111111111", needle,
		t0, t0.Add(20*time.Minute), 4, "first_byte", 0)
	writeShardedTestIndex(t, indexStore, "2222222222222222", needle,
		t0.Add(10*time.Minute), t0.Add(30*time.Minute), 4, "first_byte", 1)

	provider, err := NewLoglineHintProvider(indexStore, 6, 0, nil, log.NewNopLogger(), nil)
	require.NoError(t, err)

	expr := mustParseExpr(t, `{job="api"} |= "9fA81cD2Ef0077aa"`)
	hints, _, err := provider.ProvideHints(
		context.Background(),
		"test-tenant",
		expr,
		model.TimeFromUnixNano(t0.Add(-time.Minute).UnixNano()),
		model.TimeFromUnixNano(t0.Add(31*time.Minute).UnixNano()),
	)
	require.NoError(t, err)
	require.NotNil(t, hints)
	require.Len(t, hints.TimeRanges, 1)
	require.Equal(t, t0.Add(10*time.Minute), hints.TimeRanges[0].Start)
	require.Equal(t, t0.Add(20*time.Minute), hints.TimeRanges[0].End)
}

func TestLoglineHintProvider_ProvideHints_EmptyShardAnnihilatesIntersection(t *testing.T) {
	const needle = "1NG8K49T"
	t0 := time.Date(2026, 8, 13, 10, 0, 0, 0, time.UTC)

	ngrams, err := ExtractQueryNgrams(needle, 6, logline.CurrentVersion)
	require.NoError(t, err)
	require.Len(t, ngrams, 3)

	byShard := make(map[int][]string)
	for shardValue := range 10 {
		meta := store.Meta{
			ShardCount:     10,
			ShardAlgorithm: shard.AlgorithmMurmur3Mix,
			ShardValue:     shardValue,
		}
		if terms := filterNgramsForShard(ngrams, meta); len(terms) > 0 {
			byShard[shardValue] = terms
		}
	}
	require.Len(t, byShard, 2)

	var emptyShard, matchingShard int
	for shardValue, terms := range byShard {
		switch len(terms) {
		case 2:
			emptyShard = shardValue
		case 1:
			matchingShard = shardValue
		default:
			require.FailNow(t, "unexpected n-gram distribution", "terms=%v", byShard)
		}
	}

	docs := []format.DocumentMetadata{
		{ID: 0, MinTimeUnix: t0.UnixMilli(), MaxTimeUnix: t0.Add(time.Second).UnixMilli()},
		{ID: 1, MinTimeUnix: t0.Add(2 * time.Second).UnixMilli(), MaxTimeUnix: t0.Add(3 * time.Second).UnixMilli()},
	}
	indexStore := newTestStore(t)
	writeShardedTermTestIndex(t, indexStore, "1111111111111111", docs, map[string][]uint32{
		byShard[emptyShard][0]: {0},
		byShard[emptyShard][1]: {1},
	}, emptyShard)
	writeShardedTermTestIndex(t, indexStore, "2222222222222222", docs, map[string][]uint32{
		byShard[matchingShard][0]: {0},
	}, matchingShard)

	provider, err := NewLoglineHintProvider(indexStore, 6, 0, nil, log.NewNopLogger(), nil)
	require.NoError(t, err)

	expr := mustParseExpr(t, `{job="api"} |= "1NG8K49T"`)
	hints, _, err := provider.ProvideHints(
		context.Background(),
		"test-tenant",
		expr,
		model.TimeFromUnixNano(t0.Add(-time.Minute).UnixNano()),
		model.TimeFromUnixNano(t0.Add(time.Minute).UnixNano()),
	)
	require.NoError(t, err)
	require.NotNil(t, hints)
	require.Empty(t, hints.TimeRanges)
}

func TestLoglineHintProvider_ProvideHints_ShardedPlusUnsharded(t *testing.T) {
	needle := "9fA81cD2Ef0077aa"
	t0 := time.Date(2026, 2, 26, 10, 0, 0, 0, time.UTC)

	indexStore := newTestStore(t)
	// Two shards that intersect to [t0+10m, t0+20m].
	writeShardedTestIndex(t, indexStore, "1111111111111111", needle,
		t0, t0.Add(20*time.Minute), 4, "first_byte", 0)
	writeShardedTestIndex(t, indexStore, "2222222222222222", needle,
		t0.Add(10*time.Minute), t0.Add(30*time.Minute), 4, "first_byte", 1)
	// Unsharded index at a disjoint time [t0+50m, t0+60m].
	writeTestIndex(t, indexStore, "3333333333333333", needle,
		t0.Add(50*time.Minute), t0.Add(60*time.Minute))

	provider, err := NewLoglineHintProvider(indexStore, 6, 0, nil, log.NewNopLogger(), nil)
	require.NoError(t, err)

	expr := mustParseExpr(t, `{job="api"} |= "9fA81cD2Ef0077aa"`)
	hints, _, err := provider.ProvideHints(
		context.Background(),
		"test-tenant",
		expr,
		model.TimeFromUnixNano(t0.Add(-time.Minute).UnixNano()),
		model.TimeFromUnixNano(t0.Add(61*time.Minute).UnixNano()),
	)
	require.NoError(t, err)
	require.NotNil(t, hints)
	require.Len(t, hints.TimeRanges, 2)
	// Sharded intersection
	require.Equal(t, t0.Add(10*time.Minute), hints.TimeRanges[0].Start)
	require.Equal(t, t0.Add(20*time.Minute), hints.TimeRanges[0].End)
	// Unsharded union
	require.Equal(t, t0.Add(50*time.Minute), hints.TimeRanges[1].Start)
	require.Equal(t, t0.Add(60*time.Minute), hints.TimeRanges[1].End)
}

func TestLoglineHintProvider_ProvideHints_CrossIndexBatchingFillsSharedBatches(t *testing.T) {
	indexStore := newTestStore(t)
	needle := "ABCDEFGH" // 3 unique 6-grams
	t0 := time.Date(2026, 2, 26, 10, 0, 0, 0, time.UTC)

	writeTestIndex(t, indexStore, "aaaaaaaaaaaaaaaa", needle, t0, t0.Add(5*time.Minute))
	writeTestIndex(t, indexStore, "bbbbbbbbbbbbbbbb", needle, t0.Add(10*time.Minute), t0.Add(15*time.Minute))

	var observedReasons []string
	var observedBatches []int
	provider, err := NewLoglineHintProvider(indexStore, 6, 0, func(reason string, termBatchesProcessed int) {
		observedReasons = append(observedReasons, reason)
		observedBatches = append(observedBatches, termBatchesProcessed)
	}, log.NewNopLogger(), nil)
	require.NoError(t, err)

	expr := mustParseExpr(t, `{job="api"} |= "ABCDEFGH"`)
	hints, stats, err := provider.ProvideHints(
		context.Background(),
		"test-tenant",
		expr,
		model.TimeFromUnixNano(t0.Add(-time.Minute).UnixNano()),
		model.TimeFromUnixNano(t0.Add(16*time.Minute).UnixNano()),
	)
	require.NoError(t, err)
	require.NotNil(t, hints)
	require.Len(t, hints.TimeRanges, 2)

	// Two indexes with three terms each and fixed batch size 64 means both
	// indexes are completed in a single term batch each.
	require.Len(t, observedReasons, 2)
	require.ElementsMatch(t, []string{"positive", "positive"}, observedReasons)
	require.ElementsMatch(t, []int{1, 1}, observedBatches)

	snap := stats.Snapshot()
	require.Equal(t, int64(2), snap.IndexQueriesTotal)
	require.Equal(t, int64(2), snap.TotalTermBatchesProcessed)
}

func TestLoglineHintProvider_ExecuteQuery_OpensReaderOncePerIndex(t *testing.T) {
	indexStore := newTestStore(t)
	needle := "9fA81cD2Ef0077aa"
	t0 := time.Date(2026, 2, 26, 10, 0, 0, 0, time.UTC)

	writeTestIndex(t, indexStore, "aaaaaaaaaaaaaaaa", needle, t0, t0.Add(5*time.Minute))

	provider, err := NewLoglineHintProvider(indexStore, 6, 0, nil, log.NewNopLogger(), nil)
	require.NoError(t, err)

	active := indexStore.Snapshot().Active()
	require.Len(t, active, 1)

	stats := NewQueryStats()
	shardRanges, err := provider.executeQuery(context.Background(), []string{needle, needle}, active, stats)
	require.NoError(t, err)
	require.NotEmpty(t, shardRanges)

	snap := stats.Snapshot()
	require.Equal(t, int64(1), snap.MetadataCacheMisses)
}

func TestLoglineHintProvider_MetadataCache_SkipsPutWhenFull(t *testing.T) {
	cache := newMetadataCache(2, nil)

	type opaqueState struct{}
	a1 := &opaqueState{}
	cache.put("a", cachedMetadata{state: a1})
	cache.put("b", cachedMetadata{state: &opaqueState{}})
	cache.put("c", cachedMetadata{state: &opaqueState{}})

	require.Equal(t, 2, cache.len(), "cache should remain capped at max entries")
	_, ok := cache.get("a")
	require.True(t, ok, "existing entries should be retained when cache is full")
	_, ok = cache.get("b")
	require.True(t, ok)
	_, ok = cache.get("c")
	require.False(t, ok, "new entry should not be cached when full")

	// Existing keys should still be updated even when the cache is at capacity.
	a2 := &opaqueState{}
	cache.put("a", cachedMetadata{state: a2})
	require.Equal(t, 2, cache.len(), "updating existing key should not change cache size")
	got, ok := cache.get("a")
	require.True(t, ok)
	require.Same(t, a2, got.state.(*opaqueState), "existing entry should be updated when full")
}

func TestLoglineHintProvider_EvictStaleMetadata(t *testing.T) {
	indexStore := newTestStore(t)
	needle := "9fA81cD2Ef0077aa"
	base := time.Date(2026, 2, 26, 10, 0, 0, 0, time.UTC)
	writeTestIndex(t, indexStore, "aaaaaaaaaaaaaaaa", needle, base, base.Add(10*time.Second))
	writeTestIndex(t, indexStore, "bbbbbbbbbbbbbbbb", needle, base.Add(20*time.Second), base.Add(30*time.Second))

	provider, err := NewLoglineHintProvider(indexStore, 6, 0, nil, log.NewNopLogger(), nil)
	require.NoError(t, err)

	active := indexStore.Snapshot().Active()
	require.Len(t, active, 2)

	for _, meta := range active {
		reader, err := provider.openIndexReader(context.Background(), meta, nil)
		require.NoError(t, err)
		require.NoError(t, reader.Close())
	}

	require.Equal(t, 2, provider.cache.len())

	ids := map[string]struct{}{
		active[0].ID(): {},
		active[1].ID(): {},
	}
	deletedID := active[0].ID()

	require.NoError(t, indexStore.DeleteIndex(context.Background(), active[0]))
	require.NoError(t, indexStore.Poll(context.Background()))

	provider.cache.evictStale(indexStore.Snapshot())

	require.Equal(t, 1, provider.cache.len())
	_, ok := provider.cache.get(deletedID)
	require.False(t, ok)

	delete(ids, deletedID)
	var remainingID string
	for id := range ids {
		remainingID = id
	}
	_, ok = provider.cache.get(remainingID)
	require.True(t, ok)
}

// minimalMeta returns a store.Meta suitable for buildTermJobs tests that don't need real index data.
// Only Version and ID-related fields are set; ShardCount=0 so filterNgramsForShard
// passes all ngrams through unchanged.
func minimalMeta(hash, date, indexVersion string) store.Meta {
	return store.Meta{
		Date:    date,
		Hash:    hash,
		Version: indexVersion,
	}
}

// TestBuildTermJobs_SingleVersionCache verifies that two blocks sharing the same
// index version both produce term jobs, confirming the per-version cache
// doesn't accidentally drop the second block.
func TestBuildTermJobs_SingleVersionCache(t *testing.T) {
	filter := "abcdefg" // produces 2 six-grams: ABCDEF, BCDEFG
	metas := []store.Meta{
		minimalMeta("aaaaaaaaaaaaaaa1", "2026-01-01", "v3"),
		minimalMeta("aaaaaaaaaaaaaaa2", "2026-01-01", "v3"),
	}

	jobs, metasByID, err := buildTermJobs([]string{filter}, metas, 6)
	require.NoError(t, err)
	require.NotEmpty(t, jobs)

	// Both blocks should appear in metasByID (each produced at least one job).
	require.Contains(t, metasByID, metas[0].ID())
	require.Contains(t, metasByID, metas[1].ID())
}

// TestBuildTermJobs_MixedVersions verifies that blocks with different index
// versions both produce term jobs against the per-version cache. The cache is
// keyed by version, so each version must hit the extractor at least once.
//
// Skipped while only one index version is supported; remove the Skip (or let
// the AllVersions guard drop out) when adding the next version after v3.
func TestBuildTermJobs_MixedVersions(t *testing.T) {
	versions := logline.AllVersions()
	if len(versions) < 2 {
		t.Skip("needs at least two supported index versions; restore when adding the next version after v3")
	}

	filter := "abcdefg" // produces 2 six-grams: ABCDEF, BCDEFG
	metas := []store.Meta{
		minimalMeta("aaaaaaaaaaaaaaa1", "2026-01-01", versions[0]),
		minimalMeta("aaaaaaaaaaaaaaa2", "2026-01-01", versions[1]),
	}

	jobs, metasByID, err := buildTermJobs([]string{filter}, metas, 6)
	require.NoError(t, err)
	require.NotEmpty(t, jobs)

	// Both blocks should appear in metasByID even though they carry different
	// index versions.
	require.Contains(t, metasByID, metas[0].ID())
	require.Contains(t, metasByID, metas[1].ID())
}

// TestBuildTermJobs_UnknownVersionReturnsError verifies that a block with an
// unrecognised index version causes the query to fail with an error.
func TestBuildTermJobs_UnknownVersionReturnsError(t *testing.T) {
	filter := "abcdefg"
	metas := []store.Meta{
		minimalMeta("aaaaaaaaaaaaaaa1", "2026-01-01", "v3"),
		minimalMeta("aaaaaaaaaaaaaaa2", "2026-01-01", "v99"), // unknown
	}

	_, _, err := buildTermJobs([]string{filter}, metas, 6)
	require.Error(t, err)
}

// TestBuildTermJobs_FilterTooShortReturnsUnsupported verifies that ErrUnsupported
// is returned when the filter string is too short to produce any ngrams for a
// known index version.
func TestBuildTermJobs_FilterTooShortReturnsUnsupported(t *testing.T) {
	filter := "ab" // too short for n=6
	metas := []store.Meta{
		minimalMeta("aaaaaaaaaaaaaaa1", "2026-01-01", "v3"),
	}

	_, _, err := buildTermJobs([]string{filter}, metas, 6)
	require.ErrorIs(t, err, ErrUnsupported)
}

func buildIndexBytes(t *testing.T, needle string, docMin, docMax time.Time) ([]byte, *format.HeaderInfo) {
	t.Helper()
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.lidx")

	docs := []format.DocumentMetadata{
		{ID: 0, MinTimeUnix: docMin.UnixMilli(), MaxTimeUnix: docMax.UnixMilli()},
	}
	writer, err := logline.NewWriter(logline.CurrentVersion, path, docs, nil)
	require.NoError(t, err)

	terms, err := ExtractQueryNgrams(needle, 6, logline.CurrentVersion)
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
	reader.Close()

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return data, &info
}

func writeShardedTermTestIndex(
	t *testing.T,
	indexStore *store.Store,
	hash string,
	docs []format.DocumentMetadata,
	postings map[string][]uint32,
	shardValue int,
) {
	t.Helper()
	require.NotEmpty(t, docs)

	path := filepath.Join(t.TempDir(), "test.lidx")
	writer, err := logline.NewWriter(logline.CurrentVersion, path, docs, nil)
	require.NoError(t, err)

	terms := make([]string, 0, len(postings))
	for term := range postings {
		terms = append(terms, term)
	}
	sort.Strings(terms)
	for _, term := range terms {
		bitmap := roaring.New()
		bitmap.AddMany(postings[term])
		var key [8]byte
		copy(key[:], term)
		require.NoError(t, writer.WriteTermBitmap(key, format.Bitmap{Roaring: bitmap}))
	}
	require.NoError(t, writer.Close())

	reader, _, err := logline.OpenFile(path)
	require.NoError(t, err)
	headerInfo := reader.ReadHeader()
	require.NoError(t, reader.Close())

	indexBytes, err := os.ReadFile(path)
	require.NoError(t, err)
	minLogTS := time.UnixMilli(docs[0].MinTimeUnix).UTC()
	maxLogTS := time.UnixMilli(docs[len(docs)-1].MaxTimeUnix).UTC()
	meta := store.Meta{
		Date:           minLogTS.Format("2006-01-02"),
		Hash:           hash,
		Version:        logline.CurrentVersion,
		MinLogTs:       minLogTS,
		MaxLogTs:       maxLogTS,
		MinRecordTs:    minLogTS,
		MaxRecordTs:    maxLogTS,
		IndexHeader:    &headerInfo,
		SizeBytes:      int64(len(indexBytes)),
		ShardCount:     10,
		ShardAlgorithm: shard.AlgorithmMurmur3Mix,
		ShardValue:     shardValue,
	}
	require.NoError(t, indexStore.PutIndex(context.Background(), bytes.NewReader(indexBytes), meta))
	require.NoError(t, indexStore.Poll(context.Background()))
}
