package util

import (
	"errors"
	"testing"

	"github.com/grafana/dskit/grpcutil"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestMultiError_GRPCStatus(t *testing.T) {
	tests := []struct {
		name     string
		errors   []error
		expected codes.Code
	}{
		{
			name:     "empty MultiError returns nil",
			errors:   []error{},
			expected: codes.OK, // nil status should be treated as OK for test purposes
		},
		{
			name: "single gRPC error",
			errors: []error{
				status.Error(codes.Code(400), "bad request"),
			},
			expected: codes.Code(400),
		},
		{
			name: "client error takes precedence over server error",
			errors: []error{
				status.Error(codes.Code(500), "server error"),
				status.Error(codes.Code(400), "client error"),
			},
			expected: codes.Code(400),
		},
		{
			name: "higher client error code takes precedence",
			errors: []error{
				status.Error(codes.Code(400), "400 error"),
				status.Error(codes.Code(404), "404 error"),
			},
			expected: codes.Code(404),
		},
		{
			name: "higher server error code takes precedence when no client errors",
			errors: []error{
				status.Error(codes.Code(500), "500 error"),
				status.Error(codes.Code(503), "503 error"),
			},
			expected: codes.Code(503),
		},
		{
			name: "mixed errors with client error winning",
			errors: []error{
				status.Error(codes.Code(500), "server error"),
				errors.New("regular error"),
				status.Error(codes.Code(403), "client error"),
				status.Error(codes.Code(503), "another server error"),
			},
			expected: codes.Code(403),
		},
		{
			name: "non-gRPC errors are ignored",
			errors: []error{
				errors.New("regular error 1"),
				errors.New("regular error 2"),
			},
			expected: codes.OK, // no gRPC status found
		},
		{
			name: "mixed with non-gRPC errors",
			errors: []error{
				errors.New("regular error"),
				status.Error(codes.Code(400), "gRPC error"),
				errors.New("another regular error"),
			},
			expected: codes.Code(400),
		},
		{
			name: "nested MultiError with gRPC status",
			errors: []error{
				createMultiErrorWithGRPCStatus(codes.Code(500)),
				status.Error(codes.Code(400), "direct error"),
			},
			expected: codes.Code(400),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			me := MultiError(tt.errors)
			st := me.GRPCStatus()

			if tt.expected == codes.OK {
				if st != nil {
					t.Errorf("expected nil status, got %v", st)
				}
				return
			}

			if st == nil {
				t.Errorf("expected status with code %v, got nil", tt.expected)
				return
			}

			if st.Code() != tt.expected {
				t.Errorf("expected code %v, got %v", tt.expected, st.Code())
			}
		})
	}
}

func TestShouldTakePrecedence(t *testing.T) {
	tests := []struct {
		name        string
		newCode     codes.Code
		currentCode codes.Code
		expected    bool
	}{
		{
			name:        "any code takes precedence over OK",
			newCode:     codes.Code(400),
			currentCode: codes.OK,
			expected:    true,
		},
		{
			name:        "client error takes precedence over server error",
			newCode:     codes.Code(400), // client error
			currentCode: codes.Code(500), // server error
			expected:    true,
		},
		{
			name:        "server error does not take precedence over client error",
			newCode:     codes.Code(500), // server error
			currentCode: codes.Code(400), // client error
			expected:    false,
		},
		{
			name:        "higher client error takes precedence",
			newCode:     codes.Code(404), // higher client error
			currentCode: codes.Code(400), // lower client error
			expected:    true,
		},
		{
			name:        "lower client error does not take precedence",
			newCode:     codes.Code(400), // lower client error
			currentCode: codes.Code(404), // higher client error
			expected:    false,
		},
		{
			name:        "higher server error takes precedence",
			newCode:     codes.Code(503), // higher server error
			currentCode: codes.Code(500), // lower server error
			expected:    true,
		},
		{
			name:        "lower server error does not take precedence",
			newCode:     codes.Code(500), // lower server error
			currentCode: codes.Code(503), // higher server error
			expected:    false,
		},
		{
			name:        "non-client/server errors use numeric precedence",
			newCode:     codes.Code(300), // higher code
			currentCode: codes.Code(200), // lower code
			expected:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shouldTakePrecedence(tt.newCode, tt.currentCode)
			if result != tt.expected {
				t.Errorf("shouldTakePrecedence(%v, %v) = %v, expected %v",
					tt.newCode, tt.currentCode, result, tt.expected)
			}
		})
	}
}

// Helper function to create a MultiError that implements GRPCStatus
func createMultiErrorWithGRPCStatus(code codes.Code) error {
	me := MultiError{status.Error(code, "nested error")}
	return me
}

// Test integration with grpcutil.ErrorToStatusCode-like functionality
func TestMultiError_IntegrationWithGRPCUtil(t *testing.T) {
	// Test the scenario from the investigation: 400 error nested in MultiError
	multiErr := MultiError{
		status.Error(codes.Code(400), "parse error"),
		status.Error(codes.Code(500), "server error"),
	}

	code := grpcutil.ErrorToStatusCode(multiErr)
	if code != codes.Code(400) {
		t.Errorf("expected 400, got %v", code)
	}

	// Test server errors only
	serverOnlyErr := MultiError{
		status.Error(codes.Code(500), "server error 1"),
		status.Error(codes.Code(503), "server error 2"),
	}

	code = grpcutil.ErrorToStatusCode(serverOnlyErr)
	if code != codes.Code(503) {
		t.Errorf("expected 503, got %v", code)
	}
}

// Test integration with the actual grpcutil.ErrorToStatusCode function
func TestMultiError_WithActualGRPCUtil(t *testing.T) {
	// Test the exact scenario from the investigation: 400 error nested in MultiError should return 400, not 500
	multiErr := MultiError{
		status.Error(codes.Code(400), "parse error"),  // This is the 400 error from the investigation
		status.Error(codes.Code(500), "server error"), // This would normally cause a 500
	}

	code := grpcutil.ErrorToStatusCode(multiErr)
	if code != codes.Code(400) {
		t.Errorf("expected 400, got %v (%d)", code, grpcutil.ErrorToStatusCode(multiErr))
	}

	// Test that server errors still work when no client errors are present
	serverOnlyErr := MultiError{
		status.Error(codes.Code(500), "server error 1"),
		status.Error(codes.Code(503), "server error 2"),
	}

	code = grpcutil.ErrorToStatusCode(serverOnlyErr)
	if code != codes.Code(503) {
		t.Errorf("expected 503, got %v", code)
	}

	// Test that non-gRPC errors return Unknown
	regularErr := MultiError{
		errors.New("regular error 1"),
		errors.New("regular error 2"),
	}

	code = grpcutil.ErrorToStatusCode(regularErr)
	if code != codes.Unknown {
		t.Errorf("expected Unknown for non-gRPC errors, got %v", code)
	}
}
