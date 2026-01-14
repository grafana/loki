package ui

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/goldfish"
)

// mockStorage implements the goldfish.Storage interface for testing
type mockStorage struct {
	queries             []goldfish.QuerySample
	capturedFilter      goldfish.QueryFilter // Add this to capture the full filter
	capturedStatsFilter goldfish.StatsFilter // Capture stats filter for verification
	stats               *goldfish.Statistics // Mock statistics response
	error               error
	getQueryFunc        func(ctx context.Context, correlationID string) (*goldfish.QuerySample, error)
}

func (m *mockStorage) StoreQuerySample(_ context.Context, _ *goldfish.QuerySample, _ *goldfish.ComparisonResult) error {
	return nil
}

func (m *mockStorage) StoreComparisonResult(_ context.Context, _ *goldfish.ComparisonResult) error {
	return nil
}

func (m *mockStorage) GetSampledQueries(_ context.Context, page, pageSize int, filter goldfish.QueryFilter) (*goldfish.APIResponse, error) {
	if m.error != nil {
		return nil, m.error
	}

	// Capture the filter for test verification
	m.capturedFilter = filter

	// Apply pagination
	start := (page - 1) * pageSize
	end := start + pageSize
	if start > len(m.queries) {
		start = len(m.queries)
	}

	// Check if there are more records
	hasMore := end < len(m.queries)

	if end > len(m.queries) {
		end = len(m.queries)
	}

	return &goldfish.APIResponse{
		Queries:  m.queries[start:end],
		HasMore:  hasMore,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

func (m *mockStorage) GetQueryByCorrelationID(ctx context.Context, correlationID string) (*goldfish.QuerySample, error) {
	if m.getQueryFunc != nil {
		return m.getQueryFunc(ctx, correlationID)
	}
	if m.error != nil {
		return nil, m.error
	}
	// Default behavior: search through queries
	for _, q := range m.queries {
		if q.CorrelationID == correlationID {
			return &q, nil
		}
	}
	return nil, errors.New("query not found")
}

func (m *mockStorage) GetStatistics(_ context.Context, filter goldfish.StatsFilter) (*goldfish.Statistics, error) {
	if m.error != nil {
		return nil, m.error
	}

	// Capture the filter for test verification
	m.capturedStatsFilter = filter

	// Return mock statistics if provided
	if m.stats != nil {
		return m.stats, nil
	}

	// Return default statistics
	return &goldfish.Statistics{
		QueriesExecuted:       100,
		EngineCoverage:        0.75,
		MatchingQueries:       0.95,
		PerformanceDifference: -0.15,
	}, nil
}

func (m *mockStorage) Close() error {
	return nil
}

// Mock bucket client for testing
type mockBucket struct {
	getFunc func(ctx context.Context, key string) (io.ReadCloser, error)
}

func (m *mockBucket) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, key)
	}
	return nil, errors.New("not implemented")
}

func (m *mockBucket) Upload(_ context.Context, _ string, _ io.Reader) error {
	return nil
}

func (m *mockBucket) Delete(_ context.Context, _ string) error {
	return nil
}

func (m *mockBucket) Exists(_ context.Context, _ string) (bool, error) {
	return false, nil
}

func (m *mockBucket) Iter(_ context.Context, _ string, _ func(string) error, _ ...objstore.IterOption) error {
	return nil
}

func (m *mockBucket) GetRange(_ context.Context, _ string, _ int64, _ int64) (io.ReadCloser, error) {
	return nil, nil
}

func (m *mockBucket) Attributes(_ context.Context, _ string) (objstore.ObjectAttributes, error) {
	return objstore.ObjectAttributes{}, nil
}

func (m *mockBucket) ObjectSize(_ context.Context, _ string) (uint64, error) {
	return 0, nil
}

func (m *mockBucket) Close() error {
	return nil
}

func (m *mockBucket) Name() string {
	return "mock-bucket"
}

func (m *mockBucket) WithExpectedErrs(_ objstore.IsOpFailureExpectedFunc) objstore.Bucket {
	return m
}

func (m *mockBucket) ReaderWithExpectedErrs(_ objstore.IsOpFailureExpectedFunc) objstore.BucketReader {
	return m
}

func (m *mockBucket) SupportedIterOptions() []objstore.IterOptionType {
	return nil
}

func (m *mockBucket) Provider() objstore.ObjProvider {
	return objstore.ObjProvider("mock")
}

func (m *mockBucket) GetAndReplace(_ context.Context, _ string, _ func(existing io.ReadCloser) (io.ReadCloser, error)) error {
	return errors.New("not implemented")
}

func (m *mockBucket) IsAccessDeniedErr(_ error) bool {
	return false
}

func (m *mockBucket) IsObjNotFoundErr(_ error) bool {
	return false
}

func (m *mockBucket) IterWithAttributes(_ context.Context, _ string, _ func(objstore.IterObjectAttributes) error, _ ...objstore.IterOption) error {
	return nil
}

// createTestService creates a Service with common test defaults
func createTestService(storage goldfish.Storage) *Service {
	return createTestServiceWithConfig(storage, GoldfishConfig{
		Enable: true,
	})
}

// createTestServiceWithConfig creates a Service with a custom config
// storage can be nil for tests that don't need storage
func createTestServiceWithConfig(storage goldfish.Storage, cfg GoldfishConfig) *Service {
	return &Service{
		cfg: Config{
			Goldfish: cfg,
		},
		logger:          log.NewNopLogger(),
		goldfishStorage: storage,
		now:             time.Now,
	}
}

