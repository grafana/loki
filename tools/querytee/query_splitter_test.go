package querytee

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	jsoniter "github.com/json-iterator/go"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShouldSplitQuery(t *testing.T) {
	now := time.Now()
	minAge := 3 * time.Hour
	threshold := now.Add(-minAge)

	tests := []struct {
		name        string
		path        string
		start       time.Time
		end         time.Time
		step        string
		minAge      time.Duration
		needsSplit  bool
		expectError bool
	}{
		{
			name:       "not a query_range request",
			path:       "/loki/api/v1/query",
			start:      threshold.Add(-time.Hour),
			end:        threshold.Add(time.Hour),
			step:       "60s",
			minAge:     minAge,
			needsSplit: false,
		},
		{
			name:       "minAge is zero (splitting disabled)",
			path:       "/loki/api/v1/query_range",
			start:      threshold.Add(-time.Hour),
			end:        threshold.Add(time.Hour),
			step:       "60s",
			minAge:     0,
			needsSplit: false,
		},
		{
			name:       "query entirely recent (no split needed)",
			path:       "/loki/api/v1/query_range",
			start:      threshold.Add(time.Hour),
			end:        threshold.Add(2 * time.Hour),
			step:       "60s",
			minAge:     minAge,
			needsSplit: false,
		},
		{
			name:       "query entirely old (no split needed)",
			path:       "/loki/api/v1/query_range",
			start:      threshold.Add(-2 * time.Hour),
			end:        threshold.Add(-time.Hour),
			step:       "60s",
			minAge:     minAge,
			needsSplit: false,
		},
		{
			name:       "query spans threshold (split needed)",
			path:       "/loki/api/v1/query_range",
			start:      threshold.Add(-time.Hour),
			end:        threshold.Add(time.Hour),
			step:       "60s",
			minAge:     minAge,
			needsSplit: true,
		},
		{
			name:       "query starts at threshold plus 1 minute (no split, all recent)",
			path:       "/loki/api/v1/query_range",
			start:      threshold.Add(time.Minute),
			end:        threshold.Add(time.Hour),
			step:       "60s",
			minAge:     minAge,
			needsSplit: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "http://example.com"+tt.path, nil)
			q := req.URL.Query()

			q.Set("start", formatTime(tt.start))
			q.Set("end", formatTime(tt.end))
			q.Set("step", tt.step)
			req.URL.RawQuery = q.Encode()

			needsSplit, splitPoint, step, err := shouldSplitQuery(req, tt.minAge)

			if tt.expectError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.needsSplit, needsSplit, "needsSplit mismatch")

			if tt.needsSplit {
				assert.True(t, splitPoint.After(tt.start) && splitPoint.Before(tt.end), "splitPoint should be between start and end")
				assert.True(t, step > 0, "step should be positive")
			}
		})
	}
}

func TestAlignToStep(t *testing.T) {
	start := time.Unix(0, 0)

	tests := []struct {
		name      string
		splitTime time.Time
		start     time.Time
		step      time.Duration
		expected  time.Time
	}{
		{
			name:      "split aligns exactly to step boundary",
			splitTime: start.Add(5 * time.Minute),
			start:     start,
			step:      time.Minute,
			expected:  start.Add(5 * time.Minute),
		},
		{
			name:      "split between boundaries (rounds down)",
			splitTime: start.Add(5*time.Minute + 30*time.Second),
			start:     start,
			step:      time.Minute,
			expected:  start.Add(5 * time.Minute),
		},
		{
			name:      "split very close to next boundary",
			splitTime: start.Add(5*time.Minute + 59*time.Second),
			start:     start,
			step:      time.Minute,
			expected:  start.Add(5 * time.Minute),
		},
		{
			name:      "larger step size",
			splitTime: start.Add(37 * time.Minute),
			start:     start,
			step:      5 * time.Minute,
			expected:  start.Add(35 * time.Minute), // floor(37/5) * 5 = 35
		},
		{
			name:      "split before start",
			splitTime: start.Add(-time.Minute),
			start:     start,
			step:      time.Minute,
			expected:  start,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := alignToStep(tt.splitTime, tt.start, tt.step)
			assert.Equal(t, tt.expected.Unix(), result.Unix(), "aligned time mismatch")
		})
	}
}

