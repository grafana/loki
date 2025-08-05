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

	"github.com/grafana/loki/v3/pkg/goldfish"
)

// mockStorage implements the goldfish.Storage interface for testing
type mockStorage struct {
	queries  []goldfish.QuerySample
	total    int
	err      error
	closed   bool
	outcome  string
	page     int
	pageSize int
}

func (m *mockStorage) StoreQuerySample(_ context.Context, _ *goldfish.QuerySample) error {
	return nil
}

func (m *mockStorage) StoreComparisonResult(_ context.Context, _ *goldfish.ComparisonResult) error {
	return nil
}

func (m *mockStorage) GetSampledQueries(_ context.Context, page, pageSize int, outcome string) (*goldfish.APIResponse, error) {
	if m.err != nil {
		return nil, m.err
	}

	m.page = page
	m.pageSize = pageSize
	m.outcome = outcome

	// Filter queries based on outcome
	filtered := m.queries
	if outcome != goldfish.OutcomeAll && outcome != "" {
		filtered = []goldfish.QuerySample{}
		for _, q := range m.queries {
			status := determineStatus(q)
			if status == outcome {
				filtered = append(filtered, q)
			}
		}
	}

	// Apply pagination
	start := (page - 1) * pageSize
	end := start + pageSize
	if start > len(filtered) {
		start = len(filtered)
	}
	if end > len(filtered) {
		end = len(filtered)
	}

	return &goldfish.APIResponse{
		Queries:  filtered[start:end],
		Total:    len(filtered),
		Page:     page,
		PageSize: pageSize,
	}, nil
}

func (m *mockStorage) Close() error {
	m.closed = true
	return nil
}

func determineStatus(q goldfish.QuerySample) string {
	if q.CellAStatusCode < 200 || q.CellAStatusCode >= 300 || q.CellBStatusCode < 200 || q.CellBStatusCode >= 300 {
		return goldfish.OutcomeError
	}
	if q.CellAResponseHash == q.CellBResponseHash {
		return goldfish.OutcomeMatch
	}
	return goldfish.OutcomeMismatch
}

func createTestQuerySample(id, tenant string, statusA, statusB int, hashA, hashB string) goldfish.QuerySample {
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
	}
}

