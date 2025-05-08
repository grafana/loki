package clientv2

import (
	"context"
	"testing"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"

	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/testkafka"
)

func TestNewConsumer(t *testing.T) {
	_, addr := testkafka.CreateClusterWithoutCustomConsumerGroupsSupport(
		t, 1, "test", kfake.EnableSASL(), kfake.Superuser("PLAIN", "user", "password"))

	tests := []struct {
		name        string
		config      kafka.Config
		expectedErr string
	}{{
		name: "ok",
		config: kafka.Config{
			Address:      addr,
			Topic:        "abcd",
			SASLUsername: "user",
			SASLPassword: flagext.SecretWithValue("password"),
		},
	}, {
		name: "invalid username",
		config: kafka.Config{
			Address:      addr,
			Topic:        "abcd",
			SASLUsername: "invalid_user",
			SASLPassword: flagext.SecretWithValue("password"),
		},
		expectedErr: "EOF",
	}, {
		name: "invalid password",
		config: kafka.Config{
			Address:      addr,
			Topic:        "abcd",
			SASLUsername: "user",
			SASLPassword: flagext.SecretWithValue("invalid_password"),
		},
		expectedErr: "EOF",
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, err := New(test.config, nil, nil, NewConsumerOpts()...)
			require.NoError(t, err)
			err = client.Ping(context.Background())
			if test.expectedErr != "" {
				require.EqualError(t, err, test.expectedErr)
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
		Address:                          addrs[0],
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

	client, err := kgo.NewClient(newCommonOpts(cfg, nil, log.NewNopLogger())...)
	require.NoError(t, err)

	setDefaultNumberOfPartitionsForAutocreatedTopics(cfg, client, log.NewNopLogger())
}
