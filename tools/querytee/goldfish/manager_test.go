package goldfish

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/loki/v3/pkg/goldfish"
	"github.com/grafana/loki/v3/pkg/storage/bucket"
	"github.com/grafana/loki/v3/tools/querytee/comparator"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockStorage implements Storage interface for testing
type mockStorage struct {
	samples []goldfish.QuerySample
	results []goldfish.ComparisonResult
	closed  bool
}

func (m *mockStorage) StoreQuerySample(_ context.Context, sample *goldfish.QuerySample, comparison *goldfish.ComparisonResult) error {
	sample.ComparisonStatus = comparison.ComparisonStatus
	m.samples = append(m.samples, *sample)
	return nil
}

func (m *mockStorage) StoreComparisonResult(_ context.Context, result *goldfish.ComparisonResult) error {
	m.results = append(m.results, *result)
	return nil
}

func (m *mockStorage) GetSampledQueries(_ context.Context, page, pageSize int, _ goldfish.QueryFilter) (*goldfish.APIResponse, error) {
	// This is only used for UI, not needed in manager tests
	return &goldfish.APIResponse{
		Queries:  []goldfish.QuerySample{},
		HasMore:  false,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

func (m *mockStorage) GetStatistics(_ context.Context, _ goldfish.StatsFilter) (*goldfish.Statistics, error) {
	return nil, nil
}

func (m *mockStorage) Close() error {
	m.closed = true
	return nil
}

func (m *mockStorage) GetQueryByCorrelationID(_ context.Context, _ string) (*goldfish.QuerySample, error) {
	// This is only used for UI, not needed in manager tests
	return nil, nil
}

func TestManager_ShouldSample(t *testing.T) {
	tests := []struct {
		name       string
		config     Config
		tenantID   string
		wantSample bool
	}{
		{
			name: "disabled manager",
			config: Config{
				Enabled: false,
			},
			tenantID:   "tenant1",
			wantSample: false,
		},
		{
			name: "default rate 0",
			config: Config{
				Enabled: true,
				SamplingConfig: SamplingConfig{
					DefaultRate: 0.0,
				},
			},
			tenantID:   "tenant1",
			wantSample: false,
		},
		{
			name: "default rate 1",
			config: Config{
				Enabled: true,
				SamplingConfig: SamplingConfig{
					DefaultRate: 1.0,
				},
			},
			tenantID:   "tenant1",
			wantSample: true,
		},
		{
			name: "tenant specific rate overrides default",
			config: Config{
				Enabled: true,
				SamplingConfig: SamplingConfig{
					DefaultRate: 0.0,
					TenantRules: map[string]float64{
						"tenant1": 1.0,
					},
				},
			},
			tenantID:   "tenant1",
			wantSample: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := &mockStorage{}
			manager, err := NewManager(tt.config, testComparator(), storage, nil, log.NewNopLogger(), prometheus.NewRegistry())
			require.NoError(t, err)

			got := manager.ShouldSample(tt.tenantID)
			assert.Equal(t, tt.wantSample, got)
		})
	}
}

func TestManager_ProcessQueryPair(t *testing.T) {
	config := Config{
		Enabled: true,
		SamplingConfig: SamplingConfig{
			DefaultRate: 1.0,
		},
	}

	storage := &mockStorage{}
	manager, err := NewManager(config, testComparator(), storage, nil, log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)

	req, _ := http.NewRequest("GET", "/loki/api/v1/query_range?query=count_over_time({job=\"test\"}[5m])&start=1700000000&end=1700001000&step=60s", nil)
	req.Header.Set("X-Scope-OrgID", "tenant1")

	cellAResp := &BackendResponse{
		BackendName: "cell-a",
		Status:      200,
		Body:        []byte(`{"status":"success","data":{"resultType":"matrix","result":[],"stats":{"summary":{"execTime":0.1,"queueTime":0.05,"totalBytesProcessed":1000,"totalLinesProcessed":100,"bytesProcessedPerSecond":10000,"linesProcessedPerSecond":1000,"totalEntriesReturned":5,"splits":1,"shards":2}}}}`),
		Duration:    100 * time.Millisecond,
		TraceID:     "",
		SpanID:      "",
	}

	cellBResp := &BackendResponse{
		BackendName: "cell-b",
		Status:      200,
		Body:        []byte(`{"status":"success","data":{"resultType":"matrix","result":[],"stats":{"summary":{"execTime":0.12,"queueTime":0.06,"totalBytesProcessed":1000,"totalLinesProcessed":100,"bytesProcessedPerSecond":8333,"linesProcessedPerSecond":833,"totalEntriesReturned":5,"splits":1,"shards":2}}},"warnings":["Query was executed using the new experimental query engine and dataobj storage."]}`),
		Duration:    120 * time.Millisecond,
		TraceID:     "",
		SpanID:      "",
	}

	manager.SendToGoldfish(req, cellAResp, cellBResp)

	// Give async processing time to complete
	time.Sleep(100 * time.Millisecond)

	// Verify sample was stored
	assert.Len(t, storage.samples, 1)
	sample := storage.samples[0]
	assert.Equal(t, "tenant1", sample.TenantID)
	assert.Equal(t, "count_over_time({job=\"test\"}[5m])", sample.Query)
	assert.Equal(t, "query_range", sample.QueryType)
	assert.Equal(t, 200, sample.CellAStatusCode)
	assert.Equal(t, 200, sample.CellBStatusCode)
	assert.Equal(t, int64(100), sample.CellAStats.ExecTimeMs)
	assert.Equal(t, int64(120), sample.CellBStats.ExecTimeMs)
	assert.Equal(t, false, sample.CellAUsedNewEngine)
	assert.Equal(t, true, sample.CellBUsedNewEngine)

	// Verify comparison result was stored
	assert.Len(t, storage.results, 1)
	result := storage.results[0]
	assert.Equal(t, goldfish.ComparisonStatusMatch, result.ComparisonStatus)
	assert.Equal(t, sample.CorrelationID, result.CorrelationID)
	assert.Equal(t, time.Duration(100)*time.Millisecond, result.PerformanceMetrics.CellAQueryTime)
	assert.Equal(t, time.Duration(120)*time.Millisecond, result.PerformanceMetrics.CellBQueryTime)
}

func Test_CaptureResponse_withTraceID(t *testing.T) {
	tests := []struct {
		name     string
		traceID  string
		expected string
	}{
		{
			name:     "captures trace ID when provided",
			traceID:  "test-trace-123",
			expected: "test-trace-123",
		},
		{
			name:     "handles empty trace ID",
			traceID:  "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock HTTP response
			resp := &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(`{"status":"success","data":{"resultType":"matrix","result":[]}}`)),
			}

			// Call CaptureResponse with traceID and empty spanID
			data, err := CaptureResponse(resp, time.Duration(100)*time.Millisecond, tt.traceID, "", log.NewNopLogger())

			require.NoError(t, err)
			assert.Equal(t, tt.expected, data.TraceID)
			assert.Equal(t, 200, data.StatusCode)
			assert.Equal(t, time.Duration(100)*time.Millisecond, data.Duration)
		})
	}
}

