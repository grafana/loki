package distributor

import (
	"math/rand"
	"testing"

	"github.com/grafana/dskit/flagext"
	"github.com/stretchr/testify/assert"
)

func TestSegmentTopicWriter_SelectPartition(t *testing.T) {
	cfg := SegmentTopicConfig{}

	writer := &SegmentTopicWriter{
		cfg:  cfg,
		rand: rand.New(rand.NewSource(1)), // Use a real rand with fixed seed for testing
	}

	// Test with empty partitions
	selected := writer.selectPartition([]int32{}, "test-key", "test-tenant")
	assert.Equal(t, int32(0), selected)

	// Test with single partition
	selected = writer.selectPartition([]int32{2}, "test-key", "test-tenant")
	assert.Equal(t, int32(2), selected)

	// Test with multiple partitions (random)
	availablePartitions := []int32{1, 2, 3}
	selected = writer.selectPartition(availablePartitions, "test-key", "test-tenant")
	assert.Contains(t, availablePartitions, selected)

	// Test that random selection works (we can't predict the exact sequence)
	// but we can verify that all selections are valid partitions
	for i := 0; i < 10; i++ {
		selected = writer.selectPartition(availablePartitions, "test-key", "test-tenant")
		assert.Contains(t, availablePartitions, selected)
	}
}

func TestSegmentTopicWriter_ConfigurationValidation(t *testing.T) {
	tests := []struct {
		name        string
		cfg         SegmentTopicConfig
		expectError bool
	}{
		{
			name: "valid config",
			cfg: SegmentTopicConfig{
				Enabled:            true,
				TopicName:          "test",
				MaxBufferedBytes:   flagext.Bytes(1024),
				MaxRecordSizeBytes: flagext.Bytes(1024),
			},
			expectError: false,
		},
		{
			name: "disabled config should not validate",
			cfg: SegmentTopicConfig{
				Enabled: false,
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