func TestCreateSplitRequests(t *testing.T) {
	start := time.Unix(0, 0)
	end := start.Add(2 * time.Hour)
	splitPoint := start.Add(time.Hour)
	step := time.Minute

	original := httptest.NewRequest("GET", "http://example.com/loki/api/v1/query_range", nil)
	q := original.URL.Query()
	q.Set("query", "rate({job=\"test\"}[5m])")
	q.Set("start", formatTime(start))
	q.Set("end", formatTime(end))
	q.Set("step", "60s")
	original.URL.RawQuery = q.Encode()

	oldQuery, recentQuery, err := createSplitRequests(original, splitPoint, step)
	require.NoError(t, err)

	// Check old query
	oldParams := oldQuery.URL.Query()
	assert.Equal(t, formatTime(start), oldParams.Get("start"))
	assert.Equal(t, formatTime(splitPoint), oldParams.Get("end"))
	assert.Equal(t, "rate({job=\"test\"}[5m])", oldParams.Get("query"))
	assert.Equal(t, "60s", oldParams.Get("step"))

	// Check recent query
	recentParams := recentQuery.URL.Query()
	assert.Equal(t, formatTime(splitPoint.Add(step)), recentParams.Get("start"))
	assert.Equal(t, formatTime(end), recentParams.Get("end"))
	assert.Equal(t, "rate({job=\"test\"}[5m])", recentParams.Get("query"))
	assert.Equal(t, "60s", recentParams.Get("step"))
}

