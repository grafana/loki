package builder

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kmsg"
)

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
