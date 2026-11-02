package limits

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/limits/proto"
)

// mockKafkaCloser is a [mockKafka] that implements Close.
type mockKafkaCloser struct {
	mockKafka
	closed bool
}

func (m *mockKafkaCloser) Close() {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	m.closed = true
}

func (m *mockKafkaCloser) getProduced() []*kgo.Record {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	return append([]*kgo.Record(nil), m.produced...)
}

func TestStreamRemovalProducer_StreamRemoved(t *testing.T) {
	kafka := &mockKafkaCloser{}
	cfg := StreamRemovalProducerConfig{Enabled: true, BufferSize: 10, MaxRecordsPerSecond: 1000}
	p := NewStreamRemovalProducer(cfg, kafka, "topic", 4, log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, services.StartAndAwaitRunning(context.Background(), p))

	lastPushAt := time.Unix(100, 0)
	p.StreamRemoved("tenant", 0x7, lastPushAt)
	require.Eventually(t, func() bool {
		return len(kafka.getProduced()) == 1
	}, time.Second, 10*time.Millisecond)

	r := kafka.getProduced()[0]
	require.Equal(t, "topic", r.Topic)
	require.Equal(t, []byte("tenant"), r.Key)
	// The record must go to the same partition as the stream's other records.
	require.Equal(t, int32(0x7%4), r.Partition)
	var rec proto.StreamMetadataRecord
	require.NoError(t, rec.Unmarshal(r.Value))
	require.Equal(t, proto.StreamMetadataRecord{
		Zone:              removalRecordZone,
		Tenant:            "tenant",
		Metadata:          &proto.StreamMetadata{StreamHash: 0x7},
		RemovedLastSeenAt: lastPushAt.UnixNano(),
	}, rec)

	require.NoError(t, services.StopAndAwaitTerminated(context.Background(), p))
	require.True(t, kafka.closed)
}

func TestStreamRemovalProducer_DropsWhenFull(t *testing.T) {
	kafka := &mockKafkaCloser{}
	cfg := StreamRemovalProducerConfig{Enabled: true, BufferSize: 2, MaxRecordsPerSecond: 1000}
	p := NewStreamRemovalProducer(cfg, kafka, "topic", 1, log.NewNopLogger(), prometheus.NewRegistry())
	// The producer is not started, so nothing drains the buffer.
	for i := 0; i < 5; i++ {
		p.StreamRemoved("tenant", uint64(i), time.Now())
	}
	require.Equal(t, float64(2), testutil.ToFloat64(p.enqueued))
	require.Equal(t, float64(3), testutil.ToFloat64(p.dropped))
}

func TestStreamRemovalProducer_StoppingDrainsBuffer(t *testing.T) {
	kafka := &mockKafkaCloser{}
	// A rate limit this low means that, while running, at most the burst of
	// one record is written before the producer is stopped.
	cfg := StreamRemovalProducerConfig{Enabled: true, BufferSize: 10, MaxRecordsPerSecond: 0.001}
	p := NewStreamRemovalProducer(cfg, kafka, "topic", 1, log.NewNopLogger(), prometheus.NewRegistry())
	for i := 0; i < 5; i++ {
		p.StreamRemoved("tenant", uint64(i), time.Now())
	}
	require.NoError(t, services.StartAndAwaitRunning(context.Background(), p))
	require.NoError(t, services.StopAndAwaitTerminated(context.Background(), p))
	require.Len(t, kafka.getProduced(), 5)
	require.True(t, kafka.closed)
}