func TestConcatenateMatrixResponses(t *testing.T) {
	t.Run("concatenate two valid matrix responses", func(t *testing.T) {
		// Create old response
		oldMatrix := loghttp.Matrix{
			{
				Metric: model.Metric{"__name__": "test_metric", "job": "test"},
				Values: []model.SamplePair{
					{Timestamp: 1000, Value: 1.0},
					{Timestamp: 2000, Value: 2.0},
				},
			},
		}
		oldResp := loghttp.QueryResponse{
			Status: "success",
			Data: loghttp.QueryResponseData{
				ResultType: loghttp.ResultTypeMatrix,
				Result:     oldMatrix,
				Statistics: stats.Result{},
			},
		}
		oldBytes, err := json.Marshal(oldResp)
		require.NoError(t, err)

		// Create recent response
		recentMatrix := loghttp.Matrix{
			{
				Metric: model.Metric{"__name__": "test_metric", "job": "test"},
				Values: []model.SamplePair{
					{Timestamp: 3000, Value: 3.0},
					{Timestamp: 4000, Value: 4.0},
				},
			},
		}
		recentResp := loghttp.QueryResponse{
			Status: "success",
			Data: loghttp.QueryResponseData{
				ResultType: loghttp.ResultTypeMatrix,
				Result:     recentMatrix,
				Statistics: stats.Result{},
			},
		}
		recentBytes, err := json.Marshal(recentResp)
		require.NoError(t, err)

		// Create mock BackendResponse objects
		oldBackendResp := &BackendResponse{
			backend:  &ProxyBackend{name: "backend-1"},
			status:   200,
			body:     oldBytes,
			err:      nil,
			duration: 100 * time.Millisecond,
		}
		recentBackendResp := &BackendResponse{
			backend:  &ProxyBackend{name: "backend-1"},
			status:   200,
			body:     recentBytes,
			err:      nil,
			duration: 50 * time.Millisecond,
		}

		result, err := concatenateResponses(oldBackendResp, recentBackendResp, logproto.FORWARD)
		require.NoError(t, err)

		// Verify combined duration
		assert.Equal(t, 150*time.Millisecond, result.duration)

		// Parse result
		var resultResp loghttp.QueryResponse
		err = resultResp.UnmarshalJSON(result.body)
		require.NoError(t, err)

		assert.Equal(t, "success", resultResp.Status)
		assert.Equal(t, string(loghttp.ResultTypeMatrix), string(resultResp.Data.ResultType))

		resultMatrix, ok := resultResp.Data.Result.(loghttp.Matrix)
		require.True(t, ok)
		require.Len(t, resultMatrix, 1)

		// Check that all 4 values are present
		assert.Len(t, resultMatrix[0].Values, 4)
		assert.Equal(t, model.SamplePair{Timestamp: 1000, Value: 1.0}, resultMatrix[0].Values[0])
		assert.Equal(t, model.SamplePair{Timestamp: 2000, Value: 2.0}, resultMatrix[0].Values[1])
		assert.Equal(t, model.SamplePair{Timestamp: 3000, Value: 3.0}, resultMatrix[0].Values[2])
		assert.Equal(t, model.SamplePair{Timestamp: 4000, Value: 4.0}, resultMatrix[0].Values[3])
	})

	t.Run("metrics only in old response", func(t *testing.T) {
		oldMatrix := loghttp.Matrix{
			{
				Metric: model.Metric{"__name__": "metric1"},
				Values: []model.SamplePair{{Timestamp: 1000, Value: 1.0}},
			},
		}
		oldResp := loghttp.QueryResponse{
			Status: "success",
			Data: loghttp.QueryResponseData{
				ResultType: loghttp.ResultTypeMatrix,
				Result:     oldMatrix,
			},
		}
		oldBytes, _ := json.Marshal(oldResp)

		recentMatrix := loghttp.Matrix{}
		recentResp := loghttp.QueryResponse{
			Status: "success",
			Data: loghttp.QueryResponseData{
				ResultType: loghttp.ResultTypeMatrix,
				Result:     recentMatrix,
			},
		}
		recentBytes, _ := json.Marshal(recentResp)

		// Create mock BackendResponse objects
		oldBackendResp := &BackendResponse{body: oldBytes, status: 200}
		recentBackendResp := &BackendResponse{body: recentBytes, status: 200}

		result, err := concatenateResponses(oldBackendResp, recentBackendResp, logproto.FORWARD)
		require.NoError(t, err)

		var resultResp loghttp.QueryResponse
		err = json.Unmarshal(result.body, &resultResp)
		require.NoError(t, err)
		resultMatrix := resultResp.Data.Result.(loghttp.Matrix)

		require.Len(t, resultMatrix, 1)
		assert.Equal(t, model.Metric{"__name__": "metric1"}, resultMatrix[0].Metric)
	})

	t.Run("metrics only in recent response", func(t *testing.T) {
		oldMatrix := loghttp.Matrix{}
		oldResp := loghttp.QueryResponse{
			Status: "success",
			Data: loghttp.QueryResponseData{
				ResultType: loghttp.ResultTypeMatrix,
				Result:     oldMatrix,
			},
		}
		oldBytes, _ := json.Marshal(oldResp)

		recentMatrix := loghttp.Matrix{
			{
				Metric: model.Metric{"__name__": "metric2"},
				Values: []model.SamplePair{{Timestamp: 2000, Value: 2.0}},
			},
		}
		recentResp := loghttp.QueryResponse{
			Status: "success",
			Data: loghttp.QueryResponseData{
				ResultType: loghttp.ResultTypeMatrix,
				Result:     recentMatrix,
			},
		}
		recentBytes, _ := json.Marshal(recentResp)

		// Create mock BackendResponse objects
		oldBackendResp := &BackendResponse{body: oldBytes, status: 200}
		recentBackendResp := &BackendResponse{body: recentBytes, status: 200}

		result, err := concatenateResponses(oldBackendResp, recentBackendResp, logproto.FORWARD)
		require.NoError(t, err)

		var resultResp loghttp.QueryResponse
		err = json.Unmarshal(result.body, &resultResp)
		require.NoError(t, err)
		resultMatrix := resultResp.Data.Result.(loghttp.Matrix)

		require.Len(t, resultMatrix, 1)
		assert.Equal(t, model.Metric{"__name__": "metric2"}, resultMatrix[0].Metric)
	})

	t.Run("merge warnings", func(t *testing.T) {
		oldResp := loghttp.QueryResponse{
			Status: "success",
			Data: loghttp.QueryResponseData{
				ResultType: loghttp.ResultTypeMatrix,
				Result:     loghttp.Matrix{},
			},
			Warnings: []string{"warning1", "warning2"},
		}
		oldBytes, _ := json.Marshal(oldResp)

		recentResp := loghttp.QueryResponse{
			Status: "success",
			Data: loghttp.QueryResponseData{
				ResultType: loghttp.ResultTypeMatrix,
				Result:     loghttp.Matrix{},
			},
			Warnings: []string{"warning2", "warning3"},
		}
		recentBytes, _ := json.Marshal(recentResp)

		// Create mock BackendResponse objects
		oldBackendResp := &BackendResponse{body: oldBytes, status: 200}
		recentBackendResp := &BackendResponse{body: recentBytes, status: 200}

		result, err := concatenateResponses(oldBackendResp, recentBackendResp, logproto.FORWARD)
		require.NoError(t, err)

		var resultResp loghttp.QueryResponse
		err = json.Unmarshal(result.body, &resultResp)
		require.NoError(t, err)

		// Should have deduplicated and sorted warnings
		assert.ElementsMatch(t, []string{"warning1", "warning2", "warning3"}, resultResp.Warnings)
	})

	t.Run("error on non-success status", func(t *testing.T) {
		oldResp := loghttp.QueryResponse{
			Status: "error",
			Data: loghttp.QueryResponseData{
				ResultType: loghttp.ResultTypeMatrix,
				Result:     loghttp.Matrix{},
			},
		}
		oldBytes, _ := json.Marshal(oldResp)

		recentResp := loghttp.QueryResponse{
			Status: "success",
			Data: loghttp.QueryResponseData{
				ResultType: loghttp.ResultTypeMatrix,
				Result:     loghttp.Matrix{},
			},
		}
		recentBytes, _ := json.Marshal(recentResp)

		// Create mock BackendResponse objects
		oldBackendResp := &BackendResponse{body: oldBytes, status: 200}
		recentBackendResp := &BackendResponse{body: recentBytes, status: 200}

		_, err := concatenateResponses(oldBackendResp, recentBackendResp, logproto.FORWARD)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "non-success status")
	})
}

