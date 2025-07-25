package delete

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/grafana/loki/v3/pkg/logcli/client"
)

func TestDeleteCreateQuery(t *testing.T) {
	tests := []struct {
		name         string
		query        Query
		expectError  bool
		expectParams client.DeleteRequestParams
		expectOutput string
	}{
		{
			name: "basic create request",
			query: Query{
				QueryString: "{job=\"test\"}",
				Start:       time.Unix(1000, 0),
				End:         time.Unix(2000, 0),
				Quiet:       false,
			},
			expectError: false,
			expectParams: client.DeleteRequestParams{
				Query: "{job=\"test\"}",
				Start: "1000",
				End:   "2000",
			},
			expectOutput: "Delete request created successfully\n",
		},
		{
			name: "create request with max interval",
			query: Query{
				QueryString: "{job=\"test\"}",
				Start:       time.Unix(1000, 0),
				End:         time.Unix(2000, 0),
				MaxInterval: "1h",
				Quiet:       false,
			},
			expectError: false,
			expectParams: client.DeleteRequestParams{
				Query:       "{job=\"test\"}",
				Start:       "1000",
				End:         "2000",
				MaxInterval: "1h",
			},
			expectOutput: "Delete request created successfully\n",
		},
		{
			name: "create request with zero times",
			query: Query{
				QueryString: "{job=\"test\"}",
				Start:       time.Time{},
				End:         time.Time{},
				Quiet:       false,
			},
			expectError: false,
			expectParams: client.DeleteRequestParams{
				Query: "{job=\"test\"}",
			},
			expectOutput: "Delete request created successfully\n",
		},
		{
			name: "create request quiet mode",
			query: Query{
				QueryString: "{job=\"test\"}",
				Start:       time.Unix(1000, 0),
				End:         time.Unix(2000, 0),
				Quiet:       true,
			},
			expectError: false,
			expectParams: client.DeleteRequestParams{
				Query: "{job=\"test\"}",
				Start: "1000",
				End:   "2000",
			},
			expectOutput: "",
		},
		{
			name: "create request with API error",
			query: Query{
				QueryString: "{job=\"test\"}",
				Start:       time.Unix(1000, 0),
				End:         time.Unix(2000, 0),
				Quiet:       false,
			},
			expectError: true,
			expectParams: client.DeleteRequestParams{
				Query: "{job=\"test\"}",
				Start: "1000",
				End:   "2000",
			},
			expectOutput: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Capture stdout
			oldStdout := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w

			// Create mock client
			mockClient := newMockDeleteClient()
			if tt.expectError {
				mockClient.withCreateError(errors.New("API error"))
			}

			// Execute test
			err := tt.query.CreateQuery(mockClient)

			// Restore stdout and capture output
			w.Close()
			os.Stdout = oldStdout
			var buf bytes.Buffer
			_, _ = io.Copy(&buf, r)
			output := buf.String()

			// Verify results
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			assert.Equal(t, tt.expectOutput, output)
			assert.Equal(t, 1, mockClient.createDeleteRequestCalls)
			assert.Equal(t, tt.expectParams, mockClient.lastCreateParams)
			assert.Equal(t, tt.query.Quiet, mockClient.lastCreateQuiet)
		})
	}
}

