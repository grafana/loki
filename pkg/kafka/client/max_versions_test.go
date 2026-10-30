package client

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/testkafka"
)

// newWarpStreamLikeCluster returns a fake Kafka cluster that closes the connection on any ApiVersions
// request above v4, like WarpStream agents that don't support ApiVersions v5 (KIP-1242). A real Kafka
// broker replies UNSUPPORTED_VERSION instead, which franz-go handles by retrying at a lower version.
func newWarpStreamLikeCluster(t *testing.T) (kafka.Config, *atomic.Int32) {
	cluster, cfg := testkafka.CreateCluster(t, 1, "test-topic")

	rejected := atomic.NewInt32(0)
	cluster.ControlKey(kmsg.ApiVersions.Int16(), func(req kmsg.Request) (kmsg.Response, error, bool) {
		cluster.KeepControl()
		if req.GetVersion() > 4 {
			rejected.Inc()
			return nil, errors.New("unsupported ApiVersions version"), true
		}
		return nil, nil, false
	})
	return cfg, rejected
}

func TestClientsConnectWithoutApiVersionsV5(t *testing.T) {
	t.Run("writer", func(t *testing.T) {
		cfg, rejected := newWarpStreamLikeCluster(t)

		client, err := NewWriterClient("test", cfg, log.NewNopLogger(), prometheus.NewRegistry())
		require.NoError(t, err)
		t.Cleanup(client.Close)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, client.Ping(ctx))
		require.Zero(t, rejected.Load())
	})

	t.Run("reader", func(t *testing.T) {
		cfg, rejected := newWarpStreamLikeCluster(t)

		client, err := NewReaderClient("test", cfg, log.NewNopLogger(), prometheus.NewRegistry())
		require.NoError(t, err)
		t.Cleanup(client.Close)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, client.Ping(ctx))
		require.Zero(t, rejected.Load())
	})

	// Sanity check that the fake cluster reproduces the failure: a client with franz-go's default
	// versions sends ApiVersions v5 and never connects.
	t.Run("default versions fail", func(t *testing.T) {
		cfg, rejected := newWarpStreamLikeCluster(t)

		client, err := kgo.NewClient(kgo.SeedBrokers(cfg.WriterConfig.Address))
		require.NoError(t, err)
		t.Cleanup(client.Close)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.Error(t, client.Ping(ctx))
		require.NotZero(t, rejected.Load())
	})
}