// createTestServiceWithNow creates a Service with a custom now function
func createTestServiceWithNow(storage goldfish.Storage, nowFunc func() time.Time) *Service {
	return &Service{
		cfg: Config{
			Goldfish: GoldfishConfig{
				Enable: true,
			},
		},
		logger:          log.NewNopLogger(),
		goldfishStorage: storage,
		now:             nowFunc,
	}
}

func createTestQuerySample(id, tenant string, statusA, statusB int, hashA, hashB string, comparisonStatus goldfish.ComparisonStatus) goldfish.QuerySample {
	return goldfish.QuerySample{
		CorrelationID:     id,
		TenantID:          tenant,
		Query:             "test query",
		QueryType:         "query_range",
		StartTime:         time.Date(2023, 1, 1, 10, 0, 0, 0, time.UTC),
		EndTime:           time.Date(2023, 1, 1, 11, 0, 0, 0, time.UTC),
		Step:              15 * time.Second,
		CellAStats:        goldfish.QueryStats{ExecTimeMs: 100},
		CellBStats:        goldfish.QueryStats{ExecTimeMs: 150},
		CellAResponseHash: hashA,
		CellBResponseHash: hashB,
		CellAResponseSize: 1000,
		CellBResponseSize: 1000,
		CellAStatusCode:   statusA,
		CellBStatusCode:   statusB,
		CellATraceID:      "trace-a-" + id,
		CellBTraceID:      "trace-b-" + id,
		SampledAt:         time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
		ComparisonStatus:  comparisonStatus,
	}
}

func TestGoldfishQueriesHandler_ComparisonStatusFilter(t *testing.T) {
	// Shared storage setup with all query types
	storage := &mockStorage{
		queries: []goldfish.QuerySample{
			createTestQuerySample("1", "tenant1", 200, 200, "hash1", "hash1", goldfish.ComparisonStatusMatch),
		},
	}

	service := createTestService(storage)
	handler := service.goldfishQueriesHandler()

	tests := []struct {
		name                   string
		url                    string
		expectedCapturedStatus goldfish.ComparisonStatus
	}{
		{
			name:                   "filters by match status",
			url:                    "/api/v1/goldfish/queries?comparisonStatus=match",
			expectedCapturedStatus: goldfish.ComparisonStatusMatch,
		},
		{
			name:                   "filters by mismatch status",
			url:                    "/api/v1/goldfish/queries?comparisonStatus=mismatch",
			expectedCapturedStatus: goldfish.ComparisonStatusMismatch,
		},
		{
			name:                   "filters by error status",
			url:                    "/api/v1/goldfish/queries?comparisonStatus=error",
			expectedCapturedStatus: goldfish.ComparisonStatusError,
		},
		{
			name:                   "defaults to no filter when parameter not provided",
			url:                    "/api/v1/goldfish/queries",
			expectedCapturedStatus: "", // empty/default
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset captured filter
			storage.capturedFilter = goldfish.QueryFilter{}

			req := httptest.NewRequest("GET", tt.url, nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			// Verify the filter was correctly captured by storage
			assert.Equal(t, tt.expectedCapturedStatus, storage.capturedFilter.ComparisonStatus)
		})
	}
}

func TestGoldfishQueriesHandler_PaginationAndInputValidation(t *testing.T) {
	// Create 25 test queries
	queries := make([]goldfish.QuerySample, 25)
	for i := range 25 {
		queries[i] = createTestQuerySample(string(rune('a'+i)), "tenant1", 200, 200, "hash1", "hash1", goldfish.ComparisonStatusMatch)
	}

	storage := &mockStorage{
		queries: queries,
	}

	service := createTestService(storage)

	handler := service.goldfishQueriesHandler()

	tests := []struct {
		name             string
		queryParams      string
		expectedPage     int
		expectedPageSize int
		expectedCount    int
		expectedHasMore  bool
	}{
		{
			name:             "default pagination",
			queryParams:      "",
			expectedPage:     1,
			expectedPageSize: 20,
			expectedCount:    20,
			expectedHasMore:  true, // 25 total, showing 20, so has more
		},
		{
			name:             "page 2 with custom size",
			queryParams:      "?page=2&pageSize=10",
			expectedPage:     2,
			expectedPageSize: 10,
			expectedCount:    10,
			expectedHasMore:  true, // page 2 shows items 10-19, still have 20-24
		},
		{
			name:             "invalid page defaults to 1",
			queryParams:      "?page=-5",
			expectedPage:     1,
			expectedPageSize: 20,
			expectedCount:    20,
			expectedHasMore:  true, // 25 total, showing 20, so has more
		},
		{
			name:             "excessive pageSize capped at 1000",
			queryParams:      "?pageSize=5000",
			expectedPage:     1,
			expectedPageSize: 1000,
			expectedCount:    25,
			expectedHasMore:  false, // showing all 25, no more
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/v1/goldfish/queries"+tt.queryParams, nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			assert.Equal(t, http.StatusOK, rr.Code)

			var response GoldfishAPIResponse
			err := json.Unmarshal(rr.Body.Bytes(), &response)
			require.NoError(t, err)

			assert.Equal(t, tt.expectedPage, response.Page)
			assert.Equal(t, tt.expectedPageSize, response.PageSize)
			assert.Len(t, response.Queries, tt.expectedCount)
			assert.Equal(t, tt.expectedHasMore, response.HasMore)
		})
	}
}

