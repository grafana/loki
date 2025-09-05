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

	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/testkafka"
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
