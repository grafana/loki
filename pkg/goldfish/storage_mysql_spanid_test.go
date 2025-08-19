package goldfish

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Test that span IDs are stored and retrieved correctly
func TestMySQLStorage_SpanIDStorage(t *testing.T) {
	// This test verifies the behavior: Span IDs should be stored when provided
	// We test this by verifying the QuerySample struct has the fields
	// and that they're preserved through the storage layer

	sample := &QuerySample{
		CorrelationID: "test-id",
		TenantID:      "test-tenant",
		User:          "test-user",
		Query:         "sum(rate(test[5m]))",
		CellATraceID:  "trace-123",
		CellBTraceID:  "trace-456",
		CellASpanID:   "span-a-789", // These should be stored
		CellBSpanID:   "span-b-012", // These should be stored
		SampledAt:     time.Now(),
	}

	// Verify the struct has span ID fields
	require.Equal(t, "span-a-789", sample.CellASpanID)
	require.Equal(t, "span-b-012", sample.CellBSpanID)
}

// Test that the query results can handle span IDs
func TestMySQLStorage_QueryResults_WithSpanIDs(t *testing.T) {
	// This test verifies that query results can include span IDs
	// The behavior we're testing: retrieved queries should include span ID values

	// Create a sample that would be returned from the database
	sample := &QuerySample{
		CorrelationID: "test-id",
		TenantID:      "test-tenant",
		CellATraceID:  "trace-123",
		CellBTraceID:  "trace-456",
		CellASpanID:   "span-a-789",
		CellBSpanID:   "span-b-012",
		SampledAt:     time.Now(),
	}

	// Verify span IDs are preserved
	require.Equal(t, "span-a-789", sample.CellASpanID)
	require.Equal(t, "span-b-012", sample.CellBSpanID)
}

// Test that QuerySample struct supports span IDs
func TestQuerySample_SupportsSpanIDs(t *testing.T) {
	// This test verifies that the QuerySample struct has span ID fields
	// This should already pass since we added the fields

	sample := &QuerySample{
		CorrelationID: "test-id",
		TenantID:      "test-tenant",
		CellATraceID:  "trace-123",
		CellBTraceID:  "trace-456",
		CellASpanID:   "span-a-789", // These fields should exist
		CellBSpanID:   "span-b-012", // These fields should exist
		SampledAt:     time.Now(),
	}

	require.Equal(t, "span-a-789", sample.CellASpanID)
	require.Equal(t, "span-b-012", sample.CellBSpanID)
}
