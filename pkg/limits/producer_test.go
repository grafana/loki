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

func TestProducer_Produce_Partitioning(t *testing.T) {
	kafka := mockKafka{}
	p := newProducer(&kafka, "topic", 8, "zone1", log.NewNopLogger(), prometheus.NewRegistry())

	// Test multiple streams with different hashes to verify partitioning
	testCases := []struct {
		streamHash        uint64
		expectedPartition int32
		tenant            string
		totalSize         uint64
	}{
		{0x0, 0, "tenant1", 100},   // 0 % 8 = 0
		{0x1, 1, "tenant2", 200},   // 1 % 8 = 1
		{0x7, 7, "tenant3", 300},   // 7 % 8 = 7
		{0x8, 0, "tenant4", 400},   // 8 % 8 = 0 (wraps around)
		{0x123, 3, "tenant5", 500}, // 0x123 % 8 = 3 (covers the original RateData test case)
	}

	for _, tc := range testCases {
		metadata := &proto.StreamMetadata{
			StreamHash: tc.streamHash,
			TotalSize:  tc.totalSize,
		}
		require.NoError(t, p.Produce(context.Background(), tc.tenant, metadata))
	}

	require.Len(t, kafka.produced, len(testCases))
	for i, tc := range testCases {
		record := kafka.produced[i]

		// Check partitioning
		require.Equal(t, tc.expectedPartition, record.Partition,
			"Stream hash 0x%x should map to partition %d", tc.streamHash, tc.expectedPartition)

		// Verify the record content
		require.Equal(t, "topic", record.Topic)
		require.Equal(t, []byte(tc.tenant), record.Key)

		// Unmarshal and verify the StreamMetadataRecord
		var recordData proto.StreamMetadataRecord
		err := recordData.Unmarshal(record.Value)
		require.NoError(t, err)
		require.Equal(t, "zone1", recordData.Zone)
		require.Equal(t, tc.tenant, recordData.Tenant)
		require.Equal(t, tc.streamHash, recordData.Metadata.StreamHash)
		require.Equal(t, tc.totalSize, recordData.Metadata.TotalSize)
	}
}
