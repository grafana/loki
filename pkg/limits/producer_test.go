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
	kafkaClient := mockKafka{produced: make(chan *kgo.Record, 1)}
	p := newProducer(&kafkaClient, "topic", 1, "zone1", log.NewNopLogger(), prometheus.NewRegistry())
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
	expectedRecord := &kgo.Record{
		Topic: "topic",
		Key:   []byte("tenant"),
		Value: b,
	}
	require.Equal(t, expectedRecord, <-kafkaClient.produced)
	// Record should fail to be produced.
	kafkaClient.produceFailer = func(_ *kgo.Record) error {
		return errors.New("failed to produce record")
	}
	require.NoError(t, p.Produce(ctx, "tenant", metadata))
	require.Equal(t, int64(1), kafkaClient.producedFailed.Load())
}
