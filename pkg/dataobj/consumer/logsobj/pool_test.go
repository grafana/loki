package logsobj

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/scratch"
)

func TestSizedBuilderPool(t *testing.T) {
	t.Run("Get returns the next builder, and nil when the pool is empty", func(t *testing.T) {
		builder, err := NewBuilder(testBuilderConfig, scratch.NewMemory())
		require.NoError(t, err)
		pool := NewSizedBuilderPool([]*Builder{builder})
		// The first call to [Get] should return the builder.
		require.NotNil(t, pool.Get())
		// The second call to [Get] should return nil as the pool has a fixed size
		// of 1.
		require.Nil(t, pool.Get())
		// If we put the builder back in the pool, we should be able to fetch it
		// once more.
		pool.Put(builder)
		require.NotNil(t, pool.Get())
		require.Nil(t, pool.Get())
	})

	t.Run("Wait blocks until a builder is available", func(t *testing.T) {
		builder, err := NewBuilder(testBuilderConfig, scratch.NewMemory())
		require.NoError(t, err)
		pool := NewSizedBuilderPool([]*Builder{builder})
		// The first call to [Wait] should not block.
		timedCtx, cancel := context.WithTimeout(t.Context(), time.Second)
		t.Cleanup(cancel)
		b, err := pool.Wait(timedCtx)
		require.NoError(t, err)
		require.NotNil(t, b)
		// The second call to [Wait] should timeout.
		b, err = pool.Wait(timedCtx)
		require.EqualError(t, err, "context deadline exceeded")
		require.Nil(t, b)
		// Put the builder back in the pool.
		pool.Put(builder)
		timedCtx, cancel = context.WithTimeout(t.Context(), time.Second)
		t.Cleanup(cancel)
		b, err = pool.Wait(timedCtx)
		require.NoError(t, err)
		require.NotNil(t, b)
	})
}
