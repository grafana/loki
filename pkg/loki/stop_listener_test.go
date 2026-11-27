package loki

import (
	"errors"
	"testing"
	"time"

	"github.com/grafana/dskit/services"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
)

func Test_stopListener(t *testing.T) {
	t.Run("stops the resource once when the service shuts down", func(t *testing.T) {
		var stops atomic.Int32
		svc := services.NewIdleService(nil, nil)
		svc.AddListener(newStopListener(func() { stops.Inc() }))

		require.NoError(t, services.StartAndAwaitRunning(t.Context(), svc))
		require.NoError(t, services.StopAndAwaitTerminated(t.Context(), svc))

		require.Eventually(t, func() bool { return stops.Load() == 1 }, time.Second, time.Millisecond)
		require.Never(t, func() bool { return stops.Load() > 1 }, 100*time.Millisecond, 10*time.Millisecond)
	})

	t.Run("stops the resource when the service fails", func(t *testing.T) {
		var stops atomic.Int32
		l := newStopListener(func() { stops.Inc() })

		l.Failed(services.Running, errors.New("boom"))

		require.Equal(t, int32(1), stops.Load())
	})

	t.Run("does not stop the resource while the service starts or runs", func(t *testing.T) {
		var stops atomic.Int32
		l := newStopListener(func() { stops.Inc() })

		l.Starting()
		l.Running()

		require.Zero(t, stops.Load())
	})

	t.Run("does not stop the resource again on later transitions", func(t *testing.T) {
		var stops atomic.Int32
		l := newStopListener(func() { stops.Inc() })

		l.Stopping(services.Running)
		l.Terminated(services.Stopping)
		l.Failed(services.Stopping, errors.New("boom"))

		require.Equal(t, int32(1), stops.Load())
	})
}
