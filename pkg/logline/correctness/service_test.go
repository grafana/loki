package correctness

import (
	"bytes"
	"context"
	"encoding/json"
	"math/rand" //#nosec G404 -- Test sampling is not security-sensitive. -- nosemgrep: math-random-used
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/RoaringBitmap/roaring"
	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"

	"github.com/grafana/loki/v3/pkg/logline"
	"github.com/grafana/loki/v3/pkg/logline/format"
	"github.com/grafana/loki/v3/pkg/logline/hintprovider"
	"github.com/grafana/loki/v3/pkg/logline/store"
)

func TestRunVerificationCycle_Correct(t *testing.T) {
	base := time.Date(2026, 2, 26, 10, 0, 0, 0, time.UTC)
	witnessTS := base.Add(1 * time.Minute)
	needle := "9fA81cD2Ef0077aa"

	loki := newTestLokiServer(t, witnessTS, needle)
	defer loki.Close()

	indexStore := newTestStore(t)
	writeTestIndex(t, indexStore, "aaaaaaaaaaaaaaaa", needle, base.Add(50*time.Second), base.Add(70*time.Second))

	svc := newTestService(t, indexStore, loki.URL, base)
	report, err := svc.runVerificationCycle(context.Background())
	require.NoError(t, err)
	require.Empty(t, report.SkippedReason)
	require.True(t, report.Correct)
	require.Equal(t, 0, report.FalseNegatives)
	require.Equal(t, 0, report.FalsePositives)
	require.GreaterOrEqual(t, report.TruePositives, 1)
}

func TestRunVerificationCycle_FalseNegative(t *testing.T) {
	base := time.Date(2026, 2, 26, 10, 0, 0, 0, time.UTC)
	witnessTS := base.Add(1 * time.Minute)
	needle := "9fA81cD2Ef0077aa"

	loki := newTestLokiServer(t, witnessTS, needle)
	defer loki.Close()

	indexStore := newTestStore(t)
	// Candidate doc does not overlap the witness timestamp → false negative.
	// The candidate also won't match in Loki → false positive (expected for a
	// non-deterministic index, does not affect correctness).
	writeTestIndex(t, indexStore, "bbbbbbbbbbbbbbbb", needle, base.Add(110*time.Second), base.Add(115*time.Second))

	svc := newTestService(t, indexStore, loki.URL, base)
	report, err := svc.runVerificationCycle(context.Background())
	require.NoError(t, err)
	require.Empty(t, report.SkippedReason)
	require.False(t, report.Correct, "false negative should make the test incorrect")
	require.Greater(t, report.FalseNegatives, 0)
	require.Equal(t, 1, report.FalsePositives)
}

func TestRunVerificationCycle_ExcludedIndexCoveringFN(t *testing.T) {
	base := time.Date(2026, 2, 26, 10, 0, 0, 0, time.UTC)
	witnessTS := base.Add(1 * time.Minute)
	needle := "9fA81cD2Ef0077aa"
	hash := "cccccccccccccccc"

	loki := newTestLokiServer(t, witnessTS, needle)
	defer loki.Close()

	// Match correctness QIW so IndexesExcludedByIngesterWindow can fire.
	bucket := objstore.NewInMemBucket()
	indexStore, err := store.NewStore(bucket, store.Config{
		MinDate:              "0001-01-01",
		QueryIngestersWithin: 1 * time.Minute,
	}, log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)

	// Log span covers the witness (service clock). Kafka/rec time must be fresh
	// against wall clock — IndexesForRange uses time.Now(), not svc.nowFn.
	wallNow := time.Now().UTC()
	writeTestIndexWithRecordTs(t, indexStore, hash, needle,
		base.Add(50*time.Second), base.Add(70*time.Second),
		wallNow.Add(-30*time.Second), wallNow.Add(-15*time.Second),
	)

	svc := newTestService(t, indexStore, loki.URL, base)
	report, err := svc.runVerificationCycle(context.Background())
	require.NoError(t, err)
	require.Empty(t, report.SkippedReason)
	require.False(t, report.Correct)
	require.Greater(t, report.FalseNegatives, 0)

	covering := svc.excludedIndexesCoveringFNs(
		report.FalseNegativeTimestamps,
		base,
		base.Add(2*time.Minute),
	)
	require.Len(t, covering, 1)
	require.Equal(t, "2026-02-26/"+hash, covering[0].ID())
}

