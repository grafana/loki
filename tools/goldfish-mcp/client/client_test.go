package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/grafana/loki/v3/pkg/goldfish"
)

func TestHealthCheck(t *testing.T) {
	tests := []struct {
		name          string
		statusCode    int
		responseBody  interface{}
		expectedError bool
		errorContains string
	}{
		{
			name:       "successful health check",
			statusCode: http.StatusOK,
			responseBody: goldfish.Statistics{
				QueriesExecuted: 100,
			},
			expectedError: false,
		},
		{
			name:          "goldfish disabled",
			statusCode:    http.StatusNotFound,
			responseBody:  ErrorResponse{Error: "goldfish feature is disabled"},
			expectedError: true,
			errorContains: "goldfish feature is disabled",
		},
		{
			name:          "server error",
			statusCode:    http.StatusInternalServerError,
			responseBody:  ErrorResponse{Error: "internal error"},
			expectedError: true,
			errorContains: "HTTP 500",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.statusCode)
				_ = json.NewEncoder(w).Encode(tt.responseBody)
			}))
			defer server.Close()

			client := NewClient(server.URL, 10*time.Second)
			err := client.HealthCheck(context.Background())

			if tt.expectedError {
				if err == nil {
					t.Errorf("HealthCheck() expected error, got nil")
				} else if tt.errorContains != "" && !contains(err.Error(), tt.errorContains) {
					t.Errorf("HealthCheck() error = %v, should contain %s", err, tt.errorContains)
				}
			} else {
				if err != nil {
					t.Errorf("HealthCheck() unexpected error: %v", err)
				}
			}
		})
	}
}

func TestGetQueries(t *testing.T) {
	mockResponse := QueriesResponse{
		Queries: []goldfish.QuerySample{
			{
				CorrelationID:    "test-123",
				Query:            "sum(rate(log[5m]))",
				ComparisonStatus: "mismatch",
				CellAStats: goldfish.QueryStats{
					ExecTimeMs: 100,
				},
				CellBStats: goldfish.QueryStats{
					ExecTimeMs: 150,
				},
			},
		},
		Total:       1,
		HasMore:     false,
		CurrentPage: 1,
		PageSize:    50,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify query parameters
		if r.URL.Query().Get("page") != "1" {
			t.Errorf("Expected page=1, got %s", r.URL.Query().Get("page"))
		}
		if r.URL.Query().Get("pageSize") != "50" {
			t.Errorf("Expected pageSize=50, got %s", r.URL.Query().Get("pageSize"))
		}
		if r.URL.Query().Get("comparisonStatus") != "mismatch" {
			t.Errorf("Expected comparisonStatus=mismatch, got %s", r.URL.Query().Get("comparisonStatus"))
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(mockResponse)
	}))
	defer server.Close()

	client := NewClient(server.URL, 10*time.Second)
	params := QueryParams{
		Page:             1,
		PageSize:         50,
		ComparisonStatus: "mismatch",
	}

	result, err := client.GetQueries(context.Background(), params)
	if err != nil {
		t.Fatalf("GetQueries() unexpected error: %v", err)
	}

	if len(result.Queries) != 1 {
		t.Errorf("GetQueries() got %d queries, want 1", len(result.Queries))
	}

	if result.Queries[0].CorrelationID != "test-123" {
		t.Errorf("GetQueries() got correlation ID %s, want test-123", result.Queries[0].CorrelationID)
	}
}

func TestGetQueryByCorrelationID(t *testing.T) {
	mockQuery := goldfish.QuerySample{
		CorrelationID:    "test-456",
		Query:            "sum(rate(log[5m]))",
		ComparisonStatus: "match",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify correlation ID parameter
		if r.URL.Query().Get("correlationId") != "test-456" {
			t.Errorf("Expected correlationId=test-456, got %s", r.URL.Query().Get("correlationId"))
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(QueriesResponse{
			Queries: []goldfish.QuerySample{mockQuery},
			Total:   1,
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, 10*time.Second)
	result, err := client.GetQueryByCorrelationID(context.Background(), "test-456")
	if err != nil {
		t.Fatalf("GetQueryByCorrelationID() unexpected error: %v", err)
	}

	if result.CorrelationID != "test-456" {
		t.Errorf("GetQueryByCorrelationID() got correlation ID %s, want test-456", result.CorrelationID)
	}
}

func TestGetQueryByCorrelationIDNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(QueriesResponse{
			Queries: []goldfish.QuerySample{},
			Total:   0,
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, 10*time.Second)
	_, err := client.GetQueryByCorrelationID(context.Background(), "nonexistent")
	if err == nil {
		t.Error("GetQueryByCorrelationID() expected error for nonexistent query, got nil")
	}
}

func TestGetStatistics(t *testing.T) {
	mockStats := goldfish.Statistics{
		QueriesExecuted:       1000,
		EngineCoverage:        0.85,
		MatchingQueries:       0.92,
		PerformanceDifference: 1.15,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(mockStats)
	}))
	defer server.Close()

	client := NewClient(server.URL, 10*time.Second)
	params := StatsParams{
		UsesRecentData: true,
	}

	result, err := client.GetStatistics(context.Background(), params)
	if err != nil {
		t.Fatalf("GetStatistics() unexpected error: %v", err)
	}

	if result.QueriesExecuted != 1000 {
		t.Errorf("GetStatistics() got QueriesExecuted %d, want 1000", result.QueriesExecuted)
	}

	if result.EngineCoverage != 0.85 {
		t.Errorf("GetStatistics() got EngineCoverage %f, want 0.85", result.EngineCoverage)
	}
}

func TestGetResult(t *testing.T) {
	mockResult := map[string]interface{}{
		"status": "success",
		"data": map[string]interface{}{
			"resultType": "matrix",
			"result":     []interface{}{},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify path includes correlation ID and cell
		expectedPath := "/ui/api/v1/goldfish/results/test-789/cell-a"
		if r.URL.Path != expectedPath {
			t.Errorf("Expected path %s, got %s", expectedPath, r.URL.Path)
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(mockResult)
	}))
	defer server.Close()

	client := NewClient(server.URL, 10*time.Second)
	result, err := client.GetResult(context.Background(), "test-789", "cell-a")
	if err != nil {
		t.Fatalf("GetResult() unexpected error: %v", err)
	}

	if result["status"] != "success" {
		t.Errorf("GetResult() got status %v, want success", result["status"])
	}
}

func TestGetResultNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(ErrorResponse{
			Error: "result not found",
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, 10*time.Second)
	_, err := client.GetResult(context.Background(), "missing", "cell-a")
	if err == nil {
		t.Error("GetResult() expected error for missing result, got nil")
	}

	// Should contain the formatted error message
	if !contains(err.Error(), "not available") {
		t.Errorf("GetResult() error should mention result not available, got: %v", err)
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && searchSubstring(s, substr)))
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