func Test_ProcessQueryPair_populatesTraceIDs(t *testing.T) {
	config := Config{
		Enabled: true,
		SamplingConfig: SamplingConfig{
			DefaultRate: 1.0,
		},
	}

	storage := &mockStorage{}
	manager, err := NewManager(config, testComparator(), storage, nil, log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)

	req, _ := http.NewRequest("GET", "/loki/api/v1/query_range?query=count_over_time({job=\"test\"}[5m])&start=1700000000&end=1700001000&step=60s", nil)
	req.Header.Set("X-Scope-OrgID", "tenant1")

	cellAResp := &BackendResponse{
		BackendName: "cell-a",
		Status:      200,
		Body:        []byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`),
		Duration:    100 * time.Millisecond,
		TraceID:     "trace-cell-a-123",
		SpanID:      "",
	}

	cellBResp := &BackendResponse{
		BackendName: "cell-b",
		Status:      200,
		Body:        []byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`),
		Duration:    120 * time.Millisecond,
		TraceID:     "trace-cell-b-456",
		SpanID:      "",
	}

	manager.SendToGoldfish(req, cellAResp, cellBResp)

	// Give async processing time to complete
	time.Sleep(100 * time.Millisecond)

	// Verify sample was stored with trace IDs
	assert.Len(t, storage.samples, 1)
	sample := storage.samples[0]
	assert.Equal(t, "trace-cell-a-123", sample.CellATraceID)
	assert.Equal(t, "trace-cell-b-456", sample.CellBTraceID)
}

