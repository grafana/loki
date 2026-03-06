package client

import (
	"errors"
	"net"
	"net/url"
	"strings"
	"syscall"
	"testing"
)

func TestIsConnectionError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "regular error",
			err:      errors.New("some error"),
			expected: false,
		},
		{
			name: "url.Error with timeout",
			err: &url.Error{
				Op:  "Get",
				URL: "http://localhost:3100",
				Err: &net.OpError{
					Op:  "dial",
					Err: syscall.ETIMEDOUT,
				},
			},
			expected: true,
		},
		{
			name: "net.OpError",
			err: &net.OpError{
				Op:  "dial",
				Err: syscall.ECONNREFUSED,
			},
			expected: true,
		},
		{
			name:     "syscall.ECONNREFUSED",
			err:      syscall.ECONNREFUSED,
			expected: true,
		},
		{
			name:     "connection refused in message",
			err:      errors.New("dial tcp [::1]:3100: connect: connection refused"),
			expected: true,
		},
		{
			name:     "no such host in message",
			err:      errors.New("dial tcp: lookup example.invalid: no such host"),
			expected: true,
		},
		{
			name:     "network unreachable in message",
			err:      errors.New("dial tcp: network is unreachable"),
			expected: true,
		},
		{
			name:     "timeout in message",
			err:      errors.New("context deadline exceeded: timeout"),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsConnectionError(tt.err)
			if result != tt.expected {
				t.Errorf("IsConnectionError() = %v, want %v for error: %v", result, tt.expected, tt.err)
			}
		})
	}
}

func TestFormatConnectionError(t *testing.T) {
	apiURL := "http://localhost:3100"
	endpoint := "/ui/api/v1/goldfish/queries"
	err := errors.New("connection refused")

	result := FormatConnectionError(apiURL, endpoint, err)

	// Check that the error message contains expected parts
	expectedParts := []string{
		"Cannot connect to Goldfish API",
		apiURL + endpoint,
		"kubectl port-forward",
		"connection refused",
	}

	for _, part := range expectedParts {
		if !strings.Contains(result, part) {
			t.Errorf("FormatConnectionError() missing expected part: %s\nGot: %s", part, result)
		}
	}
}

func TestFormatAPIError(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		errorMsg   string
		endpoint   string
		expected   []string
	}{
		{
			name:       "404 error",
			statusCode: 404,
			errorMsg:   "goldfish feature is disabled",
			endpoint:   "/ui/api/v1/goldfish/queries",
			expected: []string{
				"HTTP 404",
				"goldfish feature is disabled",
				"Goldfish is enabled",
			},
		},
		{
			name:       "500 error",
			statusCode: 500,
			errorMsg:   "internal server error",
			endpoint:   "/ui/api/v1/goldfish/queries",
			expected: []string{
				"HTTP 500",
				"internal server error",
				"experiencing issues",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatAPIError(tt.statusCode, tt.errorMsg, tt.endpoint)

			for _, part := range tt.expected {
				if !strings.Contains(result, part) {
					t.Errorf("FormatAPIError() missing expected part: %s\nGot: %s", part, result)
				}
			}
		})
	}
}

func TestFormatResultNotFoundError(t *testing.T) {
	correlationID := "abc123"
	result := FormatResultNotFoundError(correlationID)

	expectedParts := []string{
		correlationID,
		"not available",
		"not persisted",
		"Result persistence is disabled",
	}

	for _, part := range expectedParts {
		if !strings.Contains(result, part) {
			t.Errorf("FormatResultNotFoundError() missing expected part: %s\nGot: %s", part, result)
		}
	}
}
