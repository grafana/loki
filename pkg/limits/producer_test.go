package limits

import (
	"context"
	"errors"
	"testing"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/limits/proto"
)

func TestProducer_Produce(t *testing.T) {
	kafka := mockKafka{}
	p := newProducer(&kafka, "topic", 1, "zone1", log.NewNopLogger(), prometheus.NewRegistry())
	// Record should be produced.
	metadata := &proto.StreamMetadata{
		StreamHash: 0x1,
		TotalSize:  100,
	}
	ctx := context.Background()
	require.NoError(t, p.Produce(ctx, "tenant", metadata))
	expectedMetadataRecord := proto.StreamMetadataRecord{
		Zone:     "zone1",
		Tenant:   "tenant",
		Metadata: metadata,
	}
	b, err := expectedMetadataRecord.Marshal()
	require.NoError(t, err)
	expectedRecords := []*kgo.Record{{
		Topic: "topic",
		Key:   []byte("tenant"),
		Value: b,
	}}
	require.Equal(t, expectedRecords, kafka.produced)
	// Record should fail to be produced.
	kafka.produced = []*kgo.Record{}
	kafka.produceFailer = func(_ *kgo.Record) error {
		return errors.New("failed to produce record")
	}
	require.NoError(t, p.Produce(ctx, "tenant", metadata))
	require.Equal(t, []*kgo.Record{}, kafka.produced)
}

func TestProducer_Produce_RateData(t *testing.T) {
	kafka := mockKafka{}
	p := newProducer(&kafka, "topic", 4, "zone1", log.NewNopLogger(), prometheus.NewRegistry())

	// Test that rate data is produced with correct partitioning
	metadata := &proto.StreamMetadata{
		StreamHash: 0x123, // This will determine the partition
		TotalSize:  500,
	}
	ctx := context.Background()
	require.NoError(t, p.Produce(ctx, "tenant1", metadata))

	// Verify the record was produced
	require.Len(t, kafka.produced, 1)
	record := kafka.produced[0]

	// Check partitioning (0x123 % 4 = 3)
	expectedPartition := int32(0x123 % 4)
	require.Equal(t, expectedPartition, record.Partition)

	// Verify the record content
	require.Equal(t, "topic", record.Topic)
	require.Equal(t, []byte("tenant1"), record.Key)

	// Unmarshal and verify the StreamMetadataRecord
	var recordData proto.StreamMetadataRecord
	err := recordData.Unmarshal(record.Value)
	require.NoError(t, err)
	require.Equal(t, "zone1", recordData.Zone)
	require.Equal(t, "tenant1", recordData.Tenant)
	require.Equal(t, uint64(0x123), recordData.Metadata.StreamHash)
	require.Equal(t, uint64(500), recordData.Metadata.TotalSize)
}

func TestProducer_Produce_Partitioning(t *testing.T) {
	kafka := mockKafka{}
	p := newProducer(&kafka, "topic", 8, "zone1", log.NewNopLogger(), prometheus.NewRegistry())

	// Test multiple streams with different hashes to verify partitioning
	testCases := []struct {
		streamHash        uint64
		expectedPartition int32
	}{
		{0x0, 0}, // 0 % 8 = 0
		{0x1, 1}, // 1 % 8 = 1
		{0x7, 7}, // 7 % 8 = 7
		{0x8, 0}, // 8 % 8 = 0 (wraps around)
		{0xF, 7}, // 15 % 8 = 7
	}

	for _, tc := range testCases {
		metadata := &proto.StreamMetadata{
			StreamHash: tc.streamHash,
			TotalSize:  100,
		}
		require.NoError(t, p.Produce(context.Background(), "tenant", metadata))
	}

	require.Len(t, kafka.produced, len(testCases))
	for i, tc := range testCases {
		require.Equal(t, tc.expectedPartition, kafka.produced[i].Partition,
			"Stream hash 0x%x should map to partition %d", tc.streamHash, tc.expectedPartition)
	}
}
