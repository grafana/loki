package enrichment

import (
	"testing"
	"time"

	"github.com/grafana/loki/v3/pkg/goldfish"
)

func TestCalculateDifferenceSummary(t *testing.T) {
	tests := []struct {
		name     string
		sample   goldfish.QuerySample
		expected DifferenceSummary
	}{
		{
			name: "perfect match - all equal",
			sample: goldfish.QuerySample{
				CellAStatusCode:   200,
				CellBStatusCode:   200,
				CellAResponseSize: 1024,
				CellBResponseSize: 1024,
				CellAResponseHash: "abc123",
				CellBResponseHash: "abc123",
			},
			expected: DifferenceSummary{
				StatusCodeMatch:   true,
				SizeMatch:         true,
				HashMatch:         true,
				SizeDeltaBytes:    0,
				CellAStatusCode:   200,
				CellBStatusCode:   200,
				CellAResponseSize: 1024,
				CellBResponseSize: 1024,
			},
		},
		{
			name: "mismatch - status codes differ",
			sample: goldfish.QuerySample{
				CellAStatusCode:   200,
				CellBStatusCode:   500,
				CellAResponseSize: 1024,
				CellBResponseSize: 1024,
				CellAResponseHash: "abc123",
				CellBResponseHash: "abc123",
			},
			expected: DifferenceSummary{
				StatusCodeMatch:   false,
				SizeMatch:         true,
				HashMatch:         true,
				SizeDeltaBytes:    0,
				CellAStatusCode:   200,
				CellBStatusCode:   500,
				CellAResponseSize: 1024,
				CellBResponseSize: 1024,
			},
		},
		{
			name: "mismatch - sizes differ (B larger)",
			sample: goldfish.QuerySample{
				CellAStatusCode:   200,
				CellBStatusCode:   200,
				CellAResponseSize: 1024,
				CellBResponseSize: 2048,
				CellAResponseHash: "abc123",
				CellBResponseHash: "def456",
			},
			expected: DifferenceSummary{
				StatusCodeMatch:   true,
				SizeMatch:         false,
				HashMatch:         false,
				SizeDeltaBytes:    1024,
				CellAStatusCode:   200,
				CellBStatusCode:   200,
				CellAResponseSize: 1024,
				CellBResponseSize: 2048,
			},
		},
		{
			name: "mismatch - sizes differ (A larger)",
			sample: goldfish.QuerySample{
				CellAStatusCode:   200,
				CellBStatusCode:   200,
				CellAResponseSize: 3072,
				CellBResponseSize: 1024,
				CellAResponseHash: "abc123",
				CellBResponseHash: "def456",
			},
			expected: DifferenceSummary{
				StatusCodeMatch:   true,
				SizeMatch:         false,
				HashMatch:         false,
				SizeDeltaBytes:    -2048,
				CellAStatusCode:   200,
				CellBStatusCode:   200,
				CellAResponseSize: 3072,
				CellBResponseSize: 1024,
			},
		},
		{
			name: "mismatch - only hash differs",
			sample: goldfish.QuerySample{
				CellAStatusCode:   200,
				CellBStatusCode:   200,
				CellAResponseSize: 1024,
				CellBResponseSize: 1024,
				CellAResponseHash: "abc123",
				CellBResponseHash: "def456",
			},
			expected: DifferenceSummary{
				StatusCodeMatch:   true,
				SizeMatch:         true,
				HashMatch:         false,
				SizeDeltaBytes:    0,
				CellAStatusCode:   200,
				CellBStatusCode:   200,
				CellAResponseSize: 1024,
				CellBResponseSize: 1024,
			},
		},
		{
			name: "everything differs",
			sample: goldfish.QuerySample{
				CellAStatusCode:   200,
				CellBStatusCode:   404,
				CellAResponseSize: 1024,
				CellBResponseSize: 512,
				CellAResponseHash: "abc123",
				CellBResponseHash: "def456",
			},
			expected: DifferenceSummary{
				StatusCodeMatch:   false,
				SizeMatch:         false,
				HashMatch:         false,
				SizeDeltaBytes:    -512,
				CellAStatusCode:   200,
				CellBStatusCode:   404,
				CellAResponseSize: 1024,
				CellBResponseSize: 512,
			},
		},
		{
			name: "zero sizes",
			sample: goldfish.QuerySample{
				CellAStatusCode:   204,
				CellBStatusCode:   204,
				CellAResponseSize: 0,
				CellBResponseSize: 0,
				CellAResponseHash: "",
				CellBResponseHash: "",
			},
			expected: DifferenceSummary{
				StatusCodeMatch:   true,
				SizeMatch:         true,
				HashMatch:         true,
				SizeDeltaBytes:    0,
				CellAStatusCode:   204,
				CellBStatusCode:   204,
				CellAResponseSize: 0,
				CellBResponseSize: 0,
			},
		},
		{
			name: "empty hashes but different sizes",
			sample: goldfish.QuerySample{
				CellAStatusCode:   200,
				CellBStatusCode:   200,
				CellAResponseSize: 100,
				CellBResponseSize: 200,
				CellAResponseHash: "",
				CellBResponseHash: "",
			},
			expected: DifferenceSummary{
				StatusCodeMatch:   true,
				SizeMatch:         false,
				HashMatch:         true,
				SizeDeltaBytes:    100,
				CellAStatusCode:   200,
				CellBStatusCode:   200,
				CellAResponseSize: 100,
				CellBResponseSize: 200,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CalculateDifferenceSummary(tt.sample)

			if result.StatusCodeMatch != tt.expected.StatusCodeMatch {
				t.Errorf("StatusCodeMatch: got %v, want %v", result.StatusCodeMatch, tt.expected.StatusCodeMatch)
			}
			if result.SizeMatch != tt.expected.SizeMatch {
				t.Errorf("SizeMatch: got %v, want %v", result.SizeMatch, tt.expected.SizeMatch)
			}
			if result.HashMatch != tt.expected.HashMatch {
				t.Errorf("HashMatch: got %v, want %v", result.HashMatch, tt.expected.HashMatch)
			}
			if result.SizeDeltaBytes != tt.expected.SizeDeltaBytes {
				t.Errorf("SizeDeltaBytes: got %d, want %d", result.SizeDeltaBytes, tt.expected.SizeDeltaBytes)
			}
			if result.CellAStatusCode != tt.expected.CellAStatusCode {
				t.Errorf("CellAStatusCode: got %d, want %d", result.CellAStatusCode, tt.expected.CellAStatusCode)
			}
			if result.CellBStatusCode != tt.expected.CellBStatusCode {
				t.Errorf("CellBStatusCode: got %d, want %d", result.CellBStatusCode, tt.expected.CellBStatusCode)
			}
			if result.CellAResponseSize != tt.expected.CellAResponseSize {
				t.Errorf("CellAResponseSize: got %d, want %d", result.CellAResponseSize, tt.expected.CellAResponseSize)
			}
			if result.CellBResponseSize != tt.expected.CellBResponseSize {
				t.Errorf("CellBResponseSize: got %d, want %d", result.CellBResponseSize, tt.expected.CellBResponseSize)
			}
		})
	}
}

