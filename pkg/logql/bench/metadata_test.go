package bench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBuildMetadata(t *testing.T) {
	config := &GeneratorConfig{
		StartTime:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		TimeSpread: 24 * time.Hour,
		Seed:       1,
	}

	// Find applications by name to avoid index dependencies
	var database, loki, nginx Service
	for _, app := range defaultApplications {
		switch app.Name {
		case "database":
			database = app
		case "loki":
			loki = app
		case "nginx":
			nginx = app
		}
	}

	// Create sample stream metadata
	streamsMeta := []StreamMetadata{
		{
			Labels:  `{cluster="cluster-0", namespace="namespace-0", service="service-0", pod="pod-0", container="container-0", env="prod", region="us-west-2", datacenter="dc1", service_name="database"}`,
			Service: database,
			Format:  LogFormatJSON,
		},
		{
			Labels:  `{cluster="cluster-0", namespace="namespace-1", service="service-1", pod="pod-1", container="container-0", env="staging", region="us-east-1", datacenter="dc2", service_name="loki"}`,
			Service: loki,
			Format:  LogFormatLogfmt,
		},
		{
			Labels:  `{cluster="cluster-1", namespace="namespace-0", service="service-2", pod="pod-2", container="container-1", env="prod", region="us-west-2", datacenter="dc1", service_name="nginx"}`,
			Service: nginx,
			Format:  LogFormatUnstructured,
		},
	}

	metadata := BuildMetadata(config, streamsMeta)

	// Verify version
	require.Equal(t, metadataVersion, metadata.Version)

	// Verify time range
	require.Equal(t, config.StartTime, metadata.TimeRange.Start)
	require.Equal(t, config.StartTime.Add(config.TimeSpread), metadata.TimeRange.End)

	// Verify all selectors
	require.Len(t, metadata.AllSelectors, 3)
	require.Contains(t, metadata.AllSelectors, streamsMeta[0].Labels)
	require.Contains(t, metadata.AllSelectors, streamsMeta[1].Labels)
	require.Contains(t, metadata.AllSelectors, streamsMeta[2].Labels)

	// Verify format indexes
	require.Len(t, metadata.ByFormat[LogFormatJSON], 1)
	require.Contains(t, metadata.ByFormat[LogFormatJSON], streamsMeta[0].Labels)

	require.Len(t, metadata.ByFormat[LogFormatLogfmt], 1)
	require.Contains(t, metadata.ByFormat[LogFormatLogfmt], streamsMeta[1].Labels)

	require.Len(t, metadata.ByFormat[LogFormatUnstructured], 1)
	require.Contains(t, metadata.ByFormat[LogFormatUnstructured], streamsMeta[2].Labels)

	// Verify application indexes
	require.Len(t, metadata.ByServiceName["database"], 1)
	require.Contains(t, metadata.ByServiceName["database"], streamsMeta[0].Labels)

	require.Len(t, metadata.ByServiceName["loki"], 1)
	require.Contains(t, metadata.ByServiceName["loki"], streamsMeta[1].Labels)

	require.Len(t, metadata.ByServiceName["nginx"], 1)
	require.Contains(t, metadata.ByServiceName["nginx"], streamsMeta[2].Labels)

	// Verify statistics
	require.Equal(t, 3, metadata.Statistics.TotalStreams)
	require.Equal(t, 1, metadata.Statistics.StreamsByFormat[LogFormatJSON])
	require.Equal(t, 1, metadata.Statistics.StreamsByFormat[LogFormatLogfmt])
	require.Equal(t, 1, metadata.Statistics.StreamsByFormat[LogFormatUnstructured])
	require.Equal(t, 1, metadata.Statistics.StreamsByService["database"])
	require.Equal(t, 1, metadata.Statistics.StreamsByService["loki"])
	require.Equal(t, 1, metadata.Statistics.StreamsByService["nginx"])

	// Verify unique labels tracking
	require.Equal(t, 2, metadata.Statistics.UniqueLabels["cluster"])   // cluster-0, cluster-1
	require.Equal(t, 2, metadata.Statistics.UniqueLabels["namespace"]) // namespace-0, namespace-1
	require.Equal(t, 2, metadata.Statistics.UniqueLabels["env"])       // prod, staging
	require.Equal(t, 2, metadata.Statistics.UniqueLabels["region"])    // us-west-2, us-east-1
}