func TestRunVerificationCycle_UnsupportedQuery(t *testing.T) {
	base := time.Date(2026, 2, 26, 10, 0, 0, 0, time.UTC)
	witnessTS := base.Add(1 * time.Minute)
	needle := "9fA81cD2Ef0077aa"

	loki := newTestLokiServer(t, witnessTS, needle)
	defer loki.Close()

	indexStore := newTestStore(t)
	svc := newTestService(t, indexStore, loki.URL, base)
	svc.hintProvider = unsupportedHintProvider{}

	report, err := svc.runVerificationCycle(context.Background())
	require.NoError(t, err)
	require.Equal(t, skipQueryUnsupported, report.SkippedReason)
}

func TestPickNeedle(t *testing.T) {
	entries := []logproto.Entry{
		{Line: "stable token hellohello"},
		{Line: "trace=9fA81cD2Ef0077aa done"},
	}

	needle, _, ok := pickNeedle(entries, 6)
	require.True(t, ok)
	require.Equal(t, "9fA81cD2Ef0077aa", needle)
}

func TestBuildLabelFilterHintQuery(t *testing.T) {
	query := buildLabelFilterHintQuery(`{job="api"}`, "trace_id", "abc123def456")
	require.Equal(t, `{job="api"} | trace_id="abc123def456"`, query)
}

func TestBuildJSONLabelFilterHintQuery(t *testing.T) {
	query := buildJSONLabelFilterHintQuery(`{job="api"}`, "dashboardUID", "grafana_slo_app-klu4xpj1w5lmbmvi8u6ec")
	require.Equal(t, `{job="api"} | json | dashboardUID="grafana_slo_app-klu4xpj1w5lmbmvi8u6ec"`, query)
}

func TestPickJSONField(t *testing.T) {
	t.Run("picks top-level string long enough for ngrams", func(t *testing.T) {
		field, value, ok := pickJSONField([]queryEntry{{
			Entry: logproto.Entry{Line: `{"dashboardUID":"grafana_slo_app-klu4xpj1w5lmbmvi8u6ec","level":"info"}`},
		}}, 6)
		require.True(t, ok)
		require.Equal(t, "dashboardUID", field)
		require.Equal(t, "grafana_slo_app-klu4xpj1w5lmbmvi8u6ec", value)
	})
	t.Run("skips non-json lines", func(t *testing.T) {
		_, _, ok := pickJSONField([]queryEntry{{Entry: logproto.Entry{Line: "request completed"}}}, 6)
		require.False(t, ok)
	})
	t.Run("skips escaped values that are not a line substring", func(t *testing.T) {
		_, _, ok := pickJSONField([]queryEntry{{
			Entry: logproto.Entry{Line: `{"msg":"hello\nworldxx"}`},
		}}, 6)
		require.False(t, ok)
	})
	t.Run("skips ineligible siblings and picks the valid field", func(t *testing.T) {
		field, value, ok := pickJSONField([]queryEntry{{
			Entry: logproto.Entry{Line: `{"level":"info","status":200,"dashboard-uid":"skipped-hyphen-keyxx","dashboardUID":"grafana_slo_app-klu4xpj1w5lmbmvi8u6ec"}`},
		}}, 6)
		require.True(t, ok)
		require.Equal(t, "dashboardUID", field)
		require.Equal(t, "grafana_slo_app-klu4xpj1w5lmbmvi8u6ec", value)
	})
	t.Run("skips json key that already exists as a stream label", func(t *testing.T) {
		field, value, ok := pickJSONField([]queryEntry{{
			Entry:        logproto.Entry{Line: `{"name":"celz-us-portland","dashboardUID":"grafana_slo_app-klu4xpj1w5lmbmvi8u6ec"}`},
			StreamLabels: loghttp.LabelSet{"name": "k6-operator"},
		}}, 6)
		require.True(t, ok)
		require.Equal(t, "dashboardUID", field)
		require.Equal(t, "grafana_slo_app-klu4xpj1w5lmbmvi8u6ec", value)
	})
	t.Run("skips json key that already exists as structured metadata", func(t *testing.T) {
		field, value, ok := pickJSONField([]queryEntry{{
			Entry: logproto.Entry{
				Line: `{"name":"celz-us-portland","dashboardUID":"grafana_slo_app-klu4xpj1w5lmbmvi8u6ec"}`,
				StructuredMetadata: []logproto.LabelAdapter{
					{Name: "name", Value: "from-sm"},
				},
			},
		}}, 6)
		require.True(t, ok)
		require.Equal(t, "dashboardUID", field)
		require.Equal(t, "grafana_slo_app-klu4xpj1w5lmbmvi8u6ec", value)
	})
	t.Run("no field when every eligible key collides", func(t *testing.T) {
		_, _, ok := pickJSONField([]queryEntry{{
			Entry:        logproto.Entry{Line: `{"name":"celz-us-portland"}`},
			StreamLabels: loghttp.LabelSet{"name": "k6-operator"},
		}}, 6)
		require.False(t, ok)
	})
	t.Run("uses first value when a key is duplicated", func(t *testing.T) {
		field, value, ok := pickJSONField([]queryEntry{{
			Entry: logproto.Entry{Line: `{"module":"agent_virtual_environment","module":"environment"}`},
		}}, 6)
		require.True(t, ok)
		require.Equal(t, "module", field)
		require.Equal(t, "agent_virtual_environment", value)
	})
}