func TestCalculateDifferenceSummaryWithCompleteQuerySample(t *testing.T) {
	// Test with a more complete QuerySample to ensure we're not affected by other fields
	sample := goldfish.QuerySample{
		CorrelationID:          "test-correlation-123",
		TenantID:               "tenant-1",
		User:                   "user@example.com",
		Query:                  "sum(rate({app=\"test\"}[5m]))",
		QueryType:              "range",
		StartTime:              time.Now().Add(-1 * time.Hour),
		EndTime:                time.Now(),
		CellAStatusCode:        200,
		CellBStatusCode:        200,
		CellAResponseSize:      5000,
		CellBResponseSize:      5500,
		CellAResponseHash:      "hash-a",
		CellBResponseHash:      "hash-b",
		ComparisonStatus:       "mismatch",
		MatchWithinTolerance:   false,
		CellAUsedNewEngine:     false,
		CellBUsedNewEngine:     true,
		CellATraceID:           "trace-a",
		CellBTraceID:           "trace-b",
		CellAResultURI:         "s3://bucket/result-a",
		CellBResultURI:         "s3://bucket/result-b",
		CellAResultCompression: "gzip",
		CellBResultCompression: "gzip",
		CellAStats: goldfish.QueryStats{
			ExecTimeMs:     100,
			BytesProcessed: 1000,
			LinesProcessed: 500,
		},
		CellBStats: goldfish.QueryStats{
			ExecTimeMs:     150,
			BytesProcessed: 1200,
			LinesProcessed: 550,
		},
	}

	result := CalculateDifferenceSummary(sample)

	// Verify only the relevant fields are used
	if result.StatusCodeMatch != true {
		t.Errorf("StatusCodeMatch: got %v, want true", result.StatusCodeMatch)
	}
	if result.SizeMatch != false {
		t.Errorf("SizeMatch: got %v, want false", result.SizeMatch)
	}
	if result.HashMatch != false {
		t.Errorf("HashMatch: got %v, want false", result.HashMatch)
	}
	if result.SizeDeltaBytes != 500 {
		t.Errorf("SizeDeltaBytes: got %d, want 500", result.SizeDeltaBytes)
	}
	if result.CellAStatusCode != 200 {
		t.Errorf("CellAStatusCode: got %d, want 200", result.CellAStatusCode)
	}
	if result.CellBStatusCode != 200 {
		t.Errorf("CellBStatusCode: got %d, want 200", result.CellBStatusCode)
	}
	if result.CellAResponseSize != 5000 {
		t.Errorf("CellAResponseSize: got %d, want 5000", result.CellAResponseSize)
	}
	if result.CellBResponseSize != 5500 {
		t.Errorf("CellBResponseSize: got %d, want 5500", result.CellBResponseSize)
	}
}