func TestGoldfishQueriesHandler_AcceptsOutcomeParameter(t *testing.T) {
	storage := &mockStorage{
		queries: []goldfish.QuerySample{
			createTestQuerySample("1", "tenant1", 200, 200, "hash1", "hash1"), // match
			createTestQuerySample("2", "tenant1", 200, 200, "hash1", "hash2"), // mismatch
		},
		total: 2,
	}

	service := &Service{
		cfg: Config{
			Goldfish: GoldfishConfig{
				Enable: true,
			},
		},
		logger:          log.NewNopLogger(),
		goldfishStorage: storage,
	}

	handler := service.goldfishQueriesHandler()

	// Test that the handler accepts and processes the outcome parameter
	req := httptest.NewRequest("GET", "/api/v1/goldfish/queries?outcome=all", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "all", storage.outcome)

	var response GoldfishAPIResponse
	err := json.Unmarshal(rr.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Len(t, response.Queries, 2)
}

func TestGoldfishQueriesHandler_DefaultsToAllOutcome(t *testing.T) {
	storage := &mockStorage{
		queries: []goldfish.QuerySample{
			createTestQuerySample("1", "tenant1", 200, 200, "hash1", "hash1"),
		},
		total: 1,
	}

	service := &Service{
		cfg: Config{
			Goldfish: GoldfishConfig{
				Enable: true,
			},
		},
		logger:          log.NewNopLogger(),
		goldfishStorage: storage,
	}

	handler := service.goldfishQueriesHandler()

	// Test without outcome parameter - should default to "all"
	req := httptest.NewRequest("GET", "/api/v1/goldfish/queries", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, goldfish.OutcomeAll, storage.outcome)
}

func TestGoldfishQueriesHandler_FiltersMatchOutcome(t *testing.T) {
	storage := &mockStorage{
		queries: []goldfish.QuerySample{
			createTestQuerySample("1", "tenant1", 200, 200, "hash1", "hash1"), // match
			createTestQuerySample("2", "tenant1", 200, 200, "hash1", "hash2"), // mismatch
			createTestQuerySample("3", "tenant1", 200, 500, "hash1", "hash1"), // error
		},
	}

	service := &Service{
		cfg: Config{
			Goldfish: GoldfishConfig{
				Enable: true,
			},
		},
		logger:          log.NewNopLogger(),
		goldfishStorage: storage,
	}

	handler := service.goldfishQueriesHandler()

	req := httptest.NewRequest("GET", "/api/v1/goldfish/queries?outcome=match", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response GoldfishAPIResponse
	err := json.Unmarshal(rr.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Len(t, response.Queries, 1)
	assert.Equal(t, "match", response.Queries[0].ComparisonStatus)
}

func TestGoldfishQueriesHandler_FiltersErrorOutcome(t *testing.T) {
	storage := &mockStorage{
		queries: []goldfish.QuerySample{
			createTestQuerySample("1", "tenant1", 200, 200, "hash1", "hash1"), // match
			createTestQuerySample("2", "tenant1", 200, 200, "hash1", "hash2"), // mismatch
			createTestQuerySample("3", "tenant1", 200, 500, "hash1", "hash1"), // error (B failed)
			createTestQuerySample("4", "tenant1", 404, 200, "hash1", "hash1"), // error (A failed)
		},
	}

	service := &Service{
		cfg: Config{
			Goldfish: GoldfishConfig{
				Enable: true,
			},
		},
		logger:          log.NewNopLogger(),
		goldfishStorage: storage,
	}

	handler := service.goldfishQueriesHandler()

	req := httptest.NewRequest("GET", "/api/v1/goldfish/queries?outcome=error", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response GoldfishAPIResponse
	err := json.Unmarshal(rr.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Len(t, response.Queries, 2)
	for _, q := range response.Queries {
		assert.Equal(t, "error", q.ComparisonStatus)
	}
}

func TestGoldfishQueriesHandler_PaginationAndInputValidation(t *testing.T) {
	// Create 25 test queries
	queries := make([]goldfish.QuerySample, 25)
	for i := 0; i < 25; i++ {
		queries[i] = createTestQuerySample(string(rune('a'+i)), "tenant1", 200, 200, "hash1", "hash1")
	}

	storage := &mockStorage{
		queries: queries,
	}

	service := &Service{
		cfg: Config{
			Goldfish: GoldfishConfig{
				Enable: true,
			},
		},
		logger:          log.NewNopLogger(),
		goldfishStorage: storage,
	}

	handler := service.goldfishQueriesHandler()

	tests := []struct {
		name             string
		queryParams      string
		expectedPage     int
		expectedPageSize int
		expectedCount    int
		expectedTotal    int
	}{
		{
			name:             "default pagination",
			queryParams:      "",
			expectedPage:     1,
			expectedPageSize: 20,
			expectedCount:    20,
			expectedTotal:    25,
		},
		{
			name:             "page 2 with custom size",
			queryParams:      "?page=2&pageSize=10",
			expectedPage:     2,
			expectedPageSize: 10,
			expectedCount:    10,
			expectedTotal:    25,
		},
		{
			name:             "invalid page defaults to 1",
			queryParams:      "?page=-5",
			expectedPage:     1,
			expectedPageSize: 20,
			expectedCount:    20,
			expectedTotal:    25,
		},
		{
			name:             "excessive pageSize capped at 1000",
			queryParams:      "?pageSize=5000",
			expectedPage:     1,
			expectedPageSize: 1000,
			expectedCount:    25,
			expectedTotal:    25,
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
			assert.Equal(t, tt.expectedTotal, response.Total)
		})
	}
}

func TestGoldfishQueriesHandler_ReturnsTraceIDs(t *testing.T) {
	storage := &mockStorage{
		queries: []goldfish.QuerySample{
			createTestQuerySample("1", "tenant1", 200, 200, "hash1", "hash1"),
		},
	}

	service := &Service{
		cfg: Config{
			Goldfish: GoldfishConfig{
				Enable: true,
			},
		},
		logger:          log.NewNopLogger(),
		goldfishStorage: storage,
	}

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
					err: errors.New("database connection failed"),
				},
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
				createTestQuerySample("1", "tenant1", 200, 200, "hash1", "hash1"),
			},
		}

		service := &Service{
			cfg: Config{
				Goldfish: GoldfishConfig{
					Enable:              true,
					GrafanaURL:          "https://grafana.example.com",
					TracesDatasourceUID: "tempo-123",
				},
			},
			logger:          log.NewNopLogger(),
			goldfishStorage: storage,
		}

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
				createTestQuerySample("1", "tenant1", 200, 200, "hash1", "hash1"),
			},
		}

		service := &Service{
			cfg: Config{
				Goldfish: GoldfishConfig{
					Enable: true,
					// No explore configuration
				},
			},
			logger:          log.NewNopLogger(),
			goldfishStorage: storage,
		}

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
		service := &Service{
			cfg: Config{
				Goldfish: GoldfishConfig{
					GrafanaURL:          "https://grafana.example.com",
					TracesDatasourceUID: "tempo-123",
				},
			},
		}

		traceID := "abc123def456"
		sampledAt := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		exploreURL := service.GenerateTraceExploreURL(traceID, sampledAt)

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
		service := &Service{
			cfg: Config{
				Goldfish: GoldfishConfig{
					// No GrafanaURL or TracesDatasourceUID
				},
			},
		}

		traceID := "abc123def456"
		sampledAt := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		exploreURL := service.GenerateTraceExploreURL(traceID, sampledAt)

		assert.Empty(t, exploreURL)
	})

	t.Run("returns empty string with partial config", func(t *testing.T) {
		// Only GrafanaURL
		service := &Service{
			cfg: Config{
				Goldfish: GoldfishConfig{
					GrafanaURL: "https://grafana.example.com",
					// No TracesDatasourceUID
				},
			},
		}

		sampledAt := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		exploreURL := service.GenerateTraceExploreURL("abc123", sampledAt)
		assert.Empty(t, exploreURL)

		// Only TracesDatasourceUID
		service2 := &Service{
			cfg: Config{
				Goldfish: GoldfishConfig{
					TracesDatasourceUID: "tempo-123",
					// No GrafanaURL
				},
			},
		}

		exploreURL2 := service2.GenerateTraceExploreURL("abc123", sampledAt)
		assert.Empty(t, exploreURL2)
	})
}

