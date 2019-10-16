package loghttp

// import (
// 	"testing"
// 	"time"

// 	"github.com/stretchr/testify/assert"
// )

// func TestHttp_defaultQueryRangeStep(t *testing.T) {
// 	t.Parallel()

// 	tests := map[string]struct {
// 		start    time.Time
// 		end      time.Time
// 		expected int
// 	}{
// 		"should not be lower then 1s": {
// 			start:    time.Unix(60, 0),
// 			end:      time.Unix(60, 0),
// 			expected: 1,
// 		},
// 		"should return 1s if input time range is 5m": {
// 			start:    time.Unix(60, 0),
// 			end:      time.Unix(360, 0),
// 			expected: 1,
// 		},
// 		"should return 14s if input time range is 1h": {
// 			start:    time.Unix(60, 0),
// 			end:      time.Unix(3660, 0),
// 			expected: 14,
// 		},
// 	}

// 	for testName, testData := range tests {
// 		testData := testData

// 		t.Run(testName, func(t *testing.T) {
// 			assert.Equal(t, testData.expected, defaultQueryRangeStep(testData.start, testData.end))
// 		})
// 	}
// }

// func TestHttp_httpRequestToRangeQueryRequest(t *testing.T) {
// 	t.Parallel()

// 	tests := map[string]struct {
// 		reqPath  string
// 		expected *rangeQueryRequest
// 	}{
// 		"should set the default step based on the input time range if the step parameter is not provided": {
// 			reqPath: "/loki/api/v1/query_range?query={}&start=0&end=3600000000000",
// 			expected: &rangeQueryRequest{
// 				query:     "{}",
// 				start:     time.Unix(0, 0),
// 				end:       time.Unix(3600, 0),
// 				step:      14 * time.Second,
// 				limit:     100,
// 				direction: logproto.BACKWARD,
// 			},
// 		},
// 		"should use the input step parameter if provided": {
// 			reqPath: "/loki/api/v1/query_range?query={}&start=0&end=3600000000000&step=5",
// 			expected: &rangeQueryRequest{
// 				query:     "{}",
// 				start:     time.Unix(0, 0),
// 				end:       time.Unix(3600, 0),
// 				step:      5 * time.Second,
// 				limit:     100,
// 				direction: logproto.BACKWARD,
// 			},
// 		},
// 	}

// 	for testName, testData := range tests {
// 		testData := testData

// 		t.Run(testName, func(t *testing.T) {
// 			req := httptest.NewRequest("GET", testData.reqPath, nil)
// 			actual, err := httpRequestToRangeQueryRequest(req)

// 			require.NoError(t, err)
// 			assert.Equal(t, testData.expected, actual)
// 		})
// 	}
// }