func TestManager_Close(t *testing.T) {
	storage := &mockStorage{}
	manager, err := NewManager(Config{Enabled: true}, testComparator(), storage, nil, log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)

	err = manager.Close()
	assert.NoError(t, err)
	assert.True(t, storage.closed)
}

func TestProcessQueryPairCapturesUser(t *testing.T) {
	tests := []struct {
		name         string
		queryTags    string
		expectedUser string
	}{
		{
			name:         "captures user from query tags",
			queryTags:    "Source=grafana,user=test.user",
			expectedUser: "test.user",
		},
		{
			name:         "defaults to unknown when no user",
			queryTags:    "Source=grafana,dashboard_id=123",
			expectedUser: "unknown",
		},
		{
			name:         "defaults to unknown when no tags",
			queryTags:    "",
			expectedUser: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := Config{
				Enabled: true,
				SamplingConfig: SamplingConfig{
					DefaultRate: 1.0,
				},
			}

			storage := &mockStorage{}
			manager, err := NewManager(config, testComparator(), storage, nil, log.NewNopLogger(), prometheus.NewRegistry())
			require.NoError(t, err)

			req, _ := http.NewRequest("GET", "/loki/api/v1/query_range?query=count_over_time({job=\"test\"}[5m])&start=1700000000&end=1700001000&step=60s", nil)
			req.Header.Set("X-Scope-OrgID", "tenant1")
			if tt.queryTags != "" {
				req.Header.Set("X-Query-Tags", tt.queryTags)
			}

			cellAResp := &BackendResponse{
				BackendName: "cell-a",
				Status:      200,
				Body:        []byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`),
				Duration:    100 * time.Millisecond,
				TraceID:     "",
				SpanID:      "",
			}

			cellBResp := &BackendResponse{
				BackendName: "cell-b",
				Status:      200,
				Body:        []byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`),
				Duration:    120 * time.Millisecond,
				TraceID:     "",
				SpanID:      "",
			}

			manager.SendToGoldfish(req, cellAResp, cellBResp)

			// Give async processing time to complete
			time.Sleep(100 * time.Millisecond)

			// Verify sample was stored with correct user
			require.Len(t, storage.samples, 1)
			sample := storage.samples[0]
			assert.Equal(t, tt.expectedUser, sample.User, "User field should be captured from X-Query-Tags header")
		})
	}
}

func TestExtractUserFromQueryTags(t *testing.T) {
	tests := []struct {
		name         string
		queryTags    string
		expectedUser string
	}{
		{
			name:         "header with user",
			queryTags:    "Source=grafana,user=john.doe",
			expectedUser: "john.doe",
		},
		{
			name:         "header without user",
			queryTags:    "Source=grafana,dashboard_id=123",
			expectedUser: "unknown",
		},
		{
			name:         "empty header",
			queryTags:    "",
			expectedUser: "unknown",
		},
		{
			name:         "header with user in different position",
			queryTags:    "dashboard_id=123,user=jane.smith,panel_id=456",
			expectedUser: "jane.smith",
		},
		{
			name:         "header with user containing special characters",
			queryTags:    "Source=grafana,user=john.doe@example.com",
			expectedUser: "john.doe@example.com",
		},
		{
			name:         "header with malformed user tag (spaces around equals)",
			queryTags:    "Source=grafana,user = test.user",
			expectedUser: "unknown", // httpreq.TagsToKeyValues ignores malformed tags
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest("GET", "/test", nil)
			if tt.queryTags != "" {
				req.Header.Set("X-Query-Tags", tt.queryTags)
			}

			got := ExtractUserFromQueryTags(req)
			assert.Equal(t, tt.expectedUser, got)
		})
	}
}

