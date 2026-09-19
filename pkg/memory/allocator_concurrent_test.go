package memory

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAllocator_lock(t *testing.T) {
	var alloc Allocator

	t.Run("can lock and unlock without panic", func(t *testing.T) {
		require.NotPanics(t, func() {
			alloc.lock()
			alloc.unlock()
		})
	})

	t.Run("cannot double-lock", func(t *testing.T) {
		require.PanicsWithValue(t, errConcurrentUse, func() {
			alloc.lock()
			alloc.lock()
		})
	})
}