func TestPickHintQuery_ShortValueUsesLineFilter(t *testing.T) {
	indexStore := newTestStore(t)
	loki := newTestLokiServer(t, time.Now(), "9fA81cD2Ef0077aa")
	defer loki.Close()
	svc := newTestService(t, indexStore, loki.URL, time.Date(2026, 2, 26, 10, 0, 0, 0, time.UTC))

	logs := []queryEntry{{Entry: logproto.Entry{Line: "request trace=9fA81cD2Ef0077aa completed"}}}
	for i := 0; i < 10; i++ {
		h, ok := svc.pickHintQuery(`{job="api"}`, "job", "api", logs)
		require.True(t, ok)
		require.Equal(t, hintQueryTypeLineFilter, h.queryType)
		require.Equal(t, "9fA81cD2Ef0077aa", h.needle)
		require.Equal(t, `{job="api"} |= "9fA81cD2Ef0077aa"`, h.query)
		require.NotNil(t, h.expr)
	}
}

func TestPickHintQuery_LongValueCanUseLabelFilter(t *testing.T) {
	indexStore := newTestStore(t)
	loki := newTestLokiServer(t, time.Now(), "9fA81cD2Ef0077aa")
	defer loki.Close()
	svc := newTestService(t, indexStore, loki.URL, time.Date(2026, 2, 26, 10, 0, 0, 0, time.UTC))

	// No SM: SM path falls back to line; stream-label and line paths remain.
	logs := []queryEntry{{Entry: logproto.Entry{Line: "request trace=9fA81cD2Ef0077aa completed"}}}
	value := "checkout-service"
	sawLabel, sawLine := false, false
	for seed := int64(0); seed < 50 && (!sawLabel || !sawLine); seed++ {
		svc.randMu.Lock()
		svc.rand = rand.New(rand.NewSource(seed))
		svc.randMu.Unlock()

		h, ok := svc.pickHintQuery(`{job="checkout-service"}`, "job", value, logs)
		require.True(t, ok)
		require.NotNil(t, h.expr)
		switch h.queryType {
		case hintQueryTypeLabelFilter:
			sawLabel = true
			require.Equal(t, value, h.needle)
			require.Equal(t, `{job="checkout-service"} | job="checkout-service"`, h.query)
		case hintQueryTypeLineFilter:
			sawLine = true
			require.Equal(t, "9fA81cD2Ef0077aa", h.needle)
			require.Equal(t, `{job="checkout-service"} |= "9fA81cD2Ef0077aa"`, h.query)
		default:
			t.Fatalf("unexpected query type %q", h.queryType)
		}
	}
	require.True(t, sawLabel, "expected some seeds to pick label_filter")
	require.True(t, sawLine, "expected some seeds to pick line_filter")
}