func TestProcessQueryPair_CapturesLogsDrilldown(t *testing.T) {
	tests := []struct {
		name              string
		queryTags         string
		expectedDrilldown bool
	}{
		{
			name:              "query with logs drilldown source first",
			queryTags:         "source=grafana-lokiexplore-app,user=john.doe",
			expectedDrilldown: true,
		},
		{
			name:              "query with logs drilldown source last",
			queryTags:         "user=john.doe,source=grafana-lokiexplore-app",
			expectedDrilldown: true,
		},
		{
			name:              "query with different source",
			queryTags:         "source=grafana,user=john.doe",
			expectedDrilldown: false,
		},
		{
			name:              "query without source",
			queryTags:         "user=john.doe",
			expectedDrilldown: false,
		},
		{
			name:              "query without query tags",
			queryTags:         "",
			expectedDrilldown: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := Config{
				Enabled: true,
				SamplingConfig: SamplingConfig{
					DefaultRate: 1.0,
				},
			}

			storage := &mockStorage{}
			manager, err := NewManager(config, testComparator(), storage, nil, log.NewNopLogger(), prometheus.NewRegistry())
			require.NoError(t, err)

			req, _ := http.NewRequest("GET", "/loki/api/v1/query_range?query=count_over_time({job=\"test\"}[5m])&start=1700000000&end=1700001000&step=60s", nil)
			req.Header.Set("X-Scope-OrgID", "tenant1")
			if tt.queryTags != "" {
				req.Header.Set("X-Query-Tags", tt.queryTags)
			}

			cellAResp := &BackendResponse{
				BackendName: "cell-a",
				Status:      200,
				Body:        []byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`),
				Duration:    100 * time.Millisecond,
				TraceID:     "",
				SpanID:      "",
			}

			cellBResp := &BackendResponse{
				BackendName: "cell-b",
				Status:      200,
				Body:        []byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`),
				Duration:    120 * time.Millisecond,
				TraceID:     "",
				SpanID:      "",
			}

			manager.SendToGoldfish(req, cellAResp, cellBResp)

			// Give async processing time to complete
			time.Sleep(100 * time.Millisecond)

			// Verify sample was stored with correct logs drilldown flag
			require.Len(t, storage.samples, 1)
			sample := storage.samples[0]
			assert.Equal(t, tt.expectedDrilldown, sample.IsLogsDrilldown, "IsLogsDrilldown field should be captured from X-Query-Tags header")
		})
	}
}

