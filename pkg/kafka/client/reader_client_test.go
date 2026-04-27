package client

import (
	"context"
	"testing"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"

	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/testkafka"
)

func TestNewReaderClient(t *testing.T) {
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
				ReaderConfig:  kafka.ClientConfig{Address: addr, ClientID: "reader"},
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
				ReaderConfig:  kafka.ClientConfig{Address: addr, ClientID: "reader"},
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
				ReaderConfig:  kafka.ClientConfig{Address: addr, ClientID: "reader"},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewReaderClient("test-client", tt.config, log.NewNopLogger(), prometheus.NewRegistry())
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

func TestNewReaderClientSCRAMAuthentication(t *testing.T) {
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
				ReaderConfig:  kafka.ClientConfig{Address: addr, ClientID: "reader"},
			}

			client, err := NewReaderClient("test-client", cfg, log.NewNopLogger(), prometheus.NewRegistry())
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

func TestSetDefaultNumberOfPartitionsForAutocreatedTopics(t *testing.T) {
	cluster, err := kfake.NewCluster(kfake.NumBrokers(1))
	require.NoError(t, err)
	t.Cleanup(cluster.Close)

	addrs := cluster.ListenAddrs()
	require.Len(t, addrs, 1)

	cfg := kafka.Config{
		ReaderConfig: kafka.ClientConfig{
			Address:  addrs[0],
			ClientID: "reader",
		},
		AutoCreateTopicDefaultPartitions: 100,
	}

	cluster.ControlKey(kmsg.AlterConfigs.Int16(), func(request kmsg.Request) (kmsg.Response, error, bool) {
		r := request.(*kmsg.AlterConfigsRequest)

		require.Len(t, r.Resources, 1)
		res := r.Resources[0]
		require.Equal(t, kmsg.ConfigResourceTypeBroker, res.ResourceType)
		require.Len(t, res.Configs, 1)
		cfg := res.Configs[0]
		require.Equal(t, "num.partitions", cfg.Name)
		require.NotNil(t, *cfg.Value)
		require.Equal(t, "100", *cfg.Value)

		return &kmsg.AlterConfigsResponse{}, nil, true
	})

	client, err := kgo.NewClient(commonKafkaClientOptions(cfg, nil, log.NewNopLogger())...)
	require.NoError(t, err)

	setDefaultNumberOfPartitionsForAutocreatedTopics(cfg, client, log.NewNopLogger())
}
