package bench

import (
	"os"
	"testing"
	"time"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/stretchr/testify/require"
)

func TestGenerateDatasetDeterminism(t *testing.T) {
	// Create two temporary files for the datasets
	file1 := t.TempDir() + "/dataset1.pb"
	file2 := t.TempDir() + "/dataset2.pb"

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

	// Generate two datasets with the same configuration
	size := int64(1024 * 1024) // 1MB for testing
	err := GenerateDatasetWithConfig(size, file1, cfg)
	require.NoError(t, err)
	err = GenerateDatasetWithConfig(size, file2, cfg)
	require.NoError(t, err)

	// Read both files
	data1, err := os.ReadFile(file1)
	require.NoError(t, err)
	data2, err := os.ReadFile(file2)
	require.NoError(t, err)

	// Unmarshal protobuf data
	req1 := &logproto.PushRequest{}
	req2 := &logproto.PushRequest{}
	err = req1.Unmarshal(data1)
	require.NoError(t, err)
	err = req2.Unmarshal(data2)
	require.NoError(t, err)

	// Compare number of streams
	require.Equal(t, len(req1.Streams), len(req2.Streams), "Number of streams should match")

	// Compare each stream
	for i := range req1.Streams {
		stream1 := req1.Streams[i]
		stream2 := req2.Streams[i]

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

func BenchmarkLogQL(b *testing.B) {
	// builder := newTestDataBuilder(b, "test-tenant")
}