func TestGoldfishQueriesHandler_ReturnsTraceIDs(t *testing.T) {
	storage := &mockStorage{
		queries: []goldfish.QuerySample{
			createTestQuerySample("1", "tenant1", 200, 200, "hash1", "hash1", goldfish.ComparisonStatusMatch),
		},
	}

	service := createTestService(storage)

	handler := service.goldfishQueriesHandler()

	req := httptest.NewRequest("GET", "/api/v1/goldfish/queries", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	// Read the response body
	body, err := io.ReadAll(rr.Body)
	require.NoError(t, err)

	// Check that trace IDs are present in the JSON response
	assert.Contains(t, string(body), `"cellATraceID":"trace-a-1"`)
	assert.Contains(t, string(body), `"cellBTraceID":"trace-b-1"`)

	// Also verify through unmarshaling
	var response GoldfishAPIResponse
	err = json.Unmarshal(body, &response)
	require.NoError(t, err)
	assert.Len(t, response.Queries, 1)
	assert.NotNil(t, response.Queries[0].CellATraceID)
	assert.NotNil(t, response.Queries[0].CellBTraceID)
	assert.Equal(t, "trace-a-1", *response.Queries[0].CellATraceID)
	assert.Equal(t, "trace-b-1", *response.Queries[0].CellBTraceID)
}

func TestGoldfishQueriesHandler_ErrorCases(t *testing.T) {
	tests := []struct {
		name           string
		service        *Service
		expectedStatus int
		expectedError  string
	}{
		{
			name: "goldfish disabled",
			service: &Service{
				cfg: Config{
					Goldfish: GoldfishConfig{
						Enable: false,
					},
				},
				logger: log.NewNopLogger(),
			},
			expectedStatus: http.StatusNotFound,
			expectedError:  "goldfish feature is disabled",
		},
		{
			name: "storage error",
			service: &Service{
				cfg: Config{
					Goldfish: GoldfishConfig{
						Enable: true,
					},
				},
				logger: log.NewNopLogger(),
				goldfishStorage: &mockStorage{
					error: errors.New("database connection failed"),
				},
				now: time.Now,
			},
			expectedStatus: http.StatusInternalServerError,
			expectedError:  "failed to retrieve sampled queries",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := tt.service.goldfishQueriesHandler()
			req := httptest.NewRequest("GET", "/api/v1/goldfish/queries", nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			assert.Equal(t, tt.expectedStatus, rr.Code)
			assert.Contains(t, rr.Body.String(), tt.expectedError)
		})
	}
}

func TestGoldfishQueriesHandler_TraceIDLinks(t *testing.T) {
	t.Run("includes trace ID links with explore config", func(t *testing.T) {
		storage := &mockStorage{
			queries: []goldfish.QuerySample{
				createTestQuerySample("1", "tenant1", 200, 200, "hash1", "hash1", goldfish.ComparisonStatusMatch),
			},
		}

		service := createTestServiceWithConfig(storage, GoldfishConfig{
			Enable:              true,
			GrafanaURL:          "https://grafana.example.com",
			TracesDatasourceUID: "tempo-123",
		})

		handler := service.goldfishQueriesHandler()
		req := httptest.NewRequest("GET", "/api/v1/goldfish/queries", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var response GoldfishAPIResponse
		err := json.Unmarshal(rr.Body.Bytes(), &response)
		require.NoError(t, err)

		// Verify trace ID links are included
		assert.NotNil(t, response.Queries[0].CellATraceLink)
		assert.NotNil(t, response.Queries[0].CellBTraceLink)
		assert.Contains(t, *response.Queries[0].CellATraceLink, "trace-a-1")
		assert.Contains(t, *response.Queries[0].CellBTraceLink, "trace-b-1")
		assert.Contains(t, *response.Queries[0].CellATraceLink, "https://grafana.example.com/explore")
	})

	t.Run("excludes trace ID links without explore config", func(t *testing.T) {
		storage := &mockStorage{
			queries: []goldfish.QuerySample{
				createTestQuerySample("1", "tenant1", 200, 200, "hash1", "hash1", goldfish.ComparisonStatusMatch),
			},
		}

		service := createTestServiceWithConfig(storage, GoldfishConfig{
			Enable: true,
			// No explore configuration
		})

		handler := service.goldfishQueriesHandler()
		req := httptest.NewRequest("GET", "/api/v1/goldfish/queries", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var response GoldfishAPIResponse
		err := json.Unmarshal(rr.Body.Bytes(), &response)
		require.NoError(t, err)

		// Verify trace ID links are not included
		assert.Nil(t, response.Queries[0].CellATraceLink)
		assert.Nil(t, response.Queries[0].CellBTraceLink)
		// But trace IDs should still be present
		assert.NotNil(t, response.Queries[0].CellATraceID)
		assert.NotNil(t, response.Queries[0].CellBTraceID)
	})
}

func TestGenerateTraceExploreURL(t *testing.T) {
	t.Run("generates correct explore URL", func(t *testing.T) {
		service := createTestServiceWithConfig(nil, GoldfishConfig{
			GrafanaURL:          "https://grafana.example.com",
			TracesDatasourceUID: "tempo-123",
		})

		traceID := "abc123def456"
		sampledAt := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		exploreURL := service.GenerateTraceExploreURL(traceID, "", sampledAt)

		// Verify the URL structure
		assert.Contains(t, exploreURL, "https://grafana.example.com/explore")
		assert.Contains(t, exploreURL, "schemaVersion=1")
		assert.Contains(t, exploreURL, "tempo-123")
		assert.Contains(t, exploreURL, traceID)
		assert.Contains(t, exploreURL, "traceql")

		// Verify time range (5 minutes before and after)
		expectedFrom := sampledAt.Add(-5 * time.Minute).UTC().Format(time.RFC3339)
		expectedTo := sampledAt.Add(5 * time.Minute).UTC().Format(time.RFC3339)
		assert.Contains(t, exploreURL, url.QueryEscape(expectedFrom))
		assert.Contains(t, exploreURL, url.QueryEscape(expectedTo))
	})

	t.Run("returns empty string without config", func(t *testing.T) {
		service := createTestServiceWithConfig(nil, GoldfishConfig{
			// No GrafanaURL or TracesDatasourceUID
		})

		traceID := "abc123def456"
		sampledAt := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		exploreURL := service.GenerateTraceExploreURL(traceID, "", sampledAt)

		assert.Empty(t, exploreURL)
	})

	t.Run("returns empty string with partial config", func(t *testing.T) {
		// Only GrafanaURL
		service := createTestServiceWithConfig(nil, GoldfishConfig{
			GrafanaURL: "https://grafana.example.com",
			// No TracesDatasourceUID
		})

		sampledAt := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		exploreURL := service.GenerateTraceExploreURL("abc123", "", sampledAt)
		assert.Empty(t, exploreURL)

		// Only TracesDatasourceUID
		service2 := createTestServiceWithConfig(nil, GoldfishConfig{
			TracesDatasourceUID: "tempo-123",
			// No GrafanaURL
		})

		exploreURL2 := service2.GenerateTraceExploreURL("abc123", "", sampledAt)
		assert.Empty(t, exploreURL2)
	})
}

func TestGoldfishConfig_LogsExploreSettings(t *testing.T) {
	t.Run("accepts logs configuration", func(t *testing.T) {
		// Create storage with sample data
		storage := &mockStorage{
			queries: []goldfish.QuerySample{
				createTestQuerySample("1", "tenant1", 200, 200, "hash1", "hash1", goldfish.ComparisonStatusMatch),
			},
		}

		// Create service with logs explore configuration
		service := createTestServiceWithConfig(storage, GoldfishConfig{
			Enable:              true,
			GrafanaURL:          "https://grafana.example.com",
			TracesDatasourceUID: "tempo-123",
			LogsDatasourceUID:   "loki-456",
			CellANamespace:      "loki-ops-002",
			CellBNamespace:      "loki-ops-003",
		})

		// Get sampled queries
		response, err := service.GetSampledQueries(1, 10, goldfish.QueryFilter{})

		// Assert no error and configuration is accepted
		require.NoError(t, err)
		assert.NotNil(t, response)
		// We'll check for logs links once we add the fields
	})
}

func TestGenerateLogsExploreURL(t *testing.T) {
	t.Run("generates correct logs explore URL", func(t *testing.T) {
		service := createTestServiceWithConfig(nil, GoldfishConfig{
			GrafanaURL:        "https://grafana.example.com",
			LogsDatasourceUID: "loki-456",
			CellANamespace:    "loki-ops-002",
			CellBNamespace:    "loki-ops-003",
		})

		traceID := "abc123def456"
		sampledAt := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

		// Test Cell A URL
		urlA := service.GenerateLogsExploreURL(traceID, service.cfg.Goldfish.CellANamespace, sampledAt)
		assert.Contains(t, urlA, "https://grafana.example.com/explore")
		assert.Contains(t, urlA, "schemaVersion=1")
		assert.Contains(t, urlA, "loki-456")
		// Check for the URL-encoded job query pattern with trace ID
		// The query should contain the namespace pattern and the trace ID
		assert.Contains(t, urlA, "loki-ops-002")
		assert.Contains(t, urlA, "abc123def456")

		// Verify time range (5 minutes before and after)
		expectedFrom := sampledAt.Add(-5 * time.Minute).UTC().Format(time.RFC3339)
		expectedTo := sampledAt.Add(5 * time.Minute).UTC().Format(time.RFC3339)
		assert.Contains(t, urlA, url.QueryEscape(expectedFrom))
		assert.Contains(t, urlA, url.QueryEscape(expectedTo))

		// Test Cell B URL
		urlB := service.GenerateLogsExploreURL(traceID, service.cfg.Goldfish.CellBNamespace, sampledAt)
		// Check for the URL-encoded job query pattern with trace ID
		// The query should contain the namespace pattern and the trace ID
		assert.Contains(t, urlB, "loki-ops-003")
		assert.Contains(t, urlB, "abc123def456")
		assert.Contains(t, urlB, url.QueryEscape(expectedFrom))
		assert.Contains(t, urlB, url.QueryEscape(expectedTo))
	})

	t.Run("returns empty string without config", func(t *testing.T) {
		service := createTestServiceWithConfig(nil, GoldfishConfig{
			// No configuration
		})

		sampledAt := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		exploreURL := service.GenerateLogsExploreURL("abc123", "namespace", sampledAt)
		assert.Empty(t, exploreURL)
	})

	t.Run("returns empty string with partial config", func(t *testing.T) {
		// Only GrafanaURL
		service := createTestServiceWithConfig(nil, GoldfishConfig{
			GrafanaURL: "https://grafana.example.com",
			// No LogsDatasourceUID
		})

		sampledAt := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		exploreURL := service.GenerateLogsExploreURL("abc123", "namespace", sampledAt)
		assert.Empty(t, exploreURL)

		// Only LogsDatasourceUID
		service2 := createTestServiceWithConfig(nil, GoldfishConfig{
			LogsDatasourceUID: "loki-456",
			// No GrafanaURL
		})

		exploreURL2 := service2.GenerateLogsExploreURL("abc123", "namespace", sampledAt)
		assert.Empty(t, exploreURL2)
	})
}

func TestGoldfishQueriesHandler_LogsLinks(t *testing.T) {
	t.Run("includes logs links with complete logs config", func(t *testing.T) {
		storage := &mockStorage{
			queries: []goldfish.QuerySample{
				createTestQuerySample("1", "tenant1", 200, 200, "hash1", "hash1", goldfish.ComparisonStatusMatch),
			},
		}

		service := createTestServiceWithConfig(storage, GoldfishConfig{
			Enable:            true,
			GrafanaURL:        "https://grafana.example.com",
			LogsDatasourceUID: "loki-456",
			CellANamespace:    "loki-ops-002",
			CellBNamespace:    "loki-ops-003",
		})

		handler := service.goldfishQueriesHandler()
		req := httptest.NewRequest("GET", "/api/v1/goldfish/queries", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var response GoldfishAPIResponse
		err := json.Unmarshal(rr.Body.Bytes(), &response)
		require.NoError(t, err)

		// Verify logs links are included
		assert.NotNil(t, response.Queries[0].CellALogsLink)
		assert.NotNil(t, response.Queries[0].CellBLogsLink)
		assert.Contains(t, *response.Queries[0].CellALogsLink, "https://grafana.example.com/explore")
		assert.Contains(t, *response.Queries[0].CellBLogsLink, "https://grafana.example.com/explore")
		assert.Contains(t, *response.Queries[0].CellALogsLink, "loki-456")
		assert.Contains(t, *response.Queries[0].CellBLogsLink, "loki-456")
		// Check for namespace patterns
		assert.Contains(t, *response.Queries[0].CellALogsLink, url.QueryEscape("loki-ops-002/.*quer.*"))
		assert.Contains(t, *response.Queries[0].CellBLogsLink, url.QueryEscape("loki-ops-003/.*quer.*"))
	})

	t.Run("excludes logs links without logs config", func(t *testing.T) {
		storage := &mockStorage{
			queries: []goldfish.QuerySample{
				createTestQuerySample("1", "tenant1", 200, 200, "hash1", "hash1", goldfish.ComparisonStatusMatch),
			},
		}

		service := createTestServiceWithConfig(storage, GoldfishConfig{
			Enable: true,
			// No logs configuration
		})

		handler := service.goldfishQueriesHandler()
		req := httptest.NewRequest("GET", "/api/v1/goldfish/queries", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var response GoldfishAPIResponse
		err := json.Unmarshal(rr.Body.Bytes(), &response)
		require.NoError(t, err)

		// Verify logs links are not included
		assert.Nil(t, response.Queries[0].CellALogsLink)
		assert.Nil(t, response.Queries[0].CellBLogsLink)
		// But trace IDs should still be present
		assert.NotNil(t, response.Queries[0].CellATraceID)
		assert.NotNil(t, response.Queries[0].CellBTraceID)
	})

	t.Run("excludes logs links with partial logs config", func(t *testing.T) {
		storage := &mockStorage{
			queries: []goldfish.QuerySample{
				createTestQuerySample("1", "tenant1", 200, 200, "hash1", "hash1", goldfish.ComparisonStatusMatch),
			},
		}

		// Missing namespace configuration
		service := createTestServiceWithConfig(storage, GoldfishConfig{
			Enable:            true,
			GrafanaURL:        "https://grafana.example.com",
			LogsDatasourceUID: "loki-456",
			// Missing CellANamespace and CellBNamespace
		})

		handler := service.goldfishQueriesHandler()
		req := httptest.NewRequest("GET", "/api/v1/goldfish/queries", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var response GoldfishAPIResponse
		err := json.Unmarshal(rr.Body.Bytes(), &response)
		require.NoError(t, err)

		// Verify logs links are not included with partial config
		assert.Nil(t, response.Queries[0].CellALogsLink)
		assert.Nil(t, response.Queries[0].CellBLogsLink)
	})
}

func TestGoldfishQueriesHandler_PartialExploreConfig(t *testing.T) {
	storage := &mockStorage{
		queries: []goldfish.QuerySample{
			createTestQuerySample("1", "tenant1", 200, 200, "hash1", "hash1", goldfish.ComparisonStatusMatch),
		},
	}

	t.Run("partial config with only GrafanaURL", func(t *testing.T) {
		service := createTestServiceWithConfig(storage, GoldfishConfig{
			Enable:     true,
			GrafanaURL: "https://grafana.example.com",
			// Missing TracesDatasourceUID
		})

		response, err := service.GetSampledQueries(1, 10, goldfish.QueryFilter{})
		require.NoError(t, err)

		// Should not include trace links with partial config
		assert.Nil(t, response.Queries[0].CellATraceLink)
		assert.Nil(t, response.Queries[0].CellBTraceLink)
		// But trace IDs should still be present
		assert.NotNil(t, response.Queries[0].CellATraceID)
		assert.NotNil(t, response.Queries[0].CellBTraceID)
	})

	t.Run("partial config with only TracesDatasourceUID", func(t *testing.T) {
		service := createTestServiceWithConfig(storage, GoldfishConfig{
			Enable:              true,
			TracesDatasourceUID: "tempo-123",
			// Missing GrafanaURL
		})

		response, err := service.GetSampledQueries(1, 10, goldfish.QueryFilter{})
		require.NoError(t, err)

		// Should not include trace links with partial config
		assert.Nil(t, response.Queries[0].CellATraceLink)
		assert.Nil(t, response.Queries[0].CellBTraceLink)
		// But trace IDs should still be present
		assert.NotNil(t, response.Queries[0].CellATraceID)
		assert.NotNil(t, response.Queries[0].CellBTraceID)
	})
}

func TestGoldfishQueriesHandler_FiltersByTenant(t *testing.T) {
	// Since simplified mockStorage doesn't filter, only include queries from tenant-b
	storage := &mockStorage{
		queries: []goldfish.QuerySample{
			createTestQuerySample("2", "tenant-b", 200, 200, "hash1", "hash1", goldfish.ComparisonStatusMatch),
			createTestQuerySample("4", "tenant-b", 200, 200, "hash4", "hash4", goldfish.ComparisonStatusMatch),
		},
	}

	service := createTestService(storage)

	handler := service.goldfishQueriesHandler()

	// Test filtering by tenant-b
	req := httptest.NewRequest("GET", "/api/v1/goldfish/queries?tenant=tenant-b", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response GoldfishAPIResponse
	err := json.Unmarshal(rr.Body.Bytes(), &response)
	require.NoError(t, err)

	// Should only return queries from tenant-b
	assert.Len(t, response.Queries, 2)
	for _, query := range response.Queries {
		assert.Equal(t, "tenant-b", query.TenantID, "Expected only tenant-b queries")
	}
	assert.False(t, response.HasMore) // Only 2 items total, no more pages
}

func TestGoldfishQueriesHandler_FiltersByUser(t *testing.T) {
	// Since simplified mockStorage doesn't filter, only include alice's queries
	queries := []goldfish.QuerySample{
		createTestQuerySample("1", "tenant-a", 200, 200, "hash1", "hash1", goldfish.ComparisonStatusMatch),
		createTestQuerySample("3", "tenant-b", 200, 200, "hash3", "hash3", goldfish.ComparisonStatusMatch),
		createTestQuerySample("5", "tenant-c", 200, 200, "hash5", "hash5", goldfish.ComparisonStatusMatch),
	}
	// Set user to alice for all queries
	for i := range queries {
		queries[i].User = "alice"
	}

	storage := &mockStorage{
		queries: queries,
	}

	service := createTestService(storage)

	handler := service.goldfishQueriesHandler()

	// Test filtering by user alice
	req := httptest.NewRequest("GET", "/api/v1/goldfish/queries?user=alice", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response GoldfishAPIResponse
	err := json.Unmarshal(rr.Body.Bytes(), &response)
	require.NoError(t, err)

	// Should only return queries from alice
	assert.Len(t, response.Queries, 3)
	for _, query := range response.Queries {
		assert.Equal(t, "alice", query.User, "Expected only alice's queries")
	}
	assert.False(t, response.HasMore) // Only 3 items total, no more pages
}

func TestGetSampledQueries_AppliesTimeDefaults(t *testing.T) {
	// Set up a fixed "now" time for predictable testing
	now := time.Date(2024, 1, 15, 14, 30, 0, 0, time.UTC)
	oneHourAgo := now.Add(-time.Hour)

	tests := []struct {
		name         string
		inputFilter  goldfish.QueryFilter
		expectedFrom time.Time
		expectedTo   time.Time
		expectErr    bool
	}{
		{
			name: "no time range specified should get defaults",
			inputFilter: goldfish.QueryFilter{
				Tenant: "test-tenant",
			},
			expectedFrom: oneHourAgo,
			expectedTo:   now,
			expectErr:    false,
		},
		{
			name: "explicit From and To should be preserved",
			inputFilter: goldfish.QueryFilter{
				From:   time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
				To:     time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
				Tenant: "test-tenant",
			},
			expectedFrom: time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
			expectedTo:   time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
			expectErr:    false,
		},
		{
			name: "only From specified should error",
			inputFilter: goldfish.QueryFilter{
				From: time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
			},
			expectedFrom: time.Time{},
			expectedTo:   time.Time{},
			expectErr:    true,
		},
		{
			name: "only To specified should error",
			inputFilter: goldfish.QueryFilter{
				To: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
			},
			expectedFrom: time.Time{},
			expectedTo:   time.Time{},
			expectErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock storage
			storage := &mockStorage{
				queries: []goldfish.QuerySample{},
			}

			// Create service with mock storage
			service := createTestServiceWithNow(storage, func() time.Time {
				return now
			})

			// Call GetSampledQueries
			_, err := service.GetSampledQueries(1, 20, tt.inputFilter)

			if tt.expectErr {
				assert.Error(t, err, "Should return error when only one time bound is specified")
				return
			}

			require.NoError(t, err)

			capturedFilter := storage.capturedFilter
			assert.Equal(t, tt.expectedFrom, capturedFilter.From,
				"From time should match expected")
			assert.Equal(t, tt.expectedTo, capturedFilter.To,
				"To time should match expected")
		})
	}
}

func TestGoldfishQueriesHandler_ParsesTimeParameters(t *testing.T) {
	t.Run("parses valid from and to parameters", func(t *testing.T) {
		storage := &mockStorage{
			queries: []goldfish.QuerySample{
				createTestQuerySample("1", "tenant-a", 200, 200, "hash1", "hash1", goldfish.ComparisonStatusMatch),
			},
		}

		service := createTestService(storage)
		handler := service.goldfishQueriesHandler()

		// Request with 'from' parameter in RFC3339 format (also need 'to' parameter due to validation)
		req := httptest.NewRequest("GET", "/api/v1/goldfish/queries?from=2024-01-01T10:00:00Z&to=2024-01-01T11:00:00Z", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		// Verify the storage received the parsed times
		expectedFromTime, _ := time.Parse(time.RFC3339, "2024-01-01T10:00:00Z")
		expectedToTime, _ := time.Parse(time.RFC3339, "2024-01-01T11:00:00Z")
		assert.Equal(t, expectedFromTime, storage.capturedFilter.From, "Expected 'from' time to be parsed and passed to storage")
		assert.Equal(t, expectedToTime, storage.capturedFilter.To, "Expected 'to' time to be parsed and passed to storage")
	})

	t.Run("returns 400 for invalid from parameter", func(t *testing.T) {
		storage := &mockStorage{
			queries: []goldfish.QuerySample{},
		}

		service := createTestService(storage)
		handler := service.goldfishQueriesHandler()

		// Request with invalid 'from' parameter
		req := httptest.NewRequest("GET", "/api/v1/goldfish/queries?from=invalid-date", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusBadRequest, rr.Code)

		var errorResp map[string]string
		err := json.Unmarshal(rr.Body.Bytes(), &errorResp)
		require.NoError(t, err)
		assert.Contains(t, errorResp["error"], "from", "Error message should mention the 'from' parameter")
	})

	t.Run("returns 400 for invalid to parameter", func(t *testing.T) {
		storage := &mockStorage{
			queries: []goldfish.QuerySample{},
		}

		service := createTestService(storage)
		handler := service.goldfishQueriesHandler()

		// Request with valid 'from' but invalid 'to' parameter
		req := httptest.NewRequest("GET", "/api/v1/goldfish/queries?from=2024-01-01T10:00:00Z&to=not-a-date", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusBadRequest, rr.Code)

		var errorResp map[string]string
		err := json.Unmarshal(rr.Body.Bytes(), &errorResp)
		require.NoError(t, err)
		assert.Contains(t, errorResp["error"], "to", "Error message should mention the 'to' parameter")
	})
}

func TestGoldfishQueriesHandler_FiltersByNewEngine(t *testing.T) {
	// Test queries with new engine usage
	queriesWithEngine := []goldfish.QuerySample{
		createTestQuerySample("1", "tenant-a", 200, 200, "hash1", "hash1", goldfish.ComparisonStatusMatch),
		createTestQuerySample("3", "tenant-b", 200, 200, "hash3", "hash3", goldfish.ComparisonStatusMatch),
		createTestQuerySample("4", "tenant-b", 200, 200, "hash4", "hash4", goldfish.ComparisonStatusMatch),
	}
	queriesWithEngine[0].CellAUsedNewEngine = true // query 1 used new engine in cell A
	queriesWithEngine[0].CellBUsedNewEngine = false
	queriesWithEngine[1].CellAUsedNewEngine = false // query 3 used new engine in cell B
	queriesWithEngine[1].CellBUsedNewEngine = true
	queriesWithEngine[2].CellAUsedNewEngine = true // query 4 used new engine in both cells
	queriesWithEngine[2].CellBUsedNewEngine = true

	// Test queries without new engine
	queriesWithoutEngine := []goldfish.QuerySample{
		createTestQuerySample("2", "tenant-a", 200, 200, "hash1", "hash1", goldfish.ComparisonStatusMatch),
		createTestQuerySample("5", "tenant-c", 200, 200, "hash5", "hash5", goldfish.ComparisonStatusMatch),
	}
	queriesWithoutEngine[0].CellAUsedNewEngine = false
	queriesWithoutEngine[0].CellBUsedNewEngine = false
	queriesWithoutEngine[1].CellAUsedNewEngine = false
	queriesWithoutEngine[1].CellBUsedNewEngine = false

	t.Run("filter new engine true", func(t *testing.T) {
		storage := &mockStorage{
			queries: queriesWithEngine,
		}

		service := createTestService(storage)

		handler := service.goldfishQueriesHandler()

		// Test filtering by newEngine=true (queries that used new engine in at least one cell)
		req := httptest.NewRequest("GET", "/api/v1/goldfish/queries?newEngine=true", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var response GoldfishAPIResponse
		err := json.Unmarshal(rr.Body.Bytes(), &response)
		require.NoError(t, err)

		// Should return queries 1, 3, and 4 (used new engine in at least one cell)
		assert.Len(t, response.Queries, 3)
		assert.False(t, response.HasMore) // Only 3 items total, no more pages

		// Verify all returned queries used new engine in at least one cell
		for _, query := range response.Queries {
			assert.True(t, query.CellAUsedNewEngine || query.CellBUsedNewEngine,
				"Expected queries that used new engine in at least one cell")
		}
	})

	t.Run("filter new engine false", func(t *testing.T) {
		storage := &mockStorage{
			queries: queriesWithoutEngine,
		}

		service := createTestService(storage)

		handler := service.goldfishQueriesHandler()

		// Test filtering by newEngine=false (queries that didn't use new engine in any cell)
		req := httptest.NewRequest("GET", "/api/v1/goldfish/queries?newEngine=false", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var response GoldfishAPIResponse
		err := json.Unmarshal(rr.Body.Bytes(), &response)
		require.NoError(t, err)

		// Should return queries 2 and 5 (didn't use new engine in any cell)
		assert.Len(t, response.Queries, 2)
		assert.False(t, response.HasMore) // Only 2 items total, no more pages

		// Verify all returned queries didn't use new engine in any cell
		for _, query := range response.Queries {
			assert.False(t, query.CellAUsedNewEngine,
				"Expected queries that didn't use new engine in cell A")
			assert.False(t, query.CellBUsedNewEngine,
				"Expected queries that didn't use new engine in cell B")
		}
	})
}

func TestGoldfishStatsHandler(t *testing.T) {
	from := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	to := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name           string
		config         GoldfishConfig
		mockStats      *goldfish.Statistics
		mockError      error
		url            string
		expectedStatus int
		checkResponse  func(t *testing.T, rr *httptest.ResponseRecorder, storage *mockStorage)
	}{
		{
			name:   "happy path",
			config: GoldfishConfig{Enable: true},
			mockStats: &goldfish.Statistics{
				QueriesExecuted:       250,
				EngineCoverage:        0.85,
				MatchingQueries:       0.98,
				PerformanceDifference: -0.20,
			},
			url:            "/ui/api/v1/goldfish/stats",
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, rr *httptest.ResponseRecorder, _ *mockStorage) {
				var stats goldfish.Statistics
				err := json.Unmarshal(rr.Body.Bytes(), &stats)
				require.NoError(t, err)

				assert.Equal(t, int64(250), stats.QueriesExecuted)
				assert.Equal(t, 0.85, stats.EngineCoverage)
				assert.Equal(t, 0.98, stats.MatchingQueries)
				assert.Equal(t, -0.20, stats.PerformanceDifference)
			},
		},
		{
			name:           "with time filters",
			config:         GoldfishConfig{Enable: true},
			url:            "/ui/api/v1/goldfish/stats?from=" + url.QueryEscape(from.Format(time.RFC3339)) + "&to=" + url.QueryEscape(to.Format(time.RFC3339)),
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, _ *httptest.ResponseRecorder, storage *mockStorage) {
				// Verify the filter was correctly captured
				assert.Equal(t, from, storage.capturedStatsFilter.From)
				assert.Equal(t, to, storage.capturedStatsFilter.To)
				assert.True(t, storage.capturedStatsFilter.UsesRecentData)
			},
		},
		{
			name:           "with usesRecentData=false",
			config:         GoldfishConfig{Enable: true},
			url:            "/ui/api/v1/goldfish/stats?usesRecentData=false",
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, _ *httptest.ResponseRecorder, storage *mockStorage) {
				// Verify the filter was correctly set
				assert.False(t, storage.capturedStatsFilter.UsesRecentData)
			},
		},
		{
			name:           "invalid time format",
			config:         GoldfishConfig{Enable: true},
			url:            "/ui/api/v1/goldfish/stats?from=invalid-time",
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, rr *httptest.ResponseRecorder, _ *mockStorage) {
				var response map[string]string
				err := json.Unmarshal(rr.Body.Bytes(), &response)
				require.NoError(t, err)
				assert.Contains(t, response["error"], "from")
			},
		},
		{
			name:           "goldfish disabled",
			config:         GoldfishConfig{Enable: false},
			url:            "/ui/api/v1/goldfish/stats",
			expectedStatus: http.StatusNotFound,
			checkResponse:  func(_ *testing.T, _ *httptest.ResponseRecorder, _ *mockStorage) {},
		},
		{
			name:           "storage error",
			config:         GoldfishConfig{Enable: true},
			mockError:      errors.New("database connection failed"),
			url:            "/ui/api/v1/goldfish/stats",
			expectedStatus: http.StatusInternalServerError,
			checkResponse: func(t *testing.T, rr *httptest.ResponseRecorder, _ *mockStorage) {
				var response map[string]string
				err := json.Unmarshal(rr.Body.Bytes(), &response)
				require.NoError(t, err)
				assert.Contains(t, response["error"], "failed to retrieve statistics")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storage := &mockStorage{
				stats: tt.mockStats,
				error: tt.mockError,
			}

			service := createTestServiceWithConfig(storage, tt.config)
			handler := service.goldfishStatsHandler()

			req := httptest.NewRequest("GET", tt.url, nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			assert.Equal(t, tt.expectedStatus, rr.Code)

			if tt.checkResponse != nil {
				tt.checkResponse(t, rr, storage)
			}
		})
	}
}
