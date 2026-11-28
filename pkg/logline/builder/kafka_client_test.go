package builder

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"

	"github.com/grafana/loki/v3/pkg/kafka"
)

// TestKafkaClientSASLAuthentication verifies authentication with the mechanism
// and credentials inherited from the root Kafka config.
func TestKafkaClientSASLAuthentication(t *testing.T) {
	tests := []struct {
		name      string
		mechanism string
		broker    string
		override  string
	}{
		{name: "default PLAIN", broker: kafka.SASLMechanismPlain},
		{name: "explicit PLAIN", mechanism: kafka.SASLMechanismPlain, broker: kafka.SASLMechanismPlain},
		{name: "SCRAM-SHA-256", mechanism: kafka.SASLMechanismScramSHA256, broker: kafka.SASLMechanismScramSHA256},
		{name: "SCRAM-SHA-512", mechanism: kafka.SASLMechanismScramSHA512, broker: kafka.SASLMechanismScramSHA512},
		{name: "override with SCRAM", mechanism: kafka.SASLMechanismScramSHA512, override: kafka.SASLMechanismScramSHA256, broker: kafka.SASLMechanismScramSHA256},
		{name: "override with PLAIN", mechanism: kafka.SASLMechanismScramSHA256, override: kafka.SASLMechanismPlain, broker: kafka.SASLMechanismPlain},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cluster, err := kfake.NewCluster(
				kfake.NumBrokers(1),
				kfake.SeedTopics(1, testTopic),
				kfake.EnableSASL(),
				kfake.Superuser(tt.broker, "user", "password"),
			)
			require.NoError(t, err)
			t.Cleanup(cluster.Close)

			cfg := Config{disableStaticMembership: true, ScratchDir: t.TempDir()}
			cfg.Kafka.SASLMechanism = tt.override
			cfg.Kafka.ApplyDefaultsFrom(kafka.Config{
				ReaderConfig:  kafka.ClientConfig{Address: cluster.ListenAddrs()[0]},
				Topic:         testTopic,
				SASLUsername:  "user",
				SASLPassword:  flagext.SecretWithValue("password"),
				SASLMechanism: tt.mechanism,
			})
			_, indexStore := newTestStore(t)
			svc, err := New(indexStore, cfg, "2026-01-01", newDefaultFakePartitionRing(), log.NewNopLogger(), prometheus.NewRegistry())
			require.NoError(t, err)
			client, err := svc.createKafkaClient()
			require.NoError(t, err)
			t.Cleanup(client.Close)

			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			require.NoError(t, client.Ping(ctx))
		})
	}
}

// TestKafkaClientCapsAPIVersions checks that the logline builder's Kafka client sends
// ApiVersions at the configured cap.
func TestKafkaClientCapsAPIVersions(t *testing.T) {
	cluster, cfg := setupKafkaTest(t)
	cfg.Kafka.APIVersionsMaxVersion = 3

	var (
		mu       sync.Mutex
		versions []int16
	)
	cluster.ControlKey(kmsg.ApiVersions.Int16(), func(req kmsg.Request) (kmsg.Response, error, bool) {
		cluster.KeepControl()
		mu.Lock()
		versions = append(versions, req.GetVersion())
		mu.Unlock()
		return nil, nil, false
	})

	_, indexStore := newTestStore(t)
	svc, err := New(indexStore, cfg, "2026-01-01", newDefaultFakePartitionRing(), log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)
	require.NoError(t, services.StartAndAwaitRunning(context.Background(), svc.Service))
	t.Cleanup(func() { _ = services.StopAndAwaitTerminated(context.Background(), svc.Service) })

	// The client connects in the background after the service starts.
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(versions) > 0
	}, 10*time.Second, 50*time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	for _, v := range versions {
		require.Equal(t, int16(3), v)
	}
}

// TestKafkaClientOffsets checks that a new consumer group starts at the
// earliest record, and that a data-loss reset doesn't rewind.
func TestKafkaClientOffsets(t *testing.T) {
	_, cfg := setupKafkaTest(t)

	_, indexStore := newTestStore(t)
	svc, err := New(indexStore, cfg, "2026-01-01", newDefaultFakePartitionRing(), log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)

	client, err := svc.createKafkaClient()
	require.NoError(t, err)
	t.Cleanup(client.Close)

	require.Equal(t, kgo.NewOffset().AtStart(), client.OptValue(kgo.ConsumeStartOffset))
	require.Equal(t, kgo.RewindOffset(0), client.OptValue(kgo.ConsumeResetOffset))
}

// TestKafkaClientMaxDecompressBatchBytes checks that the decompress limit is
// passed to the client, and that 0 keeps the franz-go default.
func TestKafkaClientMaxDecompressBatchBytes(t *testing.T) {
	tests := []struct {
		name  string
		value int
		want  int
	}{
		{name: "default", value: kafka.DefaultMaxDecompressBatchBytes, want: kafka.DefaultMaxDecompressBatchBytes},
		{name: "custom", value: 1 << 20, want: 1 << 20},                   // 1 MiB
		{name: "zero uses the franz-go default", value: 0, want: 1 << 30}, // 1 GiB
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, cfg := setupKafkaTest(t)
			cfg.Kafka.MaxDecompressBatchBytes = tt.value

			_, indexStore := newTestStore(t)
			svc, err := New(indexStore, cfg, "2026-01-01", newDefaultFakePartitionRing(), log.NewNopLogger(), prometheus.NewRegistry())
			require.NoError(t, err)

			client, err := svc.createKafkaClient()
			require.NoError(t, err)
			t.Cleanup(client.Close)

			require.Equal(t, tt.want, client.OptValue(kgo.MaxDecompressBatchBytes))
		})
	}
}