func TestDeleteListQuery(t *testing.T) {
	tests := []struct {
		name            string
		query           Query
		deleteRequests  []client.DeleteRequest
		expectError     bool
		expectOutput    string
		expectCallCount int
	}{
		{
			name: "empty list",
			query: Query{
				Quiet: false,
			},
			deleteRequests: []client.DeleteRequest{},
			expectError:    false,
			expectOutput:   "Found 0 delete requests:\n",
		},
		{
			name: "single delete request",
			query: Query{
				Quiet: false,
			},
			deleteRequests: []client.DeleteRequest{
				{
					StartTime: 1000,
					EndTime:   2000,
					Query:     "{job=\"test\"}",
					Status:    "received",
				},
			},
			expectError: false,
			expectOutput: "Found 1 delete requests:\n" +
				"Query: {job=\"test\"}\n" +
				"Start Time: " + time.Unix(1000, 0).Format(time.RFC3339) + "\n" +
				"End Time: " + time.Unix(2000, 0).Format(time.RFC3339) + "\n" +
				"Status: received\n" +
				"---\n",
		},
		{
			name: "multiple delete requests",
			query: Query{
				Quiet: false,
			},
			deleteRequests: []client.DeleteRequest{
				{
					StartTime: 1000,
					EndTime:   2000,
					Query:     "{job=\"test1\"}",
					Status:    "received",
				},
				{
					StartTime: 3000,
					EndTime:   4000,
					Query:     "{job=\"test2\"}",
					Status:    "processed",
				},
			},
			expectError: false,
			expectOutput: "Found 2 delete requests:\n" +
				"Query: {job=\"test1\"}\n" +
				"Start Time: " + time.Unix(1000, 0).Format(time.RFC3339) + "\n" +
				"End Time: " + time.Unix(2000, 0).Format(time.RFC3339) + "\n" +
				"Status: received\n" +
				"---\n" +
				"Query: {job=\"test2\"}\n" +
				"Start Time: " + time.Unix(3000, 0).Format(time.RFC3339) + "\n" +
				"End Time: " + time.Unix(4000, 0).Format(time.RFC3339) + "\n" +
				"Status: processed\n" +
				"---\n",
		},
		{
			name: "quiet mode",
			query: Query{
				Quiet: true,
			},
			deleteRequests: []client.DeleteRequest{
				{
					StartTime: 1000,
					EndTime:   2000,
					Query:     "{job=\"test\"}",
					Status:    "received",
				},
			},
			expectError: false,
			expectOutput: "Query: {job=\"test\"}\n" +
				"Start Time: " + time.Unix(1000, 0).Format(time.RFC3339) + "\n" +
				"End Time: " + time.Unix(2000, 0).Format(time.RFC3339) + "\n" +
				"Status: received\n" +
				"---\n",
		},
		{
			name: "API error",
			query: Query{
				Quiet: false,
			},
			deleteRequests: nil,
			expectError:    true,
			expectOutput:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Capture stdout
			oldStdout := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w

			// Create mock client
			mockClient := newMockDeleteClient().withDeleteRequests(tt.deleteRequests)
			if tt.expectError {
				mockClient.withListError(errors.New("API error"))
			}

			// Execute test
			err := tt.query.ListQuery(mockClient)

			// Restore stdout and capture output
			w.Close()
			os.Stdout = oldStdout
			var buf bytes.Buffer
			_, _ = io.Copy(&buf, r)
			output := buf.String()

			// Verify results
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			assert.Equal(t, tt.expectOutput, output)
			assert.Equal(t, 1, mockClient.listDeleteRequestsCalls)
			assert.Equal(t, tt.query.Quiet, mockClient.lastListQuiet)
		})
	}
}

func TestDeleteListQueryJSON(t *testing.T) {
	tests := []struct {
		name           string
		query          Query
		deleteRequests []client.DeleteRequest
		expectError    bool
		expectedJSON   string
	}{
		{
			name: "empty list JSON",
			query: Query{
				Quiet: false,
			},
			deleteRequests: []client.DeleteRequest{},
			expectError:    false,
			expectedJSON:   "[]\n",
		},
		{
			name: "single request JSON",
			query: Query{
				Quiet: false,
			},
			deleteRequests: []client.DeleteRequest{
				{
					StartTime: 1000,
					EndTime:   2000,
					Query:     "{job=\"test\"}",
					Status:    "received",
				},
			},
			expectError: false,
			expectedJSON: "[\n" +
				"  {\n" +
				"    \"start_time\": 1000,\n" +
				"    \"end_time\": 2000,\n" +
				"    \"query\": \"{job=\\\"test\\\"}\",\n" +
				"    \"status\": \"received\"\n" +
				"  }\n" +
				"]\n",
		},
		{
			name: "API error JSON",
			query: Query{
				Quiet: false,
			},
			deleteRequests: nil,
			expectError:    true,
			expectedJSON:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Capture stdout
			oldStdout := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w

			// Create mock client
			mockClient := newMockDeleteClient().withDeleteRequests(tt.deleteRequests)
			if tt.expectError {
				mockClient.withListError(errors.New("API error"))
			}

			// Execute test
			err := tt.query.ListQueryJSON(mockClient)

			// Restore stdout and capture output
			w.Close()
			os.Stdout = oldStdout
			var buf bytes.Buffer
			_, _ = io.Copy(&buf, r)
			output := buf.String()

			// Verify results
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			assert.Equal(t, tt.expectedJSON, output)
			assert.Equal(t, 1, mockClient.listDeleteRequestsCalls)
			assert.Equal(t, tt.query.Quiet, mockClient.lastListQuiet)
		})
	}
}

