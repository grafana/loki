package client

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/testkafka"
	"github.com/grafana/loki/v3/pkg/validation"
)

func TestNewWriterClient(t *testing.T) {
	_, addr := testkafka.CreateClusterWithoutCustomConsumerGroupsSupport(t, 1, "test", kfake.EnableSASL(), kfake.Superuser("PLAIN", "user", "password"))

	tests := []struct {
		name    string
		config  kafka.Config
		wantErr bool
	}{
		{
			name: "PLAIN: valid credentials",
			config: kafka.Config{
				Topic:         "abcd",
				SASLUsername:  "user",
				SASLPassword:  flagext.SecretWithValue("password"),
				SASLMechanism: kafka.SASLMechanismPlain,
				WriterConfig:  kafka.ClientConfig{Address: addr, ClientID: "writer"},
				WriteTimeout:  time.Second,
			},
			wantErr: false,
		},
		{
			name: "PLAIN: wrong password",
			config: kafka.Config{
				Topic:         "abcd",
				SASLUsername:  "user",
				SASLPassword:  flagext.SecretWithValue("wrong wrong wrong"),
				SASLMechanism: kafka.SASLMechanismPlain,
				WriterConfig:  kafka.ClientConfig{Address: addr, ClientID: "writer"},
				WriteTimeout:  time.Second,
			},
			wantErr: true,
		},
		{
			name: "PLAIN: wrong username",
			config: kafka.Config{
				Topic:         "abcd",
				SASLUsername:  "wrong wrong wrong",
				SASLPassword:  flagext.SecretWithValue("password"),
				SASLMechanism: kafka.SASLMechanismPlain,
				WriterConfig:  kafka.ClientConfig{Address: addr, ClientID: "writer"},
				WriteTimeout:  time.Second,
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewWriterClient("test-client", tt.config, 10, log.NewNopLogger(), prometheus.NewRegistry())
			require.NoError(t, err)

			err = client.Ping(context.Background())
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestNewWriterClientSCRAMAuthentication(t *testing.T) {
	tests := []struct {
		name           string
		mechanism      string
		kfakeMechanism string
		wantErr        bool
	}{
		{
			name:           "SCRAM-SHA-256: valid credentials",
			mechanism:      kafka.SASLMechanismScramSHA256,
			kfakeMechanism: "SCRAM-SHA-256",
			wantErr:        false,
		},
		{
			name:           "SCRAM-SHA-512: valid credentials",
			mechanism:      kafka.SASLMechanismScramSHA512,
			kfakeMechanism: "SCRAM-SHA-512",
			wantErr:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, addr := testkafka.CreateClusterWithoutCustomConsumerGroupsSupport(
				t, 1, "test",
				kfake.EnableSASL(),
				kfake.Superuser(tt.kfakeMechanism, "user", "password"),
			)

			cfg := kafka.Config{
				Topic:         "abcd",
				SASLUsername:  "user",
				SASLPassword:  flagext.SecretWithValue("password"),
				SASLMechanism: tt.mechanism,
				WriterConfig:  kafka.ClientConfig{Address: addr, ClientID: "writer"},
				WriteTimeout:  time.Second,
			}

			client, err := NewWriterClient("test-client", cfg, 10, log.NewNopLogger(), prometheus.NewRegistry())
			require.NoError(t, err)
			t.Cleanup(client.Close)

			err = client.Ping(context.Background())
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}

}

func TestProducer(t *testing.T) {
	t.Run("on context canceled", func(t *testing.T) {
		_, kafkaCfg := testkafka.CreateCluster(t, 1, "test-topic")
		client, err := NewWriterClient("test-client", kafkaCfg, 100, log.NewNopLogger(), prometheus.NewRegistry())
		require.NoError(t, err)

		producer := NewProducer("test-producer", client, 1024*1024, prometheus.NewRegistry())

		// Force a canceled context.
		cancelCtx, cancel := context.WithCancel(t.Context())
		cancel()
		rec1 := &kgo.Record{Key: []byte("key1"), Value: []byte("value1")}
		rec2 := &kgo.Record{Key: []byte("key2"), Value: []byte("value2")}
		results := producer.ProduceSync(cancelCtx, []*kgo.Record{rec1, rec2})
		require.Len(t, results, 2)

		// Each result should contain a "context canceled" error.
		require.Equal(t, rec1, results[0].Record)
		require.EqualError(t, results[0].Err, "context canceled")
		require.Equal(t, rec2, results[1].Record)
		require.EqualError(t, results[1].Err, "context canceled")
	})
}

func TestProducerWithInterceptor(t *testing.T) {
	_, kafkaCfg := testkafka.CreateCluster(t, 1, "test-topic")

	client, err := NewWriterClient("test-client", kafkaCfg, 100, log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)

	producer := NewProducer("test-producer", client, 1024*1024, prometheus.NewRegistry(),
		WithRecordsInterceptor(validation.IngestionPoliciesKafkaProducerInterceptor))

	t.Run("with policy in context", func(t *testing.T) {
		// Create context with ingestion policy
		ctx := validation.InjectIngestionPolicyContext(t.Context(), "test-policy")

		// Create test records
		records := []*kgo.Record{
			{Value: []byte("test-value-1"), Partition: 0},
			{Value: []byte("test-value-2"), Partition: 0},
		}

		results := producer.ProduceSync(ctx, records)
		require.NoError(t, results.FirstErr())

		// Verify interceptor added the ingestion policy header to all records
		for _, record := range records {
			require.Len(t, record.Headers, 1)
			require.Equal(t, "x-loki-ingestion-policy", record.Headers[0].Key)
			require.Equal(t, []byte("test-policy"), record.Headers[0].Value)
		}
	})

	t.Run("without policy in context", func(t *testing.T) {
		records := []*kgo.Record{
			{Value: []byte("test-value-1"), Partition: 0},
		}

		results := producer.ProduceSync(t.Context(), records)
		require.NoError(t, results.FirstErr())

		// Verify no headers were added
		require.Len(t, records[0].Headers, 0)
	})
}