func TestPickHintQuery_FourWayWhenAllEligible(t *testing.T) {
	indexStore := newTestStore(t)
	loki := newTestLokiServer(t, time.Now(), "9fA81cD2Ef0077aa")
	defer loki.Close()
	svc := newTestService(t, indexStore, loki.URL, time.Date(2026, 2, 26, 10, 0, 0, 0, time.UTC))

	jsonValue := "grafana_slo_app-klu4xpj1w5lmbmvi8u6ec"
	smValue := "traceidvalue99"
	streamValue := "checkout-service"
	logs := []queryEntry{{
		Entry: logproto.Entry{
			Line: `{"dashboardUID":"grafana_slo_app-klu4xpj1w5lmbmvi8u6ec","trace-id":"9fA81cD2Ef0077aa"}`,
			StructuredMetadata: []logproto.LabelAdapter{
				{Name: "trace_id", Value: smValue},
			},
		},
	}}
	lineNeedle, _, ok := pickNeedle(protoEntries(logs), 6)
	require.True(t, ok)

	sawSM, sawLabel, sawJSON, sawLine := false, false, false, false
	for seed := int64(0); seed < 100 && (!sawSM || !sawLabel || !sawJSON || !sawLine); seed++ {
		svc.randMu.Lock()
		svc.rand = rand.New(rand.NewSource(seed))
		svc.randMu.Unlock()

		h, ok := svc.pickHintQuery(`{job="checkout-service"}`, "job", streamValue, logs)
		require.True(t, ok)
		require.NotNil(t, h.expr)
		switch h.queryType {
		case hintQueryTypeSMLabelFilter:
			sawSM = true
			require.Equal(t, smValue, h.needle)
		case hintQueryTypeLabelFilter:
			sawLabel = true
			require.Equal(t, streamValue, h.needle)
		case hintQueryTypeJSONLabelFilter:
			sawJSON = true
			require.Equal(t, jsonValue, h.needle)
			require.Equal(t, `{job="checkout-service"} | json | dashboardUID="grafana_slo_app-klu4xpj1w5lmbmvi8u6ec"`, h.query)
		case hintQueryTypeLineFilter:
			sawLine = true
			require.Equal(t, lineNeedle, h.needle)
		default:
			t.Fatalf("unexpected query type %q", h.queryType)
		}
	}
	require.True(t, sawSM, "expected some seeds to pick sm_label_filter")
	require.True(t, sawLabel, "expected some seeds to pick label_filter")
	require.True(t, sawJSON, "expected some seeds to pick json_label_filter")
	require.True(t, sawLine, "expected some seeds to pick line_filter")
}

func TestPickHintQuery_CanUseJSONLabelFilter(t *testing.T) {
	indexStore := newTestStore(t)
	loki := newTestLokiServer(t, time.Now(), "9fA81cD2Ef0077aa")
	defer loki.Close()
	svc := newTestService(t, indexStore, loki.URL, time.Date(2026, 2, 26, 10, 0, 0, 0, time.UTC))

	jsonValue := "grafana_slo_app-klu4xpj1w5lmbmvi8u6ec"
	logs := []queryEntry{{
		Entry: logproto.Entry{Line: `{"dashboardUID":"grafana_slo_app-klu4xpj1w5lmbmvi8u6ec","trace-id":"9fA81cD2Ef0077aa"}`},
	}}

	sawJSON, sawLine := false, false
	for seed := int64(0); seed < 80 && (!sawJSON || !sawLine); seed++ {
		svc.randMu.Lock()
		svc.rand = rand.New(rand.NewSource(seed))
		svc.randMu.Unlock()

		h, ok := svc.pickHintQuery(`{job="api"}`, "job", "api", logs)
		require.True(t, ok)
		require.NotNil(t, h.expr)
		switch h.queryType {
		case hintQueryTypeJSONLabelFilter:
			sawJSON = true
			require.Equal(t, jsonValue, h.needle)
			require.Equal(t, `{job="api"} | json | dashboardUID="grafana_slo_app-klu4xpj1w5lmbmvi8u6ec"`, h.query)
		case hintQueryTypeLineFilter:
			sawLine = true
		default:
			t.Fatalf("unexpected query type %q", h.queryType)
		}
	}
	require.True(t, sawJSON, "expected some seeds to pick json_label_filter")
	require.True(t, sawLine, "expected some seeds to pick line_filter")
}

