package bench

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

// TestMetadataGenerationIntegration tests the full flow:
// 1. Generate data with Builder
// 2. Metadata file is created
// 3. Metadata can be loaded
// 4. MetadataVariableResolver can use it
func TestMetadataGenerationIntegration(t *testing.T) {
	dir := t.TempDir()

	// Create a generator with small dataset
	opt := DefaultOpt().
		WithNumStreams(10).
		WithSeed(42)

	reg := prometheus.NewRegistry()
	chunkStore, err := NewChunkStoreWithRegisterer(dir, testTenant, reg)
	require.NoError(t, err)

	builder := NewBuilder(dir, opt, chunkStore)

	// Generate small amount of data (1KB)
	err = builder.Generate(context.Background(), 1024)
	require.NoError(t, err)

	// Close store immediately to unregister metrics before next test
	err = chunkStore.Close()
	require.NoError(t, err)

	// Verify metadata file was created
	metadata, err := LoadMetadata(dir)
	require.NoError(t, err)

	// Verify metadata content
	require.Equal(t, metadataVersion, metadata.Version)
	require.Equal(t, 10, metadata.Statistics.TotalStreams)
	require.Len(t, metadata.AllSelectors, 10)
	require.NotEmpty(t, metadata.ByFormat)
	require.NotEmpty(t, metadata.ByServiceName)

	// Verify we can create a resolver from the metadata
	resolver := NewMetadataVariableResolver(metadata, 12)
	require.NotNil(t, resolver)

	// Test that resolver can resolve a simple json query
	jsonQuery := `${SELECTOR} | json`
	jsonReq := QueryRequirements{
		LogFormat: "json",
	}

	resolved, err := resolver.ResolveQuery(jsonQuery, jsonReq, false)
	require.NoError(t, err)
	require.NotEqual(t, jsonQuery, resolved) // Should have replaced ${SELECTOR}
	require.NotContains(t, resolved, "${")   // No variables left

	// Test that resolver can resolve a simple logfmt query
	logfmtQuery := `${SELECTOR} | logfmt`
	logfmtReq := QueryRequirements{
		LogFormat: "logfmt",
	}

	resolved, err = resolver.ResolveQuery(logfmtQuery, logfmtReq, false)
	require.NoError(t, err)
	require.NotEqual(t, logfmtQuery, resolved) // Should have replaced ${SELECTOR}
	require.NotContains(t, resolved, "${")     // No variables left
}

// TestMetadataFormatFiltering verifies that format filtering works correctly
func TestMetadataFormatFiltering(t *testing.T) {
	dir := t.TempDir()

	opt := DefaultOpt().
		WithNumStreams(15). // Enough to get multiple formats
		WithSeed(123)

	reg := prometheus.NewRegistry()
	chunkStore, err := NewChunkStoreWithRegisterer(dir, testTenant, reg)
	require.NoError(t, err)

	builder := NewBuilder(dir, opt, chunkStore)
	err = builder.Generate(context.Background(), 1024)
	require.NoError(t, err)

	// Close store immediately to unregister metrics before next test
	err = chunkStore.Close()
	require.NoError(t, err)

	metadata, err := LoadMetadata(dir)
	require.NoError(t, err)

	// Verify we have multiple formats
	require.NotEmpty(t, metadata.ByFormat[LogFormatJSON])
	require.NotEmpty(t, metadata.ByFormat[LogFormatLogfmt])

	resolver := NewMetadataVariableResolver(metadata, 123)

	// Query with JSON format should only get JSON selectors
	jsonQuery := `${SELECTOR} | json | unwrap rows_affected`
	jsonReq := QueryRequirements{
		LogFormat:         "json",
		UnwrappableFields: []string{"rows_affected"},
	}

	resolvedJSON, err := resolver.ResolveQuery(jsonQuery, jsonReq, false)
	require.NoError(t, err)

	// The resolved selector should be in the JSON format list
	var foundSelector string
	for _, sel := range metadata.ByFormat[LogFormatJSON] {
		if contains(resolvedJSON, sel) {
			foundSelector = sel
			break
		}
	}
	require.NotEmpty(t, foundSelector, "resolved query should contain a JSON format selector")

	// Verify the selector is NOT in the logfmt list
	for _, sel := range metadata.ByFormat[LogFormatLogfmt] {
		require.NotContains(t, resolvedJSON, sel, "JSON query should not contain logfmt selector")
	}
}

// contains checks if s contains substr
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