func TestGoldfishConfig_LogsExploreSettings(t *testing.T) {
	t.Run("accepts logs configuration", func(t *testing.T) {
		// Create storage with sample data
		storage := &mockStorage{
			queries: []goldfish.QuerySample{
				createTestQuerySample("1", "tenant1", 200, 200, "hash1", "hash1"),
			},
		}

		// Create service with logs explore configuration
		service := &Service{
			cfg: Config{
				Goldfish: GoldfishConfig{
					Enable:              true,
					GrafanaURL:          "https://grafana.example.com",
					TracesDatasourceUID: "tempo-123",
					LogsDatasourceUID:   "loki-456",
					CellANamespace:      "loki-ops-002",
					CellBNamespace:      "loki-ops-003",
				},
			},
			logger:          log.NewNopLogger(),
			goldfishStorage: storage,
		}

		// Get sampled queries
		response, err := service.GetSampledQueries(1, 10, "all")

		// Assert no error and configuration is accepted
		require.NoError(t, err)
		assert.NotNil(t, response)
		// We'll check for logs links once we add the fields
	})
}

func TestGenerateLogsExploreURL(t *testing.T) {
	t.Run("generates correct logs explore URL", func(t *testing.T) {
		service := &Service{
			cfg: Config{
				Goldfish: GoldfishConfig{
					GrafanaURL:        "https://grafana.example.com",
					LogsDatasourceUID: "loki-456",
					CellANamespace:    "loki-ops-002",
					CellBNamespace:    "loki-ops-003",
				},
			},
		}

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
		service := &Service{
			cfg: Config{
				Goldfish: GoldfishConfig{
					// No configuration
				},
			},
		}

		sampledAt := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		exploreURL := service.GenerateLogsExploreURL("abc123", "namespace", sampledAt)
		assert.Empty(t, exploreURL)
	})

	t.Run("returns empty string with partial config", func(t *testing.T) {
		// Only GrafanaURL
		service := &Service{
			cfg: Config{
				Goldfish: GoldfishConfig{
					GrafanaURL: "https://grafana.example.com",
					// No LogsDatasourceUID
				},
			},
		}

		sampledAt := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
		exploreURL := service.GenerateLogsExploreURL("abc123", "namespace", sampledAt)
		assert.Empty(t, exploreURL)

		// Only LogsDatasourceUID
		service2 := &Service{
			cfg: Config{
				Goldfish: GoldfishConfig{
					LogsDatasourceUID: "loki-456",
					// No GrafanaURL
				},
			},
		}

		exploreURL2 := service2.GenerateLogsExploreURL("abc123", "namespace", sampledAt)
		assert.Empty(t, exploreURL2)
	})
}

