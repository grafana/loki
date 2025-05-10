package clientv2

import (
	"context"
	"testing"

	"github.com/grafana/dskit/flagext"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kfake"

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