func TestSaveAndLoadMetadata(t *testing.T) {
	dir := t.TempDir()

	config := &GeneratorConfig{
		StartTime:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		TimeSpread: 24 * time.Hour,
		Seed:       1,
	}

	// Find applications by name
	var database, loki Service
	for _, app := range defaultApplications {
		switch app.Name {
		case "database":
			database = app
		case "loki":
			loki = app
		}
	}

	streamsMeta := []StreamMetadata{
		{
			Labels:  `{service_name="database"}`,
			Service: database,
			Format:  LogFormatJSON,
		},
		{
			Labels:  `{service_name="loki"}`,
			Service: loki,
			Format:  LogFormatLogfmt,
		},
	}

	// Build and save metadata
	original := BuildMetadata(config, streamsMeta)
	err := SaveMetadata(dir, original)
	require.NoError(t, err)

	// Verify file exists
	metadataPath := filepath.Join(dir, metadataFileName)
	require.FileExists(t, metadataPath)

	// Verify file is valid JSON
	data, err := os.ReadFile(metadataPath)
	require.NoError(t, err)
	var jsonCheck map[string]interface{}
	err = json.Unmarshal(data, &jsonCheck)
	require.NoError(t, err)

	// Load metadata
	loaded, err := LoadMetadata(dir)
	require.NoError(t, err)

	// Verify loaded metadata matches original
	require.Equal(t, original.Version, loaded.Version)
	require.Equal(t, original.TimeRange, loaded.TimeRange)
	require.Equal(t, original.AllSelectors, loaded.AllSelectors)
	require.Equal(t, original.ByFormat, loaded.ByFormat)
	require.Equal(t, original.ByServiceName, loaded.ByServiceName)
	require.Equal(t, original.Statistics.TotalStreams, loaded.Statistics.TotalStreams)
}

func TestLoadMetadata_SerializesStreamMetadata(t *testing.T) {
	dir := t.TempDir()

	// Use the default config which has all services configured
	config := defaultGeneratorConfig

	// Generate and save metadata
	metadata := GenerateInMemoryMetadata(&config)

	// Verify MetadataBySelector was populated
	require.NotEmpty(t, metadata.MetadataBySelector)

	// Verify all streams have range metadata
	for _, selector := range metadata.AllSelectors {
		streamMeta, ok := metadata.MetadataBySelector[selector]
		require.True(t, ok, "selector %s should exist in MetadataBySelector", selector)
		require.NotZero(t, streamMeta.MinRange, "MinRange should be set for selector %s", selector)
		require.NotZero(t, streamMeta.MinInstantRange, "MinInstantRange should be set for selector %s", selector)
	}

	err := SaveMetadata(dir, metadata)
	require.NoError(t, err)
	loaded, err := LoadMetadata(dir)
	require.NoError(t, err)

	require.Equal(t, len(metadata.MetadataBySelector), len(loaded.MetadataBySelector))

	// Verify each selector has correct range values preserved
	for selector, originalMeta := range metadata.MetadataBySelector {
		loadedMeta, ok := loaded.MetadataBySelector[selector]
		require.True(t, ok, "selector %s should exist in loaded MetadataBySelector", selector)
		require.Equal(t, originalMeta.MinRange, loadedMeta.MinRange, "MinRange should match for selector %s", selector)
		require.Equal(t, originalMeta.MinInstantRange, loadedMeta.MinInstantRange, "MinInstantRange should match for selector %s", selector)
	}

	// Verify resolver can use the loaded metadata
	resolver := NewMetadataVariableResolver(loaded, 1)
	query := "count_over_time(${SELECTOR}[${RANGE}])"
	resolved, err := resolver.ResolveQuery(query, QueryRequirements{}, false)
	require.NoError(t, err)
	require.NotContains(t, resolved, "${SELECTOR}")
	require.NotContains(t, resolved, "${RANGE}")
}