func TestDeleteCancelQuery(t *testing.T) {
	tests := []struct {
		name         string
		query        Query
		requestID    string
		force        bool
		expectError  bool
		expectOutput string
	}{
		{
			name: "basic cancel request",
			query: Query{
				Quiet: false,
			},
			requestID:    "test-request-123",
			force:        false,
			expectError:  false,
			expectOutput: "Delete request test-request-123 cancelled successfully\n",
		},
		{
			name: "cancel with force",
			query: Query{
				Quiet: false,
			},
			requestID:    "test-request-456",
			force:        true,
			expectError:  false,
			expectOutput: "Delete request test-request-456 cancelled successfully\n",
		},
		{
			name: "cancel quiet mode",
			query: Query{
				Quiet: true,
			},
			requestID:    "test-request-789",
			force:        false,
			expectError:  false,
			expectOutput: "",
		},
		{
			name: "cancel with API error",
			query: Query{
				Quiet: false,
			},
			requestID:    "test-request-error",
			force:        false,
			expectError:  true,
			expectOutput: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Capture stdout
			oldStdout := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w

			// Create mock client
			mockClient := newMockDeleteClient()
			if tt.expectError {
				mockClient.withCancelError(errors.New("API error"))
			}

			// Execute test
			err := tt.query.CancelQuery(mockClient, tt.requestID, tt.force)

			// Restore stdout and capture output
			w.Close()
			os.Stdout = oldStdout
			var buf bytes.Buffer
			_, _ = io.Copy(&buf, r)
			output := buf.String()

			// Verify results
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			assert.Equal(t, tt.expectOutput, output)
			assert.Equal(t, 1, mockClient.cancelDeleteRequestCalls)
			assert.Equal(t, tt.requestID, mockClient.lastCancelRequestID)
			assert.Equal(t, tt.force, mockClient.lastCancelForce)
			assert.Equal(t, tt.query.Quiet, mockClient.lastCancelQuiet)
		})
	}
}

func TestTimeConversion(t *testing.T) {
	tests := []struct {
		name     string
		time     time.Time
		expected string
	}{
		{
			name:     "zero time",
			time:     time.Time{},
			expected: "",
		},
		{
			name:     "unix epoch",
			time:     time.Unix(0, 0),
			expected: "0",
		},
		{
			name:     "specific time",
			time:     time.Unix(1672531200, 0), // 2023-01-01 00:00:00 UTC
			expected: "1672531200",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result string
			if !tt.time.IsZero() {
				result = strconv.FormatInt(tt.time.Unix(), 10)
			}
			assert.Equal(t, tt.expected, result)
		})
	}
}

func BenchmarkDeleteCreateQuery(b *testing.B) {
	mockClient := newMockDeleteClient()
	query := Query{
		QueryString: "{job=\"test\"}",
		Start:       time.Unix(1000, 0),
		End:         time.Unix(2000, 0),
		Quiet:       true,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := query.CreateQuery(mockClient)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDeleteListQuery(b *testing.B) {
	deleteRequests := make([]client.DeleteRequest, 100)
	for i := 0; i < 100; i++ {
		deleteRequests[i] = client.DeleteRequest{
			StartTime: int64(1000 + i),
			EndTime:   int64(2000 + i),
			Query:     fmt.Sprintf("{job=\"test%d\"}", i),
			Status:    "received",
		}
	}

	mockClient := newMockDeleteClient().withDeleteRequests(deleteRequests)
	query := Query{Quiet: true}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := query.ListQuery(mockClient)
		if err != nil {
			b.Fatal(err)
		}
	}
}
