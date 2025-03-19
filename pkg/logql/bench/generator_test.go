package bench

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestGenerateDatasetDeterminism tests the determinism of the dataset generator
// This is important to ensure that the same configuration always produces the same dataset.
// This way multiple runs of the benchmark will be more consistent and the results will be more reliable.
func TestGenerateDatasetDeterminism(t *testing.T) {
	// Create a test configuration with fixed timestamps
	cfg := GeneratorConfig{
		StartTime:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		TimeSpread: 24 * time.Hour,
		DenseIntervals: []DenseInterval{
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

	// Compare only the first 10 batches
	numBatchesToCompare := 10

	// Collect batches from both generators
	var batches1, batches2 []*Batch

	// Collect batches from first generator
	batchCount1 := 0
	for batch := range g1.Batches() {
		batches1 = append(batches1, batch)
		batchCount1++
		if batchCount1 >= numBatchesToCompare {
			break
		}
	}

	// Collect batches from second generator
	batchCount2 := 0
	for batch := range g2.Batches() {
		batches2 = append(batches2, batch)
		batchCount2++
		if batchCount2 >= numBatchesToCompare {
			break
		}
	}

	// Compare number of batches
	require.Equal(t, numBatchesToCompare, len(batches1), "Number of batches from first generator should match expected count")
	require.Equal(t, numBatchesToCompare, len(batches2), "Number of batches from second generator should match expected count")

	// Compare each batch
	for i := 0; i < numBatchesToCompare; i++ {
		batch1 := batches1[i]
		batch2 := batches2[i]

		// Compare number of streams in each batch
		require.Equal(t, len(batch1.Streams), len(batch2.Streams), "Number of streams should match for batch %d", i)

		// Compare each stream in the batch
		for j := range batch1.Streams {
			stream1 := batch1.Streams[j]
			stream2 := batch2.Streams[j]

			// Compare labels
			require.Equal(t, stream1.Labels, stream2.Labels, "Labels should match for batch %d stream %d", i, j)

			// Compare number of entries
			require.Equal(t, len(stream1.Entries), len(stream2.Entries), "Number of entries should match for batch %d stream %d", i, j)

			// Compare each entry
			for k := range stream1.Entries {
				entry1 := stream1.Entries[k]
				entry2 := stream2.Entries[k]

				require.Equal(t, entry1.Timestamp, entry2.Timestamp, "Timestamp should match for batch %d stream %d entry %d", i, j, k)
				require.Equal(t, entry1.Line, entry2.Line, "Line should match for batch %d stream %d entry %d", i, j, k)
				require.Equal(t, len(entry1.StructuredMetadata), len(entry2.StructuredMetadata), "StructuredMetadata length should match for batch %d stream %d entry %d", i, j, k)

				// Compare structured metadata
				for l := range entry1.StructuredMetadata {
					meta1 := entry1.StructuredMetadata[l]
					meta2 := entry2.StructuredMetadata[l]
					require.Equal(t, meta1.Name, meta2.Name, "Metadata name should match for batch %d stream %d entry %d metadata %d", i, j, k, l)
					require.Equal(t, meta1.Value, meta2.Value, "Metadata value should match for batch %d stream %d entry %d metadata %d values names %s and %s", i, j, k, l, meta1.Name, meta2.Name)
				}
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
		DenseIntervals: []DenseInterval{
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
