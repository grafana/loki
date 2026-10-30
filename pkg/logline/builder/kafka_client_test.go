package builder

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/kafka/testkafka"
)

// TestCreateKafkaClient_ConnectsWithoutApiVersionsV5 verifies the builder's own Kafka client caps the
// ApiVersions request version, so it can connect to backends like WarpStream that close the connection
// on ApiVersions v5.
func TestCreateKafkaClient_ConnectsWithoutApiVersionsV5(t *testing.T) {
	cluster, cfg := setupKafkaTest(t)
	rejected := testkafka.CloseOnAPIVersionsAbove(cluster, 4)

	_, indexStore := newTestStore(t)
	svc, err := New(indexStore, cfg, "2026-01-01", newDefaultFakePartitionRing(), log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)
	require.NoError(t, svc.initKafkaClient())
	t.Cleanup(svc.client.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, svc.client.Ping(ctx))
	require.Zero(t, rejected.Load())
}