func TestLoadMetadata_VersionMismatch(t *testing.T) {
	dir := t.TempDir()

	// Write metadata with wrong version
	badMetadata := map[string]interface{}{
		"version":        "999.0",
		"time_range":     map[string]string{"start": "2024-01-01T00:00:00Z", "end": "2024-01-02T00:00:00Z"},
		"by_format":      map[string][]string{},
		"by_application": map[string][]string{},
		"all_selectors":  []string{},
		"statistics":     map[string]interface{}{"total_streams": 0},
	}

	data, err := json.Marshal(badMetadata)
	require.NoError(t, err)

	metadataPath := filepath.Join(dir, metadataFileName)
	err = os.WriteFile(metadataPath, data, 0o644)
	require.NoError(t, err)

	// Attempt to load should fail
	_, err = LoadMetadata(dir)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported metadata version")
}

func TestMetadata_Determinism(t *testing.T) {
	config := &GeneratorConfig{
		StartTime:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		TimeSpread: 24 * time.Hour,
		Seed:       1,
	}

	// Find applications by name
	appMap := make(map[string]Service)
	for _, app := range defaultApplications {
		appMap[app.Name] = app
	}

	streamsMeta := []StreamMetadata{
		{Labels: `{service_name="database"}`, Service: appMap["database"], Format: LogFormatJSON},
		{Labels: `{service_name="web-server"}`, Service: appMap["web-server"], Format: LogFormatJSON},
		{Labels: `{service_name="loki"}`, Service: appMap["loki"], Format: LogFormatLogfmt},
	}

	// Build metadata twice
	metadata1 := BuildMetadata(config, streamsMeta)
	metadata2 := BuildMetadata(config, streamsMeta)

	// Should be identical
	require.Equal(t, metadata1.AllSelectors, metadata2.AllSelectors)
	require.Equal(t, metadata1.ByFormat, metadata2.ByFormat)
	require.Equal(t, metadata1.ByServiceName, metadata2.ByServiceName)
}