func TestConcatenateStreamResponses(t *testing.T) {
	t.Run("concatenate streams with same labels - FORWARD direction", func(t *testing.T) {
		oldResp := loghttp.QueryResponse{
			Status: "success",
			Data: loghttp.QueryResponseData{
				ResultType: loghttp.ResultTypeStream,
				Result: loghttp.Streams{
					{
						Labels: map[string]string{"job": "test", "instance": "1"},
						Entries: []loghttp.Entry{
							{Timestamp: time.Unix(0, 1000), Line: "log line 1"},
							{Timestamp: time.Unix(0, 2000), Line: "log line 2"},
						},
					},
				},
				Statistics: stats.Result{},
			},
		}
		oldBytes, err := jsoniter.Marshal(oldResp)
		require.NoError(t, err)

		recentResp := loghttp.QueryResponse{
			Status: "success",
			Data: loghttp.QueryResponseData{
				ResultType: loghttp.ResultTypeStream,
				Result: loghttp.Streams{
					{
						Labels: map[string]string{"job": "test", "instance": "1"},
						Entries: []loghttp.Entry{
							{Timestamp: time.Unix(0, 3000), Line: "log line 3"},
							{Timestamp: time.Unix(0, 4000), Line: "log line 4"},
						},
					},
				},
				Statistics: stats.Result{},
			},
		}
		recentBytes, err := jsoniter.Marshal(recentResp)
		require.NoError(t, err)

		// mock BackendResponse objects
		oldBackendResp := &BackendResponse{
			backend:  &ProxyBackend{name: "backend-1"},
			status:   200,
			body:     oldBytes,
			err:      nil,
			duration: 100 * time.Millisecond,
		}
		recentBackendResp := &BackendResponse{
			backend:  &ProxyBackend{name: "backend-1"},
			status:   200,
			body:     recentBytes,
			err:      nil,
			duration: 50 * time.Millisecond,
		}

		result, err := concatenateResponses(oldBackendResp, recentBackendResp, logproto.FORWARD)
		require.NoError(t, err)
		assert.Equal(t, 150*time.Millisecond, result.duration)

		var resultResp loghttp.QueryResponse
		err = resultResp.UnmarshalJSON(result.body)
		require.NoError(t, err)

		assert.Equal(t, "success", resultResp.Status)
		assert.Equal(t, string(loghttp.ResultTypeStream), string(resultResp.Data.ResultType))

		resultStreams, ok := resultResp.Data.Result.(loghttp.Streams)
		require.True(t, ok)
		require.Len(t, resultStreams, 1)

		// Check that all 4 entries are present in FORWARD order (old first, then recent)
		assert.Len(t, resultStreams[0].Entries, 4)
		assert.Equal(t, "log line 1", resultStreams[0].Entries[0].Line)
		assert.Equal(t, int64(1000), resultStreams[0].Entries[0].Timestamp.UnixNano())
		assert.Equal(t, "log line 2", resultStreams[0].Entries[1].Line)
		assert.Equal(t, int64(2000), resultStreams[0].Entries[1].Timestamp.UnixNano())
		assert.Equal(t, "log line 3", resultStreams[0].Entries[2].Line)
		assert.Equal(t, int64(3000), resultStreams[0].Entries[2].Timestamp.UnixNano())
		assert.Equal(t, "log line 4", resultStreams[0].Entries[3].Line)
		assert.Equal(t, int64(4000), resultStreams[0].Entries[3].Timestamp.UnixNano())
	})

	t.Run("concatenate streams with same labels - BACKWARD direction", func(t *testing.T) {
		// Create old response with entries in BACKWARD order
		oldResp := loghttp.QueryResponse{
			Status: "success",
			Data: loghttp.QueryResponseData{
				ResultType: loghttp.ResultTypeStream,
				Result: loghttp.Streams{
					{
						Labels: map[string]string{"job": "test", "instance": "1"},
						Entries: []loghttp.Entry{
							{Timestamp: time.Unix(0, 2000), Line: "log line 2"},
							{Timestamp: time.Unix(0, 1000), Line: "log line 1"},
						},
					},
				},
				Statistics: stats.Result{},
			},
		}
		oldBytes, err := jsoniter.Marshal(oldResp)
		require.NoError(t, err)

		// Create recent response with entries in BACKWARD order
		recentResp := loghttp.QueryResponse{
			Status: "success",
			Data: loghttp.QueryResponseData{
				ResultType: loghttp.ResultTypeStream,
				Result: loghttp.Streams{
					{
						Labels: map[string]string{"job": "test", "instance": "1"},
						Entries: []loghttp.Entry{
							{Timestamp: time.Unix(0, 4000), Line: "log line 4"},
							{Timestamp: time.Unix(0, 3000), Line: "log line 3"},
						},
					},
				},
				Statistics: stats.Result{},
			},
		}
		recentBytes, err := jsoniter.Marshal(recentResp)
		require.NoError(t, err)

		// mock BackendResponse objects
		oldBackendResp := &BackendResponse{body: oldBytes, status: 200}
		recentBackendResp := &BackendResponse{body: recentBytes, status: 200}

		result, err := concatenateResponses(oldBackendResp, recentBackendResp, logproto.BACKWARD)
		require.NoError(t, err)

		var resultResp loghttp.QueryResponse
		err = resultResp.UnmarshalJSON(result.body)
		require.NoError(t, err)

		resultStreams := resultResp.Data.Result.(loghttp.Streams)
		require.Len(t, resultStreams, 1)

		// Check that all 4 entries are present in BACKWARD order (recent first, then old)
		assert.Len(t, resultStreams[0].Entries, 4)
		assert.Equal(t, "log line 4", resultStreams[0].Entries[0].Line)
		assert.Equal(t, "log line 3", resultStreams[0].Entries[1].Line)
		assert.Equal(t, "log line 2", resultStreams[0].Entries[2].Line)
		assert.Equal(t, "log line 1", resultStreams[0].Entries[3].Line)
	})

	t.Run("multiple streams with different labels", func(t *testing.T) {
		// Create old response with multiple streams
		oldResp := loghttp.QueryResponse{
			Status: "success",
			Data: loghttp.QueryResponseData{
				ResultType: loghttp.ResultTypeStream,
				Result: loghttp.Streams{
					{
						Labels:  map[string]string{"job": "test", "instance": "1"},
						Entries: []loghttp.Entry{{Timestamp: time.Unix(0, 1000), Line: "stream1 old"}},
					},
					{
						Labels:  map[string]string{"job": "test", "instance": "2"},
						Entries: []loghttp.Entry{{Timestamp: time.Unix(0, 1000), Line: "stream2 old"}},
					},
				},
			},
		}
		oldBytes, _ := jsoniter.Marshal(oldResp)

		// Create recent response with multiple streams
		recentResp := loghttp.QueryResponse{
			Status: "success",
			Data: loghttp.QueryResponseData{
				ResultType: loghttp.ResultTypeStream,
				Result: loghttp.Streams{
					{
						Labels:  map[string]string{"job": "test", "instance": "1"},
						Entries: []loghttp.Entry{{Timestamp: time.Unix(0, 2000), Line: "stream1 recent"}},
					},
					{
						Labels:  map[string]string{"job": "test", "instance": "2"},
						Entries: []loghttp.Entry{{Timestamp: time.Unix(0, 2000), Line: "stream2 recent"}},
					},
				},
			},
		}
		recentBytes, _ := jsoniter.Marshal(recentResp)

		oldBackendResp := &BackendResponse{body: oldBytes, status: 200}
		recentBackendResp := &BackendResponse{body: recentBytes, status: 200}

		result, err := concatenateResponses(oldBackendResp, recentBackendResp, logproto.FORWARD)
		require.NoError(t, err)

		var resultResp loghttp.QueryResponse
		err = json.Unmarshal(result.body, &resultResp)
		require.NoError(t, err)

		resultStreams := resultResp.Data.Result.(loghttp.Streams)
		require.Len(t, resultStreams, 2)

		// Both streams should have 2 entries each
		for _, stream := range resultStreams {
			assert.Len(t, stream.Entries, 2)
		}
	})

	t.Run("streams only in old response", func(t *testing.T) {
		oldResp := loghttp.QueryResponse{
			Status: "success",
			Data: loghttp.QueryResponseData{
				ResultType: loghttp.ResultTypeStream,
				Result: loghttp.Streams{
					{
						Labels:  map[string]string{"job": "test"},
						Entries: []loghttp.Entry{{Timestamp: time.Unix(0, 1000), Line: "old log"}},
					},
				},
			},
		}
		oldBytes, _ := jsoniter.Marshal(oldResp)

		recentResp := loghttp.QueryResponse{
			Status: "success",
			Data: loghttp.QueryResponseData{
				ResultType: loghttp.ResultTypeStream,
				Result:     loghttp.Streams{},
			},
		}
		recentBytes, _ := jsoniter.Marshal(recentResp)

		oldBackendResp := &BackendResponse{body: oldBytes, status: 200}
		recentBackendResp := &BackendResponse{body: recentBytes, status: 200}

		result, err := concatenateResponses(oldBackendResp, recentBackendResp, logproto.FORWARD)
		require.NoError(t, err)

		var resultResp loghttp.QueryResponse
		err = json.Unmarshal(result.body, &resultResp)
		require.NoError(t, err)

		resultStreams := resultResp.Data.Result.(loghttp.Streams)
		require.Len(t, resultStreams, 1)
		assert.Equal(t, "old log", resultStreams[0].Entries[0].Line)
	})

	t.Run("streams only in recent response", func(t *testing.T) {
		oldResp := loghttp.QueryResponse{
			Status: "success",
			Data: loghttp.QueryResponseData{
				ResultType: loghttp.ResultTypeStream,
				Result:     loghttp.Streams{},
			},
		}
		oldBytes, _ := jsoniter.Marshal(oldResp)

		recentResp := loghttp.QueryResponse{
			Status: "success",
			Data: loghttp.QueryResponseData{
				ResultType: loghttp.ResultTypeStream,
				Result: loghttp.Streams{
					{
						Labels:  map[string]string{"job": "test"},
						Entries: []loghttp.Entry{{Timestamp: time.Unix(0, 2000), Line: "recent log"}},
					},
				},
			},
		}
		recentBytes, _ := jsoniter.Marshal(recentResp)

		oldBackendResp := &BackendResponse{body: oldBytes, status: 200}
		recentBackendResp := &BackendResponse{body: recentBytes, status: 200}

		result, err := concatenateResponses(oldBackendResp, recentBackendResp, logproto.FORWARD)
		require.NoError(t, err)

		var resultResp loghttp.QueryResponse
		err = json.Unmarshal(result.body, &resultResp)
		require.NoError(t, err)

		resultStreams := resultResp.Data.Result.(loghttp.Streams)
		require.Len(t, resultStreams, 1)
		assert.Equal(t, "recent log", resultStreams[0].Entries[0].Line)
	})

	t.Run("merge warnings with streams", func(t *testing.T) {
		oldResp := loghttp.QueryResponse{
			Status: "success",
			Data: loghttp.QueryResponseData{
				ResultType: loghttp.ResultTypeStream,
				Result:     loghttp.Streams{},
			},
			Warnings: []string{"warning1", "warning2"},
		}
		oldBytes, _ := jsoniter.Marshal(oldResp)

		recentResp := loghttp.QueryResponse{
			Status: "success",
			Data: loghttp.QueryResponseData{
				ResultType: loghttp.ResultTypeStream,
				Result:     loghttp.Streams{},
			},
			Warnings: []string{"warning2", "warning3"},
		}
		recentBytes, _ := jsoniter.Marshal(recentResp)

		oldBackendResp := &BackendResponse{body: oldBytes, status: 200}
		recentBackendResp := &BackendResponse{body: recentBytes, status: 200}

		result, err := concatenateResponses(oldBackendResp, recentBackendResp, logproto.FORWARD)
		require.NoError(t, err)

		var resultResp loghttp.QueryResponse
		err = json.Unmarshal(result.body, &resultResp)
		require.NoError(t, err)

		// Should have deduplicated and sorted warnings
		assert.ElementsMatch(t, []string{"warning1", "warning2", "warning3"}, resultResp.Warnings)
	})

	t.Run("error on mismatched result types", func(t *testing.T) {
		// Old response is matrix
		oldResp := loghttp.QueryResponse{
			Status: "success",
			Data: loghttp.QueryResponseData{
				ResultType: loghttp.ResultTypeMatrix,
				Result:     loghttp.Matrix{},
			},
		}
		oldBytes, _ := jsoniter.Marshal(oldResp)

		// Recent response is streams
		recentResp := loghttp.QueryResponse{
			Status: "success",
			Data: loghttp.QueryResponseData{
				ResultType: loghttp.ResultTypeStream,
				Result:     loghttp.Streams{},
			},
		}
		recentBytes, _ := jsoniter.Marshal(recentResp)

		oldBackendResp := &BackendResponse{body: oldBytes, status: 200}
		recentBackendResp := &BackendResponse{body: recentBytes, status: 200}

		_, err := concatenateResponses(oldBackendResp, recentBackendResp, logproto.FORWARD)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "different result types")
	})
}
