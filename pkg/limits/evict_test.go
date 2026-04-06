package limits

import (
	"context"
	"testing"
	"time"

	"github.com/coder/quartz"
	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
)

type mockEvictable struct {
	calls []time.Time
	clock quartz.Clock
}

func (m *mockEvictable) evict(_ context.Context) error {
	m.calls = append(m.calls, m.clock.Now())
	return nil
}

func TestEvictor(t *testing.T) {
	// Set a timeout on the test.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	clock := quartz.NewMock(t)
	m := mockEvictable{clock: clock}
	e, err := newEvictor(ctx, time.Second, &m, log.NewNopLogger())
	require.NoError(t, err)
	e.clock = clock

	// See https://github.com/coder/quartz/blob/v0.1.3/example_test.go#L48.
	trap := clock.Trap().TickerFunc()
	defer trap.Close()
	go e.Run() //nolint:errcheck
	call, err := trap.Wait(ctx)
	require.NoError(t, err)
	err = call.Release(ctx)
	require.NoError(t, err)

	// Do a tick.
	clock.Advance(time.Second).MustWait(ctx)
	tick1 := clock.Now()
	require.Len(t, m.calls, 1)
	require.Equal(t, tick1, m.calls[0])

	// Do another tick.
	clock.Advance(time.Second).MustWait(ctx)
	tick2 := clock.Now()
	require.Len(t, m.calls, 2)
	require.Equal(t, tick1, m.calls[0])
	require.Equal(t, tick2, m.calls[1])
}
