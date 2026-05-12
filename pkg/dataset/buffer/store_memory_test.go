package buffer_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataset/buffer"
)

func TestMemoryStore(t *testing.T) {
	var s buffer.MemoryStore

	var (
		inputData = []buffer.Data{buffer.Data("hello"), buffer.Data("world")}
		expectIDs = []buffer.ID{1, 2}
	)

	ids, err := s.WriteBuffers(t.Context(), inputData)
	require.NoError(t, err)
	require.Equal(t, expectIDs, ids)

	got, err := s.ReadBuffers(t.Context(), nil, ids)
	require.NoError(t, err)
	require.Equal(t, inputData, got)
}

func TestMemoryStore_WriteBuffers(t *testing.T) {
	t.Run("WriteBuffers clones memory", func(t *testing.T) {
		var s buffer.MemoryStore

		orig := buffer.Data("abc")
		ids, err := s.WriteBuffers(t.Context(), []buffer.Data{orig})
		require.NoError(t, err)

		// Mutate the original; the stored copy must be unaffected.
		orig[0] = 'z'

		got, err := s.ReadBuffers(t.Context(), nil, ids)
		require.NoError(t, err)
		require.Equal(t, buffer.Data("abc"), got[0])
	})

	t.Run("normalizes nil to empty", func(t *testing.T) {
		var s buffer.MemoryStore

		ids, err := s.WriteBuffers(t.Context(), []buffer.Data{nil})
		require.NoError(t, err)

		got, err := s.ReadBuffers(t.Context(), nil, ids)
		require.NoError(t, err)
		require.Equal(t, buffer.Data{}, got[0])
	})
}

func TestMemoryStore_ReadBuffers(t *testing.T) {
	t.Run("fails with invalid ID", func(t *testing.T) {
		var s buffer.MemoryStore

		_, err := s.ReadBuffers(t.Context(), nil, []buffer.ID{0})
		require.Error(t, err, "ReadBuffers should fail with invalid ID")

		_, err = s.ReadBuffers(t.Context(), nil, []buffer.ID{99})
		require.Error(t, err, "ReadBuffers should fail with invalid ID")
	})
}

func TestMemoryStore_BufferSizes(t *testing.T) {
	t.Run("succeeds with valid buffers", func(t *testing.T) {
		var s buffer.MemoryStore

		var (
			inputData   = []buffer.Data{[]byte("ab"), []byte("cdefg")}
			expectSizes = []int64{2, 5}
		)

		ids, err := s.WriteBuffers(t.Context(), inputData)
		require.NoError(t, err)

		sizes, err := s.BufferSizes(t.Context(), nil, ids)
		require.NoError(t, err)
		require.Equal(t, expectSizes, sizes)
	})

	t.Run("fails with non-existing buffers", func(t *testing.T) {
		ctx := context.Background()
		var s buffer.MemoryStore

		_, err := s.BufferSizes(ctx, nil, []buffer.ID{0})
		require.Error(t, err)

		_, err = s.BufferSizes(ctx, nil, []buffer.ID{99})
		require.Error(t, err)
	})
}

func TestMemoryStore_Delete(t *testing.T) {
	t.Run("succeeds with existing buffers", func(t *testing.T) {
		var s buffer.MemoryStore

		ids, err := s.WriteBuffers(t.Context(), []buffer.Data{[]byte("a"), []byte("b")})
		require.NoError(t, err)

		require.NoError(t, s.Delete(t.Context(), ids[:1]), "Delete should succeed with existing buffers")

		_, err = s.ReadBuffers(t.Context(), nil, ids[:1])
		require.Error(t, err, "ReadBuffers should fail with deleted buffer")

		_, err = s.BufferSizes(t.Context(), nil, ids[:1])
		require.Error(t, err, "BufferSizes should fail with deleted buffer")

		got, err := s.ReadBuffers(t.Context(), nil, ids[1:])
		require.NoError(t, err)
		require.Equal(t, buffer.Data("b"), got[0], "Undeleted buffers should still be readable")
	})

	t.Run("fails on deleted buffer", func(t *testing.T) {
		var s buffer.MemoryStore

		ids, err := s.WriteBuffers(t.Context(), []buffer.Data{[]byte("x")})
		require.NoError(t, err)

		require.NoError(t, s.Delete(t.Context(), ids), "Delete should succeed with existing buffers")
		require.Error(t, s.Delete(t.Context(), ids), "Delete should fail with deleted buffer")
	})
}
