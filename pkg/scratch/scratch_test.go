package scratch_test

import (
	"io"
	"math"
	"testing"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/scratch"
)

func TestStore(t *testing.T) {
	t.Run("impl=Memory", func(t *testing.T) {
		testStore(t, func() scratch.Store { return scratch.NewMemory() })
	})

	t.Run("impl=Filesystem", func(t *testing.T) {
		testStore(t, func() scratch.Store {
			logger := log.NewNopLogger()
			store, err := scratch.NewFilesystem(logger, t.TempDir())
			require.NoError(t, err)
			return store
		})
	})

	t.Run("impl=Observed", func(t *testing.T) {
		testStore(t, func() scratch.Store {
			return scratch.ObserveStore(scratch.NewMetrics(), scratch.NewMemory())
		})
	})
}

func testStore(t *testing.T, makeStore func() scratch.Store) {
	exampleData := []byte("test data content")

	t.Run("Put", func(t *testing.T) {
		store := makeStore()

		require.NotPanics(t, func() {
			_ = store.Put(exampleData)
		})
	})

	t.Run("Read", func(t *testing.T) {
		store := makeStore()

		handle := store.Put(exampleData)
		reader, err := store.Read(handle)
		require.NoError(t, err)
		defer reader.Close()

		actualData, err := io.ReadAll(reader)
		require.NoError(t, err)
		require.Equal(t, exampleData, actualData)
	})

	t.Run("Read (invalid handle)", func(t *testing.T) {
		store := makeStore()

		invalidHandle := scratch.Handle(math.MaxUint64)
		_, err := store.Read(invalidHandle)
		require.ErrorAs(t, err, new(scratch.HandleNotFoundError))
	})

	t.Run("Remove", func(t *testing.T) {
		store := makeStore()

		handle := store.Put(exampleData)

		// Check that the handle exists.
		closer, err := store.Read(handle)
		require.NoError(t, err)
		require.NoError(t, closer.Close())

		require.NoError(t, store.Remove(handle))

		_, err = store.Read(handle)
		require.ErrorAs(t, err, new(scratch.HandleNotFoundError))
		require.ErrorAs(t, store.Remove(handle), new(scratch.HandleNotFoundError))
	})

	t.Run("Remove (invalid handle)", func(t *testing.T) {
		store := makeStore()

		invalidHandle := scratch.Handle(math.MaxUint64)
		require.ErrorAs(t, store.Remove(invalidHandle), new(scratch.HandleNotFoundError))
	})
}