func TestManagerResultPersistenceModes(t *testing.T) {
	baseConfig := Config{
		Enabled: true,
		SamplingConfig: SamplingConfig{
			DefaultRate: 1.0,
		},
		StorageConfig: StorageConfig{
			Type:             "cloudsql",
			CloudSQLUser:     "user",
			CloudSQLDatabase: "db",
		},
		ResultsStorage: ResultsStorageConfig{
			Enabled:     true,
			Backend:     ResultsBackendGCS,
			Compression: ResultsCompressionGzip,
			Bucket:      bucket.Config{},
		},
	}
	baseConfig.ResultsStorage.Bucket.GCS.BucketName = "bucket"

	tests := []struct {
		name         string
		mode         ResultsPersistenceMode
		cellABody    []byte
		cellBBody    []byte
		expectStores int
	}{
		{
			name:         "mismatch only stores when hashes differ",
			mode:         ResultsPersistenceModeMismatchOnly,
			cellABody:    []byte(`{"status":"success-a","data":{"resultType":"matrix","result":[],"stats":{"summary":{"execTime":0.09,"queueTime":0.05,"totalBytesProcessed":1000,"totalLinesProcessed":100,"bytesProcessedPerSecond":10000,"linesProcessedPerSecond":1000,"totalEntriesReturned":5,"splits":1,"shards":2}}}}`),
			cellBBody:    []byte(`{"status":"success-b","data":{"resultType":"matrix","result":[],"stats":{"summary":{"execTime":0.11,"queueTime":0.06,"totalBytesProcessed":1000,"totalLinesProcessed":100,"bytesProcessedPerSecond":8333,"linesProcessedPerSecond":833,"totalEntriesReturned":5,"splits":1,"shards":2}}}}`),
			expectStores: 2,
		},
		{
			name:         "mismatch only skips identical hashes",
			mode:         ResultsPersistenceModeMismatchOnly,
			cellABody:    []byte(`{"status":"success","data":{"resultType":"matrix","result":[],"stats":{"summary":{"execTime":0.09,"queueTime":0.05,"totalBytesProcessed":1000,"totalLinesProcessed":100,"bytesProcessedPerSecond":10000,"linesProcessedPerSecond":1000,"totalEntriesReturned":5,"splits":1,"shards":2}}}}`),
			cellBBody:    []byte(`{"status":"success","data":{"resultType":"matrix","result":[],"stats":{"summary":{"execTime":0.11,"queueTime":0.06,"totalBytesProcessed":1000,"totalLinesProcessed":100,"bytesProcessedPerSecond":8333,"linesProcessedPerSecond":833,"totalEntriesReturned":5,"splits":1,"shards":2}}}}`),
			expectStores: 0,
		},
		{
			name:         "all mode stores for every sample",
			mode:         ResultsPersistenceModeAll,
			cellABody:    []byte(`{"status":"success","data":{"resultType":"matrix","result":[],"stats":{"summary":{"execTime":0.09,"queueTime":0.05,"totalBytesProcessed":1000,"totalLinesProcessed":100,"bytesProcessedPerSecond":10000,"linesProcessedPerSecond":1000,"totalEntriesReturned":5,"splits":1,"shards":2}}}}`),
			cellBBody:    []byte(`{"status":"success","data":{"resultType":"matrix","result":[],"stats":{"summary":{"execTime":0.11,"queueTime":0.06,"totalBytesProcessed":1000,"totalLinesProcessed":100,"bytesProcessedPerSecond":8333,"linesProcessedPerSecond":833,"totalEntriesReturned":5,"splits":1,"shards":2}}}}`),
			expectStores: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := baseConfig
			config.ResultsStorage.Mode = tt.mode

			storage := &mockStorage{}
			results := &mockResultStore{}
			manager, err := NewManager(config, testComparator(), storage, results, log.NewNopLogger(), prometheus.NewRegistry())
			require.NoError(t, err)

			req, _ := http.NewRequest("GET", "/loki/api/v1/query_range?query=sum(rate({job=\"app\"}[1m]))", nil)
			req.Header.Set("X-Scope-OrgID", "tenant1")

			cellA := &BackendResponse{
				BackendName: "cell-a",
				Status:      200,
				Body:        tt.cellABody,
				Duration:    90 * time.Millisecond,
				TraceID:     "",
				SpanID:      "",
			}

			cellB := &BackendResponse{
				BackendName: "cell-b",
				Status:      200,
				Body:        tt.cellBBody,
				Duration:    110 * time.Millisecond,
				TraceID:     "",
				SpanID:      "",
			}

			manager.SendToGoldfish(req, cellA, cellB)

			// Give async processing time to complete
			time.Sleep(100 * time.Millisecond)

			require.Equal(t, tt.expectStores, len(results.calls))

			if tt.expectStores > 0 {
				require.Len(t, storage.samples, 1)
				sample := storage.samples[0]
				assert.NotEmpty(t, sample.CellAResultURI)
				assert.NotEmpty(t, sample.CellBResultURI)
				assert.Equal(t, "cell-a", results.calls[0].opts.CellLabel)
				if tt.expectStores == 2 {
					assert.Equal(t, "cell-b", results.calls[1].opts.CellLabel)
				}
			}
		})
	}
}

type resultStoreCall struct {
	opts         StoreOptions
	originalSize int64
}

