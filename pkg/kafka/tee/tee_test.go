package tee

import (
	"os"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/ring"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/distributor"
	"github.com/grafana/loki/v3/pkg/kafka/testkafka"

	"github.com/grafana/loki/pkg/push"
)

func TestPushKafkaRecords(t *testing.T) {
	_, cfg := testkafka.CreateCluster(t, 1, "topic")
	tee, err := NewTee(cfg, "test", prometheus.NewRegistry(), log.NewLogfmtLogger(os.Stdout), newTestPartitionRing())
	require.NoError(t, err)

	err = tee.sendStream("test", distributor.KeyedStream{
		HashKey: 1,
		Stream: push.Stream{
			Labels: `{foo="bar"}`,
			Entries: []push.Entry{
				{Timestamp: time.Now(), Line: "test"},
			},
		},
	})
	require.NoError(t, err)
}

type testPartitionRing struct {
	partitionRing *ring.PartitionRing
}

func (t *testPartitionRing) PartitionRing() *ring.PartitionRing {
	return t.partitionRing
}

func newTestPartitionRing() ring.PartitionRingReader {
	desc := ring.NewPartitionRingDesc()
	desc.AddPartition(0, ring.PartitionActive, time.Now())
	return &testPartitionRing{
		partitionRing: ring.NewPartitionRing(*desc),
	}
}