func TestPickHintQuery_CanUseStructuredMetadata(t *testing.T) {
	indexStore := newTestStore(t)
	loki := newTestLokiServer(t, time.Now(), "9fA81cD2Ef0077aa")
	defer loki.Close()
	svc := newTestService(t, indexStore, loki.URL, time.Date(2026, 2, 26, 10, 0, 0, 0, time.UTC))

	smValue := "traceidvalue99"
	logs := []queryEntry{{
		Entry: logproto.Entry{
			Line: "request completed",
			StructuredMetadata: []logproto.LabelAdapter{
				{Name: "trace_id", Value: smValue},
				{Name: "__error__", Value: "shouldbeignored"},
			},
		},
	}}

	// Stream label value is short, so the stream-label path falls back to line.
	sawSM, sawLine := false, false
	for seed := int64(0); seed < 50 && (!sawSM || !sawLine); seed++ {
		svc.randMu.Lock()
		svc.rand = rand.New(rand.NewSource(seed))
		svc.randMu.Unlock()

		h, ok := svc.pickHintQuery(`{job="api"}`, "job", "api", logs)
		require.True(t, ok)
		require.NotNil(t, h.expr)
		switch h.queryType {
		case hintQueryTypeSMLabelFilter:
			sawSM = true
			require.Equal(t, smValue, h.needle)
			require.Equal(t, `{job="api"} | trace_id="traceidvalue99"`, h.query)
		case hintQueryTypeLineFilter:
			sawLine = true
		default:
			t.Fatalf("unexpected query type %q", h.queryType)
		}
	}
	require.True(t, sawSM, "expected some seeds to pick sm_label_filter")
	require.True(t, sawLine, "expected some seeds to pick line_filter")
}

func TestPickHintQuery_IneligibleSMFallsBackToLineNotLabel(t *testing.T) {
	indexStore := newTestStore(t)
	loki := newTestLokiServer(t, time.Now(), "9fA81cD2Ef0077aa")
	defer loki.Close()
	svc := newTestService(t, indexStore, loki.URL, time.Date(2026, 2, 26, 10, 0, 0, 0, time.UTC))

	// SM present but unusable; stream label is long enough that the label path
	// would succeed if tried. SM-first must fall back to line, not label.
	streamValue := "checkout-service"
	logs := []queryEntry{{
		Entry: logproto.Entry{
			Line: "request trace=9fA81cD2Ef0077aa completed",
			StructuredMetadata: []logproto.LabelAdapter{
				{Name: "__error__", Value: "shouldbeignored"},
				{Name: "trace_id", Value: "short"},
			},
		},
	}}

	sawSMFirst := false
	for seed := int64(0); seed < 100; seed++ {
		if rand.New(rand.NewSource(seed)).Intn(4) != 0 {
			continue
		}
		sawSMFirst = true

		svc.randMu.Lock()
		svc.rand = rand.New(rand.NewSource(seed))
		svc.randMu.Unlock()

		h, ok := svc.pickHintQuery(`{job="checkout-service"}`, "job", streamValue, logs)
		require.True(t, ok)
		require.Equal(t, hintQueryTypeLineFilter, h.queryType,
			"SM-first with ineligible SM must fall back to line_filter, not label_filter")
		require.Equal(t, "9fA81cD2Ef0077aa", h.needle)
	}
	require.True(t, sawSMFirst, "expected at least one seed that picks the SM branch first")
}

type unsupportedHintProvider struct{}

func (unsupportedHintProvider) ProvideHints(
	context.Context,
	string,
	syntax.Expr,
	model.Time,
	model.Time,
) (*hintprovider.Hints, *hintprovider.QueryStats, error) {
	return nil, nil, hintprovider.ErrUnsupported
}