func TestGoldfishQueriesHandler_LogsLinks(t *testing.T) {
	t.Run("includes logs links with complete logs config", func(t *testing.T) {
		storage := &mockStorage{
			queries: []goldfish.QuerySample{
				createTestQuerySample("1", "tenant1", 200, 200, "hash1", "hash1"),
			},
		}

		service := &Service{
			cfg: Config{
				Goldfish: GoldfishConfig{
					Enable:            true,
					GrafanaURL:        "https://grafana.example.com",
					LogsDatasourceUID: "loki-456",
					CellANamespace:    "loki-ops-002",
					CellBNamespace:    "loki-ops-003",
				},
			},
			logger:          log.NewNopLogger(),
			goldfishStorage: storage,
		}

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
				createTestQuerySample("1", "tenant1", 200, 200, "hash1", "hash1"),
			},
		}

		service := &Service{
			cfg: Config{
				Goldfish: GoldfishConfig{
					Enable: true,
					// No logs configuration
				},
			},
			logger:          log.NewNopLogger(),
			goldfishStorage: storage,
		}

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
				createTestQuerySample("1", "tenant1", 200, 200, "hash1", "hash1"),
			},
		}

		// Missing namespace configuration
		service := &Service{
			cfg: Config{
				Goldfish: GoldfishConfig{
					Enable:            true,
					GrafanaURL:        "https://grafana.example.com",
					LogsDatasourceUID: "loki-456",
					// Missing CellANamespace and CellBNamespace
				},
			},
			logger:          log.NewNopLogger(),
			goldfishStorage: storage,
		}

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
			createTestQuerySample("1", "tenant1", 200, 200, "hash1", "hash1"),
		},
	}

	t.Run("partial config with only GrafanaURL", func(t *testing.T) {
		service := &Service{
			cfg: Config{
				Goldfish: GoldfishConfig{
					Enable:     true,
					GrafanaURL: "https://grafana.example.com",
					// Missing TracesDatasourceUID
				},
			},
			logger:          log.NewNopLogger(),
			goldfishStorage: storage,
		}

		response, err := service.GetSampledQueries(1, 10, "all")
		require.NoError(t, err)

		// Should not include trace links with partial config
		assert.Nil(t, response.Queries[0].CellATraceLink)
		assert.Nil(t, response.Queries[0].CellBTraceLink)
		// But trace IDs should still be present
		assert.NotNil(t, response.Queries[0].CellATraceID)
		assert.NotNil(t, response.Queries[0].CellBTraceID)
	})

	t.Run("partial config with only TracesDatasourceUID", func(t *testing.T) {
		service := &Service{
			cfg: Config{
				Goldfish: GoldfishConfig{
					Enable:              true,
					TracesDatasourceUID: "tempo-123",
					// Missing GrafanaURL
				},
			},
			logger:          log.NewNopLogger(),
			goldfishStorage: storage,
		}

		response, err := service.GetSampledQueries(1, 10, "all")
		require.NoError(t, err)

		// Should not include trace links with partial config
		assert.Nil(t, response.Queries[0].CellATraceLink)
		assert.Nil(t, response.Queries[0].CellBTraceLink)
		// But trace IDs should still be present
		assert.NotNil(t, response.Queries[0].CellATraceID)
		assert.NotNil(t, response.Queries[0].CellBTraceID)
	})
}
