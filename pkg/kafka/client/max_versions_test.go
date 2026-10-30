package client

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
	"github.com/twmb/franz-go/pkg/kversion"

	"github.com/grafana/loki/v3/pkg/kafka/testkafka"
)

func TestMaxVersions(t *testing.T) {
	versions := MaxVersions()

	got, ok := versions.LookupMaxKeyVersion(kmsg.ApiVersions.Int16())
	require.True(t, ok)
	require.Equal(t, int16(maxAPIVersionsRequestVersion), got)

	// Every other request keeps franz-go's default maximum version.
	stable := kversion.Stable()
	for _, key := range []kmsg.Key{kmsg.Produce, kmsg.Fetch, kmsg.Metadata, kmsg.ListOffsets, kmsg.JoinGroup, kmsg.OffsetCommit} {
		want, wantOK := stable.LookupMaxKeyVersion(key.Int16())
		got, gotOK := versions.LookupMaxKeyVersion(key.Int16())
		require.Equal(t, wantOK, gotOK, key.Name())
		require.Equal(t, want, got, key.Name())
	}
}

func TestClientsConnectWithoutApiVersionsV5(t *testing.T) {
	t.Run("writer", func(t *testing.T) {
		cluster, cfg := testkafka.CreateCluster(t, 1, "test-topic")
		rejected := testkafka.CloseOnAPIVersionsAbove(cluster, 4)

		client, err := NewWriterClient("test", cfg, log.NewNopLogger(), prometheus.NewRegistry())
		require.NoError(t, err)
		t.Cleanup(client.Close)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, client.Ping(ctx))
		require.Zero(t, rejected.Load())
	})

	t.Run("reader", func(t *testing.T) {
		cluster, cfg := testkafka.CreateCluster(t, 1, "test-topic")
		rejected := testkafka.CloseOnAPIVersionsAbove(cluster, 4)

		client, err := NewReaderClient("test", cfg, log.NewNopLogger(), prometheus.NewRegistry())
		require.NoError(t, err)
		t.Cleanup(client.Close)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, client.Ping(ctx))
		require.Zero(t, rejected.Load())
	})

	// Sanity check that the fake cluster reproduces the failure: a client with franz-go's default
	// versions sends ApiVersions v5 and never connects. If a future franz-go release falls back to a
	// lower version when the connection is closed, this subtest fails and the cap may no longer be needed.
	t.Run("default versions fail", func(t *testing.T) {
		cluster, cfg := testkafka.CreateCluster(t, 1, "test-topic")
		rejected := testkafka.CloseOnAPIVersionsAbove(cluster, 4)

		client, err := kgo.NewClient(kgo.SeedBrokers(cfg.WriterConfig.Address))
		require.NoError(t, err)
		t.Cleanup(client.Close)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.Error(t, client.Ping(ctx))
		require.NotZero(t, rejected.Load())
	})
}
