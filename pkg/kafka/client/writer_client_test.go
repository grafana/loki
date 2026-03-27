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
			name: "valid config",
			config: kafka.Config{
				Topic:        "abcd",
				SASLUsername: "user",
				SASLPassword: flagext.SecretWithValue("password"),
				WriterConfig: kafka.ClientConfig{
					Address:  addr,
					ClientID: "writer",
				},
				WriteTimeout: time.Second,
			},
			wantErr: false,
		},
		{
			name: "wrong password",
			config: kafka.Config{
				WriterConfig: kafka.ClientConfig{
					Address:  addr,
					ClientID: "writer",
				},
				Topic:        "abcd",
				WriteTimeout: time.Second,
				SASLUsername: "user",
				SASLPassword: flagext.SecretWithValue("wrong wrong wrong"),
			},
			wantErr: true,
		},
		{
			name: "wrong username",
			config: kafka.Config{
				WriterConfig: kafka.ClientConfig{
					Address:  addr,
					ClientID: "writer",
				},
				Topic:        "abcd",
				WriteTimeout: time.Second,
				SASLUsername: "wrong wrong wrong",
				SASLPassword: flagext.SecretWithValue("password"),
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
