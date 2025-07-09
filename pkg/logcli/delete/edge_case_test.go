package delete

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/grafana/loki/v3/pkg/logcli/client"
)

// TestEdgeCasesParameterValidation tests edge cases and parameter validation
func TestEdgeCasesParameterValidation(t *testing.T) {
	tests := []struct {
		name        string
		query       Query
		expectError bool
		description string
	}{
		{
			name: "empty query string",
			query: Query{
				QueryString: "",
				Start:       time.Unix(1000, 0),
				End:         time.Unix(2000, 0),
				Quiet:       true,
			},
			expectError: false,
			description: "Empty query string should be allowed",
		},
		{
			name: "start time after end time",
			query: Query{
				QueryString: "{job=\"test\"}",
				Start:       time.Unix(2000, 0),
				End:         time.Unix(1000, 0),
				Quiet:       true,
			},
			expectError: false,
			description: "Start time after end time should be handled by server",
		},
		{
			name: "very long query string",
			query: Query{
				QueryString: "{job=\"" + generateLongString(10000) + "\"}",
				Start:       time.Unix(1000, 0),
				End:         time.Unix(2000, 0),
				Quiet:       true,
			},
			expectError: false,
			description: "Very long query string should be handled",
		},
		{
			name: "special characters in query",
			query: Query{
				QueryString: "{job=\"test with spaces and 特殊文字 and émojis 🚀\"}",
				Start:       time.Unix(1000, 0),
				End:         time.Unix(2000, 0),
				Quiet:       true,
			},
			expectError: false,
			description: "Special characters should be handled",
		},
		{
			name: "zero times",
			query: Query{
				QueryString: "{job=\"test\"}",
				Start:       time.Time{},
				End:         time.Time{},
				Quiet:       true,
			},
			expectError: false,
			description: "Zero times should be handled",
		},
		{
			name: "max interval with special format",
			query: Query{
				QueryString: "{job=\"test\"}",
				Start:       time.Unix(1000, 0),
				End:         time.Unix(2000, 0),
				MaxInterval: "1h30m45s",
				Quiet:       true,
			},
			expectError: false,
			description: "Complex max interval should be handled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := newMockDeleteClient()
			if tt.expectError {
				mockClient.withCreateError(errors.New("validation error"))
			}

			err := tt.query.CreateQuery(mockClient)
			if tt.expectError {
				assert.Error(t, err, tt.description)
			} else {
				assert.NoError(t, err, tt.description)
			}
		})
	}
}

// TestEdgeCasesListBehavior tests edge cases in list behavior
func TestEdgeCasesListBehavior(t *testing.T) {
	tests := []struct {
		name           string
		deleteRequests []client.DeleteRequest
		expectError    bool
		description    string
	}{
		{
			name:           "empty list",
			deleteRequests: []client.DeleteRequest{},
			expectError:    false,
			description:    "Empty list should be handled gracefully",
		},
		{
			name: "single request with missing fields",
			deleteRequests: []client.DeleteRequest{
				{
					StartTime: 0,
					EndTime:   0,
					Query:     "",
					Status:    "",
				},
			},
			expectError: false,
			description: "Missing fields should be handled",
		},
		{
			name: "request with negative timestamps",
			deleteRequests: []client.DeleteRequest{
				{
					StartTime: -1000,
					EndTime:   -500,
					Query:     "{job=\"test\"}",
					Status:    "received",
				},
			},
			expectError: false,
			description: "Negative timestamps should be handled",
		},
		{
			name: "request with very large timestamps",
			deleteRequests: []client.DeleteRequest{
				{
					StartTime: 9223372036854775807, // max int64
					EndTime:   9223372036854775807,
					Query:     "{job=\"test\"}",
					Status:    "received",
				},
			},
			expectError: false,
			description: "Very large timestamps should be handled",
		},
		{
			name: "request with unusual status values",
			deleteRequests: []client.DeleteRequest{
				{
					StartTime: 1000,
					EndTime:   2000,
					Query:     "{job=\"test\"}",
					Status:    "UNKNOWN_STATUS_VALUE",
				},
			},
			expectError: false,
			description: "Unusual status values should be handled",
		},
		{
			name: "very large number of requests",
			deleteRequests: generateLargeRequestList(1000),
			expectError:    false,
			description:    "Large number of requests should be handled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query := Query{Quiet: true}
			mockClient := newMockDeleteClient().withDeleteRequests(tt.deleteRequests)
			if tt.expectError {
				mockClient.withListError(errors.New("list error"))
			}

			err := query.ListQuery(mockClient)
			if tt.expectError {
				assert.Error(t, err, tt.description)
			} else {
				assert.NoError(t, err, tt.description)
			}
		})
	}
}