type mockResultStore struct {
	calls  []resultStoreCall
	closed bool
}

func (m *mockResultStore) Store(_ context.Context, payload []byte, opts StoreOptions) (*StoredResult, error) {
	m.calls = append(m.calls, resultStoreCall{opts: opts, originalSize: int64(len(payload))})
	uri := "mock://" + opts.CellLabel + "/" + opts.CorrelationID
	return &StoredResult{
		URI:          uri,
		Size:         int64(len(payload)),
		OriginalSize: int64(len(payload)),
		Compression:  ResultsCompressionGzip,
	}, nil
}

func (m *mockResultStore) Close(context.Context) error {
	m.closed = true
	return nil
}

// mockResponseComparator implements ResponsesComparator for testing
type mockResponseComparator struct {
	match bool
}

func (m *mockResponseComparator) Compare(_, _ []byte, _ time.Time) (*comparator.ComparisonSummary, error) {
	if m.match {
		return &comparator.ComparisonSummary{}, nil
	}
	return nil, errors.New("comparison failed")
}

func TestManager_StoreQuerySample_UsesComparatorResult(t *testing.T) {
	tests := []struct {
		name            string
		cellAHash       string
		cellBHash       string
		comparatorMatch bool
		expectedStatus  goldfish.ComparisonStatus
	}{
		{
			name:            "hash mismatch with tolerance match",
			cellAHash:       "hash1",
			cellBHash:       "hash2",
			comparatorMatch: true,
			expectedStatus:  goldfish.ComparisonStatusMatch,
		},
		{
			name:            "hash mismatch without tolerance match",
			cellAHash:       "hash1",
			cellBHash:       "hash2",
			comparatorMatch: false,
			expectedStatus:  goldfish.ComparisonStatusMismatch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := &mockStorage{}

			mockComparator := &mockResponseComparator{
				match: tt.comparatorMatch,
			}

			config := Config{
				Enabled: true,
				ResultsStorage: ResultsStorageConfig{
					Mode: ResultsPersistenceModeAll,
				},
				PerformanceTolerance: 0.1,
			}

			manager, err := NewManager(config, mockComparator, storage, nil, log.NewNopLogger(), prometheus.NewRegistry())
			require.NoError(t, err)

			// Create responses with different status values to ensure different hashes
			// Use status field differences since result parsing is complex
			cellAResp := &BackendResponse{
				BackendName: "cell-a",
				Status:      200,
				Body:        []byte(`{"status":"success-a","data":{"resultType":"matrix","result":[],"stats":{"summary":{"execTime":0.1,"queueTime":0.05,"totalBytesProcessed":1000,"totalLinesProcessed":100,"bytesProcessedPerSecond":10000,"linesProcessedPerSecond":1000,"totalEntriesReturned":5,"splits":1,"shards":2}}}}`),
				Duration:    100 * time.Millisecond,
				TraceID:     "",
				SpanID:      "",
			}
			cellBResp := &BackendResponse{
				BackendName: "cell-b",
				Status:      200,
				Body:        []byte(`{"status":"success-b","data":{"resultType":"matrix","result":[],"stats":{"summary":{"execTime":0.1,"queueTime":0.05,"totalBytesProcessed":1000,"totalLinesProcessed":100,"bytesProcessedPerSecond":10000,"linesProcessedPerSecond":1000,"totalEntriesReturned":5,"splits":1,"shards":2}}}}`),
				Duration:    100 * time.Millisecond,
				TraceID:     "",
				SpanID:      "",
			}

			req, _ := http.NewRequest("GET", "/loki/api/v1/query_range?query=test", nil)

			// Process the query pair
			manager.SendToGoldfish(req, cellAResp, cellBResp)

			// Give async processing time to complete
			time.Sleep(100 * time.Millisecond)

			// Verify the stored sample has the correct comparison status from the comparator
			require.Len(t, storage.samples, 1)
			assert.Equal(t, tt.expectedStatus, storage.samples[0].ComparisonStatus,
				"comparison status should match what the comparator returned")
		})
	}
}
