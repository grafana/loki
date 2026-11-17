package ring

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/kv/consul"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

const testInstanceID = "instance-1"

func newTestServerRingManager(t *testing.T) *RingManager {
	t.Helper()
	kvStore, closer := consul.NewInMemoryClient(ring.GetCodec(), log.NewNopLogger(), nil)
	t.Cleanup(func() { _ = closer.Close() })

	cfg := RingConfig{
		KVStore:          kv.Config{Mock: kvStore},
		HeartbeatPeriod:  100 * time.Millisecond,
		HeartbeatTimeout: time.Minute,
		InstanceID:       testInstanceID,
		InstanceAddr:     "127.0.0.1",
		InstancePort:     9095,
	}
	rm, err := NewRingManager("test", ServerMode, cfg, 1, 8, log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)
	return rm
}

func instanceState(t *testing.T, rm *RingManager) ring.InstanceState {
	t.Helper()
	desc, err := rm.Ring.GetInstance(testInstanceID)
	if err != nil {
		return ring.PENDING
	}
	return desc.State
}

func TestRingManager_NoBeforeActiveGoesActive(t *testing.T) {
	rm := newTestServerRingManager(t)
	require.NoError(t, services.StartAndAwaitRunning(context.Background(), rm))
	t.Cleanup(func() { _ = services.StopAndAwaitTerminated(context.Background(), rm) })

	require.Equal(t, ring.ACTIVE, instanceState(t, rm))
}

func TestRingManager_BeforeActiveHoldsJoining(t *testing.T) {
	rm := newTestServerRingManager(t)

	entered := make(chan struct{})
	release := make(chan struct{})
	rm.SetBeforeActive(func(ctx context.Context) error {
		close(entered)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})

	require.NoError(t, rm.StartAsync(context.Background()))
	t.Cleanup(func() { _ = services.StopAndAwaitTerminated(context.Background(), rm) })

	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("before-active hook was not called")
	}

	// While the hook runs, the instance stays JOINING and the service is not running.
	for range 5 {
		require.Equal(t, ring.JOINING, instanceState(t, rm))
		require.Equal(t, services.Starting, rm.State())
		time.Sleep(100 * time.Millisecond)
	}

	close(release)
	require.NoError(t, rm.AwaitRunning(context.Background()))
	require.Equal(t, ring.ACTIVE, instanceState(t, rm))
}

func TestRingManager_BeforeActiveErrorFailsStart(t *testing.T) {
	rm := newTestServerRingManager(t)
	rm.SetBeforeActive(func(context.Context) error { return errors.New("preload failed") })

	err := services.StartAndAwaitRunning(context.Background(), rm)
	require.ErrorContains(t, err, "preload failed")
	require.Equal(t, services.Failed, rm.State())
}
