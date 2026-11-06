package client

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"

	"github.com/grafana/loki/v3/pkg/kafka/testkafka"
)

// TestClientCapsApiVersionsAtV4 is a regression test for
// https://github.com/grafana/loki/issues/24790.
//
// franz-go >= v1.22 negotiates each connection with an ApiVersions v5 request (KIP-1242).
// WarpStream (and some other Kafka-protocol-compatible backends) close the connection instead of
// replying UNSUPPORTED_VERSION, which breaks the producer and consumer paths. commonKafkaClientOptions
// caps the ApiVersions request at v4 so Loki stays compatible; this test asserts the clients actually
// negotiate at v4.
func TestClientCapsApiVersionsAtV4(t *testing.T) {
	cluster, cfg := testkafka.CreateCluster(t, 1, "test")

	var maxSeenAPIVersion atomic.Int32
	cluster.ControlKey(kmsg.ApiVersions.Int16(), func(req kmsg.Request) (kmsg.Response, error, bool) {
		if v := int32(req.GetVersion()); v > maxSeenAPIVersion.Load() {
			maxSeenAPIVersion.Store(v)
		}
		// Return (nil, nil, false) so kfake handles the request normally; we only observe it.
		return nil, nil, false
	})

	for _, tc := range []struct {
		name    string
		newClnt func() (*kgo.Client, error)
	}{
		{
			name: "reader",
			newClnt: func() (*kgo.Client, error) {
				return NewReaderClient("test", cfg, log.NewNopLogger(), prometheus.NewRegistry())
			},
		},
		{
			name: "writer",
			newClnt: func() (*kgo.Client, error) {
				return NewWriterClient("test", cfg, log.NewNopLogger(), prometheus.NewRegistry())
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			maxSeenAPIVersion.Store(0)

			client, err := tc.newClnt()
			require.NoError(t, err)
			defer client.Close()

			// Ping forces a broker connection, which triggers ApiVersions negotiation.
			require.NoError(t, client.Ping(context.Background()))

			require.Equal(t, int32(4), maxSeenAPIVersion.Load(),
				"the shared Kafka client options must cap the ApiVersions request at v4 for WarpStream compatibility (issue #24790)")
		})
	}
}