func TestCalculateMinRanges(t *testing.T) {
	tests := []struct {
		name             string
		timeSpread       time.Duration
		expectMinRange   time.Duration
		expectMinInstant time.Duration
		description      string
	}{
		{
			name:             "Low log rate - 1 hour spread",
			timeSpread:       1 * time.Hour,
			expectMinRange:   5*time.Minute + 27*time.Second,  // 5 logs / (55/3600 logs/sec) = 327.27s, rounded to 327s
			expectMinInstant: 10*time.Minute + 55*time.Second, // 10 logs / (55/3600 logs/sec) = 654.55s, rounded to 655s
			description:      "With 55 logs over 1 hour, rate is ~0.015 logs/sec, needs ~5.5 min for 5 logs, ~11 min for 10 logs",
		},
		{
			name:             "Medium log rate - 10 minutes spread",
			timeSpread:       10 * time.Minute,
			expectMinRange:   1 * time.Minute,                // Calculated: 54.545s rounded to 55s, but minimum threshold of 1 minute applies
			expectMinInstant: 1*time.Minute + 49*time.Second, // 10 logs / (55/600 logs/sec) = 109.09s, rounded to 109s
			description:      "With 55 logs over 10 minutes, rate is ~0.092 logs/sec, needs ~54s for 5 logs (hits min), ~109s for 10 logs",
		},
		{
			name:             "High log rate - 1 minute spread",
			timeSpread:       1 * time.Minute,
			expectMinRange:   1 * time.Minute, // Calculated: 5.45s, but minimum threshold applies
			expectMinInstant: 1 * time.Minute, // Calculated: 10.9s, but minimum threshold applies
			description:      "With 55 logs over 1 minute, rate is ~0.917 logs/sec, hits minimum threshold of 1 minute",
		},
		{
			name:             "Very high log rate - 10 seconds spread",
			timeSpread:       10 * time.Second,
			expectMinRange:   1 * time.Minute, // Calculated: <1s, minimum threshold applies
			expectMinInstant: 1 * time.Minute, // Calculated: <2s, minimum threshold applies
			description:      "With 55 logs over 10 seconds, rate is ~5.5 logs/sec, hits minimum threshold",
		},
		{
			name:             "Very low log rate - 24 hours spread",
			timeSpread:       24 * time.Hour,
			expectMinRange:   15 * time.Minute, // Calculated: ~2.2 hours, maximum threshold applies
			expectMinInstant: 30 * time.Minute, // Calculated: ~4.4 hours, maximum threshold applies
			description:      "With 55 logs over 24 hours, rate is very low, hits maximum thresholds",
		},
		{
			name:             "Edge case - 1 second spread",
			timeSpread:       1 * time.Second,
			expectMinRange:   1 * time.Minute, // Hits minimum threshold
			expectMinInstant: 1 * time.Minute, // Hits minimum threshold
			description:      "With very high rate, minimum threshold applies",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &GeneratorConfig{
				StartTime:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				TimeSpread: tt.timeSpread,
				Seed:       1,
			}

			minRange, minInstantRange := CalculateMinRanges(config)

			// Verify both ranges are positive
			require.Greater(t, minRange, time.Duration(0), "MinRange must be positive")
			require.Greater(t, minInstantRange, time.Duration(0), "MinInstantRange must be positive")

			// Verify MinInstantRange >= MinRange (instant needs equal or more lookback)
			require.GreaterOrEqual(t, minInstantRange, minRange,
				"MinInstantRange (%v) should be >= MinRange (%v)", minInstantRange, minRange)

			// Check expected values with exact equality (calculations are rounded to whole seconds)
			require.Equal(t, tt.expectMinRange, minRange,
				"MinRange: expected %v, got %v (description: %s)", tt.expectMinRange, minRange, tt.description)
			require.Equal(t, tt.expectMinInstant, minInstantRange,
				"MinInstantRange: expected %v, got %v (description: %s)", tt.expectMinInstant, minInstantRange, tt.description)

			// Verify minimum thresholds
			require.GreaterOrEqual(t, minRange, 1*time.Minute, "MinRange must be at least 1 minute")
			require.GreaterOrEqual(t, minInstantRange, 1*time.Minute, "MinInstantRange must be at least 1 minute")

			// Verify maximum thresholds
			require.LessOrEqual(t, minRange, 15*time.Minute, "MinRange must not exceed 15 minutes")
			require.LessOrEqual(t, minInstantRange, 30*time.Minute, "MinInstantRange must not exceed 30 minutes")
		})
	}
}

func TestStreamMetadata_PopulatesRanges(t *testing.T) {
	// Test that generator populates MinRange and MinInstantRange in StreamMetadata
	opt := DefaultOpt().
		WithStartTime(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)).
		WithTimeSpread(1 * time.Hour).
		WithNumStreams(10).
		WithSeed(1)

	gen := NewGenerator(opt)
	gen.generateStreamMetadata()

	// Calculate expected values
	expectedMinRange, expectedMinInstantRange := CalculateMinRanges(&gen.config)

	// Verify all streams have ranges populated
	require.Len(t, gen.StreamsMeta, 10, "Should generate 10 streams")

	for i, stream := range gen.StreamsMeta {
		require.Greater(t, stream.MinRange, time.Duration(0),
			"Stream %d: MinRange should be positive", i)
		require.Greater(t, stream.MinInstantRange, time.Duration(0),
			"Stream %d: MinInstantRange should be positive", i)

		require.Equal(t, expectedMinRange, stream.MinRange,
			"Stream %d: MinRange should match calculated value", i)
		require.Equal(t, expectedMinInstantRange, stream.MinInstantRange,
			"Stream %d: MinInstantRange should match calculated value", i)

		require.GreaterOrEqual(t, stream.MinInstantRange, stream.MinRange,
			"Stream %d: MinInstantRange should be >= MinRange", i)
	}
}