func newTestService(t *testing.T, indexStore *store.Store, endpoint string, start time.Time) *Service {
	t.Helper()

	cfg := Config{
		LokiQueryEndpoints:   []string{endpoint},
		TenantID:             "test-tenant",
		QueryInterval:        1 * time.Second,
		QueryIngestersWithin: 1 * time.Minute,
		QueryRangeMin:        2 * time.Minute,
		QueryRangeMax:        2 * time.Minute,
		RequestTimeout:       5 * time.Second,
		CycleTimeout:         20 * time.Second,
		ErrorBackoffMin:      1 * time.Second,
		ErrorBackoffMax:      2 * time.Second,
		LogQueryLimit:        100,
		NgramLength:          6,
	}

	svc, err := New(indexStore, cfg, log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)
	svc.startedAt = start
	svc.nowFn = func() time.Time {
		return start.Add(3 * time.Minute)
	}
	return svc
}

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	bucket := objstore.NewInMemBucket()
	indexStore, err := store.NewStore(bucket, store.Config{MinDate: "0001-01-01"}, log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)
	return indexStore
}

func writeTestIndex(t *testing.T, indexStore *store.Store, hash, needle string, docMin, docMax time.Time) {
	t.Helper()
	writeTestIndexWithRecordTs(t, indexStore, hash, needle, docMin, docMax, docMin, docMax)
}

func writeTestIndexWithRecordTs(
	t *testing.T,
	indexStore *store.Store,
	hash, needle string,
	docMin, docMax, minRec, maxRec time.Time,
) {
	t.Helper()
	indexBytes, headerInfo := buildIndexBytes(t, needle, docMin, docMax)

	meta := store.Meta{
		Date:        docMin.UTC().Format("2006-01-02"),
		Hash:        hash,
		Version:     "v3",
		MinLogTs:    docMin.UTC(),
		MaxLogTs:    docMax.UTC(),
		MinRecordTs: minRec.UTC(),
		MaxRecordTs: maxRec.UTC(),
		IndexHeader: headerInfo,
		SizeBytes:   int64(len(indexBytes)),
	}

	require.NoError(t, indexStore.PutIndex(context.Background(), bytes.NewReader(indexBytes), meta))
	require.NoError(t, indexStore.Poll(context.Background()))
}

