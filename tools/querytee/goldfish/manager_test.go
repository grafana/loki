package goldfish

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/loki/v3/pkg/goldfish"
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

func (m *mockStorage) StoreQuerySample(_ context.Context, sample *goldfish.QuerySample) error {
	m.samples = append(m.samples, *sample)
	return nil
}

func (m *mockStorage) StoreComparisonResult(_ context.Context, result *goldfish.ComparisonResult) error {
	m.results = append(m.results, *result)
	return nil
}

func (m *mockStorage) GetSampledQueries(_ context.Context, page, pageSize int, _ string) (*goldfish.APIResponse, error) {
	// This is only used for UI, not needed in manager tests
	return &goldfish.APIResponse{
		Queries:  []goldfish.QuerySample{},
		Total:    0,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

func (m *mockStorage) Close() error {
	m.closed = true
	return nil
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
			manager, err := NewManager(tt.config, storage, log.NewNopLogger(), prometheus.NewRegistry())
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
	manager, err := NewManager(config, storage, log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)

	req, _ := http.NewRequest("GET", "/loki/api/v1/query_range?query=count_over_time({job=\"test\"}[5m])&start=1700000000&end=1700001000&step=60s", nil)
	req.Header.Set("X-Scope-OrgID", "tenant1")

	cellAResp := &ResponseData{
		Body:          []byte(`{"status":"success","data":{"resultType":"matrix","result":[],"stats":{"summary":{"execTime":0.1,"queueTime":0.05,"totalBytesProcessed":1000,"totalLinesProcessed":100,"bytesProcessedPerSecond":10000,"linesProcessedPerSecond":1000,"totalEntriesReturned":5,"splits":1,"shards":2}}}}`),
		StatusCode:    200,
		Duration:      100 * time.Millisecond,
		Stats:         goldfish.QueryStats{ExecTimeMs: 100, QueueTimeMs: 50, BytesProcessed: 1000, LinesProcessed: 100},
		Hash:          "hash123",
		Size:          150,
		UsedNewEngine: false,
	}

	cellBResp := &ResponseData{
		Body:          []byte(`{"status":"success","data":{"resultType":"matrix","result":[],"stats":{"summary":{"execTime":0.12,"queueTime":0.06,"totalBytesProcessed":1000,"totalLinesProcessed":100,"bytesProcessedPerSecond":8333,"linesProcessedPerSecond":833,"totalEntriesReturned":5,"splits":1,"shards":2}}}}`),
		StatusCode:    200,
		Duration:      120 * time.Millisecond,
		Stats:         goldfish.QueryStats{ExecTimeMs: 120, QueueTimeMs: 60, BytesProcessed: 1000, LinesProcessed: 100},
		Hash:          "hash123", // Same hash indicates same data
		Size:          155,
		UsedNewEngine: true,
	}

	ctx := context.Background()
	manager.ProcessQueryPair(ctx, req, cellAResp, cellBResp)

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
	assert.Equal(t, "hash123", sample.CellAResponseHash)
	assert.Equal(t, "hash123", sample.CellBResponseHash)
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

			// Call CaptureResponseWithTraceID
			data, err := CaptureResponse(resp, time.Duration(100)*time.Millisecond, tt.traceID)

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
	manager, err := NewManager(config, storage, log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)

	req, _ := http.NewRequest("GET", "/loki/api/v1/query_range?query=count_over_time({job=\"test\"}[5m])&start=1700000000&end=1700001000&step=60s", nil)
	req.Header.Set("X-Scope-OrgID", "tenant1")

	cellAResp := &ResponseData{
		Body:          []byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`),
		StatusCode:    200,
		Duration:      100 * time.Millisecond,
		Stats:         goldfish.QueryStats{ExecTimeMs: 100, QueueTimeMs: 50, BytesProcessed: 1000, LinesProcessed: 100},
		Hash:          "hash123",
		Size:          150,
		UsedNewEngine: false,
		TraceID:       "trace-cell-a-123",
	}

	cellBResp := &ResponseData{
		Body:          []byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`),
		StatusCode:    200,
		Duration:      120 * time.Millisecond,
		Stats:         goldfish.QueryStats{ExecTimeMs: 120, QueueTimeMs: 60, BytesProcessed: 1000, LinesProcessed: 100},
		Hash:          "hash123",
		Size:          155,
		UsedNewEngine: true,
		TraceID:       "trace-cell-b-456",
	}

	ctx := context.Background()
	manager.ProcessQueryPair(ctx, req, cellAResp, cellBResp)

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
	manager, err := NewManager(Config{Enabled: true}, storage, log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)

	err = manager.Close()
	assert.NoError(t, err)
	assert.True(t, storage.closed)
}