// TestEdgeCasesCancelBehavior tests edge cases in cancel behavior
func TestEdgeCasesCancelBehavior(t *testing.T) {
	tests := []struct {
		name        string
		requestID   string
		force       bool
		expectError bool
		description string
	}{
		{
			name:        "empty request ID",
			requestID:   "",
			force:       false,
			expectError: false,
			description: "Empty request ID should be handled by server",
		},
		{
			name:        "very long request ID",
			requestID:   generateLongString(10000),
			force:       false,
			expectError: false,
			description: "Very long request ID should be handled",
		},
		{
			name:        "request ID with special characters",
			requestID:   "test-request-with-特殊文字-and-émojis-🚀",
			force:       false,
			expectError: false,
			description: "Special characters in request ID should be handled",
		},
		{
			name:        "request ID with only whitespace",
			requestID:   "   \t\n   ",
			force:       false,
			expectError: false,
			description: "Whitespace-only request ID should be handled",
		},
		{
			name:        "valid request ID with force",
			requestID:   "valid-request-123",
			force:       true,
			expectError: false,
			description: "Valid request with force should work",
		},
		{
			name:        "valid request ID without force",
			requestID:   "valid-request-456",
			force:       false,
			expectError: false,
			description: "Valid request without force should work",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query := Query{Quiet: true}
			mockClient := newMockDeleteClient()
			if tt.expectError {
				mockClient.withCancelError(errors.New("cancel error"))
			}

			err := query.CancelQuery(mockClient, tt.requestID, tt.force)
			if tt.expectError {
				assert.Error(t, err, tt.description)
			} else {
				assert.NoError(t, err, tt.description)
			}
		})
	}
}

// TestEdgeCasesJSONOutput tests edge cases in JSON output
func TestEdgeCasesJSONOutput(t *testing.T) {
	tests := []struct {
		name           string
		deleteRequests []client.DeleteRequest
		expectError    bool
		description    string
	}{
		{
			name:           "empty list JSON",
			deleteRequests: []client.DeleteRequest{},
			expectError:    false,
			description:    "Empty list should produce valid JSON",
		},
		{
			name: "request with quotes in query",
			deleteRequests: []client.DeleteRequest{
				{
					StartTime: 1000,
					EndTime:   2000,
					Query:     "{job=\"test with \\\"quotes\\\" inside\"}",
					Status:    "received",
				},
			},
			expectError: false,
			description: "Quotes in query should be properly escaped",
		},
		{
			name: "request with unicode characters",
			deleteRequests: []client.DeleteRequest{
				{
					StartTime: 1000,
					EndTime:   2000,
					Query:     "{job=\"test with 特殊文字 and émojis 🚀\"}",
					Status:    "received",
				},
			},
			expectError: false,
			description: "Unicode characters should be properly encoded",
		},
		{
			name: "request with newlines and tabs",
			deleteRequests: []client.DeleteRequest{
				{
					StartTime: 1000,
					EndTime:   2000,
					Query:     "{job=\"test\\nwith\\ttabs\"}",
					Status:    "received",
				},
			},
			expectError: false,
			description: "Newlines and tabs should be properly escaped",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query := Query{Quiet: true}
			mockClient := newMockDeleteClient().withDeleteRequests(tt.deleteRequests)
			if tt.expectError {
				mockClient.withListError(errors.New("list error"))
			}

			err := query.ListQueryJSON(mockClient)
			if tt.expectError {
				assert.Error(t, err, tt.description)
			} else {
				assert.NoError(t, err, tt.description)
			}
		})
	}
}

// TestEdgeCasesTimeConversion tests edge cases in time conversion
func TestEdgeCasesTimeConversion(t *testing.T) {
	tests := []struct {
		name        string
		timestamp   int64
		expectPanic bool
		description string
	}{
		{
			name:        "unix epoch",
			timestamp:   0,
			expectPanic: false,
			description: "Unix epoch should be handled",
		},
		{
			name:        "negative timestamp",
			timestamp:   -1000,
			expectPanic: false,
			description: "Negative timestamp should be handled",
		},
		{
			name:        "max int64",
			timestamp:   9223372036854775807,
			expectPanic: false,
			description: "Max int64 should be handled",
		},
		{
			name:        "min int64",
			timestamp:   -9223372036854775808,
			expectPanic: false,
			description: "Min int64 should be handled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					if !tt.expectPanic {
						t.Errorf("Unexpected panic: %v", r)
					}
				}
			}()

			// Test time conversion
			timeObj := time.Unix(tt.timestamp, 0)
			formatted := timeObj.Format(time.RFC3339)
			
			assert.NotEmpty(t, formatted, tt.description)
		})
	}
}

// Helper functions for edge case testing
func generateLongString(length int) string {
	result := make([]byte, length)
	for i := 0; i < length; i++ {
		result[i] = 'a' + byte(i%26)
	}
	return string(result)
}

func generateLargeRequestList(count int) []client.DeleteRequest {
	requests := make([]client.DeleteRequest, count)
	for i := 0; i < count; i++ {
		requests[i] = client.DeleteRequest{
			StartTime: int64(1000 + i),
			EndTime:   int64(2000 + i),
			Query:     "{job=\"test" + string(rune(i)) + "\"}",
			Status:    "received",
		}
	}
	return requests
}