func buildIndexBytes(t *testing.T, needle string, docMin, docMax time.Time) ([]byte, *format.HeaderInfo) {
	t.Helper()
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.lidx")

	docs := []format.DocumentMetadata{
		{ID: 0, MinTimeUnix: docMin.UnixMilli(), MaxTimeUnix: docMax.UnixMilli()},
	}
	writer, err := logline.NewWriter("v3", path, docs, nil)
	require.NoError(t, err)

	terms, err := hintprovider.ExtractQueryNgrams(needle, 6, "v3")
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

func TestRunning_CompletesTests(t *testing.T) {
	base := time.Date(2026, 2, 26, 10, 0, 0, 0, time.UTC)
	witnessTS := base.Add(1 * time.Minute)
	needle := "9fA81cD2Ef0077aa"

	loki := newTestLokiServer(t, witnessTS, needle)
	defer loki.Close()

	indexStore := newTestStore(t)
	writeTestIndex(t, indexStore, "aaaaaaaaaaaaaaaa", needle, base.Add(50*time.Second), base.Add(70*time.Second))

	cfg := Config{
		LokiQueryEndpoints:   []string{loki.URL},
		TenantID:             "test-tenant",
		QueryInterval:        time.Second,
		QueryIngestersWithin: 1 * time.Minute,
		QueryRangeMin:        2 * time.Minute,
		QueryRangeMax:        2 * time.Minute,
		RequestTimeout:       5 * time.Second,
		CycleTimeout:         20 * time.Second,
		ErrorBackoffMin:      1 * time.Second,
		ErrorBackoffMax:      2 * time.Second,
		LogQueryLimit:        100,
		NgramLength:          6,
	}

	reg := prometheus.NewRegistry()
	svc, err := New(indexStore, cfg, log.NewNopLogger(), reg)
	require.NoError(t, err)
	svc.startedAt = base
	svc.nowFn = func() time.Time {
		return base.Add(3 * time.Minute)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	require.NoError(t, svc.StartAsync(ctx))
	require.NoError(t, svc.AwaitRunning(ctx))

	// Let cycles run, then stop via context cancellation.
	<-ctx.Done()
	svc.StopAsync()
	require.NoError(t, svc.AwaitTerminated(context.Background()))

	mfs, err := reg.Gather()
	require.NoError(t, err)
	var testsTotal float64
	for _, mf := range mfs {
		if mf.GetName() == "logline_index_correctness_tests_total" {
			testsTotal = mf.GetMetric()[0].GetCounter().GetValue()
		}
	}
	require.GreaterOrEqual(t, testsTotal, float64(1), "service should complete at least one test")
}

func newTestLokiServer(t *testing.T, witnessTS time.Time, needle string) *httptest.Server {
	t.Helper()

	writeEnvelope := func(w http.ResponseWriter, data any) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"data":   data,
		}))
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "test-tenant", r.Header.Get("X-Scope-OrgID"))

		switch r.URL.Path {
		case "/loki/api/v1/labels":
			writeEnvelope(w, []string{"job"})
			return
		case "/loki/api/v1/label/job/values":
			writeEnvelope(w, []string{"api"})
			return
		case "/loki/api/v1/query_range":
			startNS, err := strconv.ParseInt(r.URL.Query().Get("start"), 10, 64)
			require.NoError(t, err)
			endNS, err := strconv.ParseInt(r.URL.Query().Get("end"), 10, 64)
			require.NoError(t, err)

			withinRange := witnessTS.UnixNano() >= startNS && witnessTS.UnixNano() <= endNS

			result := []map[string]any{}
			if withinRange {
				result = append(result, map[string]any{
					"stream": map[string]string{"job": "api"},
					"values": [][]string{{strconv.FormatInt(witnessTS.UnixNano(), 10), "request trace=" + needle + " completed"}},
				})
			}

			writeEnvelope(w, map[string]any{
				"resultType": "streams",
				"result":     result,
			})
			return
		default:
			http.NotFound(w, r)
			return
		}
	}))
}

func TestPickRandomRange_ClampsToMaxLookback(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	startedAt := now.Add(-60 * 24 * time.Hour) // older than max lookback
	maxLookback := 30 * 24 * time.Hour
	rangeDur := 10 * time.Minute

	svc := &Service{
		cfg: Config{
			QueryIngestersWithin: 3 * time.Hour,
			QueryRangeMin:        rangeDur,
			QueryRangeMax:        rangeDur,
			MaxLookback:          maxLookback,
		},
		startedAt: startedAt,
		rand:      rand.New(rand.NewSource(1)),
	}

	earliest := now.Add(-maxLookback)
	eligibleEnd := now.Add(-3 * time.Hour)

	for i := 0; i < 50; i++ {
		start, end, skip := svc.pickRandomRange(now)
		require.Empty(t, skip)
		require.False(t, start.Before(earliest), "start %v before earliest %v", start, earliest)
		require.True(t, start.After(startedAt), "expected clamp past started_at")
		require.Equal(t, rangeDur, end.Sub(start))
		require.False(t, end.After(eligibleEnd))
	}
}

func TestPickRandomRange_RespectsRecentStartedAt(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	startedAt := now.Add(-7 * 24 * time.Hour) // more recent than max lookback
	maxLookback := 30 * 24 * time.Hour
	rangeDur := 10 * time.Minute

	svc := &Service{
		cfg: Config{
			QueryIngestersWithin: 3 * time.Hour,
			QueryRangeMin:        rangeDur,
			QueryRangeMax:        rangeDur,
			MaxLookback:          maxLookback,
		},
		startedAt: startedAt,
		rand:      rand.New(rand.NewSource(1)),
	}

	for i := 0; i < 50; i++ {
		start, end, skip := svc.pickRandomRange(now)
		require.Empty(t, skip)
		require.False(t, start.Before(startedAt), "start %v before started_at %v", start, startedAt)
		require.Equal(t, rangeDur, end.Sub(start))
	}
}
