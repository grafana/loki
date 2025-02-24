package bench

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
)

// TestGenerateDatasetDeterminism tests the determinism of the dataset generator
// This is important to ensure that the same configuration always produces the same dataset.
// This way multiple runs of the benchmark will be more consistent and the results will be more reliable.
func TestGenerateDatasetDeterminism(t *testing.T) {
	// Create a test configuration with fixed timestamps
	cfg := GeneratorConfig{
		StartTime:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		TimeSpread: 24 * time.Hour,
		DenseIntervals: []struct {
			Start    time.Time
			Duration time.Duration
		}{
			{
				Start:    time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
				Duration: time.Hour,
			},
			{
				Start:    time.Date(2024, 1, 1, 15, 0, 0, 0, time.UTC),
				Duration: 30 * time.Minute,
			},
		},
		LabelConfig: defaultLabelConfig,
	}

	// Generate two sets of streams with the same configuration
	numStreams := 100
	var streams1, streams2 []logproto.Stream

	g1 := NewGenerator(Opt{
		startTime:      cfg.StartTime,
		timeSpread:     cfg.TimeSpread,
		denseIntervals: cfg.DenseIntervals,
		labelConfig:    cfg.LabelConfig,
		numStreams:     numStreams,
		seed:           1,
	})

	g2 := NewGenerator(Opt{
		startTime:      cfg.StartTime,
		timeSpread:     cfg.TimeSpread,
		denseIntervals: cfg.DenseIntervals,
		labelConfig:    cfg.LabelConfig,
		numStreams:     numStreams,
		seed:           1,
	})

	// Collect streams from first generator
	for batch := range g1.Batches() {
		streams1 = append(streams1, batch.Streams...)
	}

	// Collect streams from second generator
	for batch := range g2.Batches() {
		streams2 = append(streams2, batch.Streams...)
	}

	// Compare number of streams
	require.Equal(t, len(streams1), len(streams2), "Number of streams should match")

	// Compare each stream
	for i := range streams1 {
		stream1 := streams1[i]
		stream2 := streams2[i]

		// Compare labels
		require.Equal(t, stream1.Labels, stream2.Labels, "Labels should match for stream %d", i)

		// Compare number of entries
		require.Equal(t, len(stream1.Entries), len(stream2.Entries), "Number of entries should match for stream %d", i)

		// Compare each entry
		for j := range stream1.Entries {
			entry1 := stream1.Entries[j]
			entry2 := stream2.Entries[j]

			require.Equal(t, entry1.Timestamp, entry2.Timestamp, "Timestamp should match for stream %d entry %d", i, j)
			require.Equal(t, entry1.Line, entry2.Line, "Line should match for stream %d entry %d", i, j)
			require.Equal(t, len(entry1.StructuredMetadata), len(entry2.StructuredMetadata), "StructuredMetadata length should match for stream %d entry %d", i, j)

			// Compare structured metadata
			for k := range entry1.StructuredMetadata {
				meta1 := entry1.StructuredMetadata[k]
				meta2 := entry2.StructuredMetadata[k]
				require.Equal(t, meta1.Name, meta2.Name, "Metadata name should match for stream %d entry %d metadata %d", i, j, k)
				require.Equal(t, meta1.Value, meta2.Value, "Metadata value should match for stream %d entry %d metadata %d", i, j, k)
			}
		}
	}
}

// TestQueryDeterminism tests that LogQL queries on the generated dataset
// produce deterministic results. This ensures that benchmark results are
// consistent across multiple runs.
func TestQueryDeterminism(t *testing.T) {
	// Create a test configuration with fixed timestamps
	cfg := GeneratorConfig{
		StartTime:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		TimeSpread: 24 * time.Hour,
		DenseIntervals: []struct {
			Start    time.Time
			Duration time.Duration
		}{
			{
				Start:    time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
				Duration: time.Hour,
			},
		},
		LabelConfig: defaultLabelConfig,
	}

	// Generate test cases with the same configuration
	cases1 := cfg.GenerateTestCases()
	cases2 := cfg.GenerateTestCases()

	require.Equal(t, len(cases1), len(cases2), "Number of test cases should match")

	// Compare each test case
	for i := range cases1 {
		case1 := cases1[i]
		case2 := cases2[i]

		// Compare test case properties
		require.Equal(t, case1.Name(), case2.Name(), "Test case names should match")
		require.Equal(t, case1.Query, case2.Query, "Queries should match")
		require.Equal(t, case1.Start, case2.Start, "Start times should match")
		require.Equal(t, case1.End, case2.End, "End times should match")
	}
}
