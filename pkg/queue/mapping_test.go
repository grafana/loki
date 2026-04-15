package queue

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestQueueMapping(t *testing.T) {
	// Individual sub-tests in this test case are reflecting a scenario and need
	// to be executed in sequential order.

	m := &Mapping[*TreeQueue]{}
	m.Init(16)

	require.Equal(t, m.Len(), 0)

	t.Run("put item to mapping", func(t *testing.T) {
		q1 := newTreeQueue(10, "queue-1")
		m.Put(q1.Name(), q1)
		require.Equal(t, 1, m.Len())
		require.Equal(t, []string{"queue-1"}, m.Keys())
	})

	t.Run("insert order is preserved if there is no empty slot", func(t *testing.T) {
		q2 := newTreeQueue(10, "queue-2")
		m.Put(q2.Name(), q2)
		require.Equal(t, 2, m.Len())
		require.Equal(t, []string{"queue-1", "queue-2"}, m.Keys())
	})

	t.Run("insert into empty slot if item was removed previously", func(t *testing.T) {
		ok := m.Remove("queue-1")
		require.True(t, ok)
		require.Equal(t, 1, m.Len())
		q3 := newTreeQueue(10, "queue-3")
		m.Put(q3.Name(), q3)
		require.Equal(t, 2, m.Len())
		require.Equal(t, []string{"queue-3", "queue-2"}, m.Keys())
	})

	t.Run("insert order is preserved across keys and values", func(t *testing.T) {
		q4 := newTreeQueue(10, "queue-4")
		m.Put(q4.Name(), q4)
		require.Equal(t, 3, m.Len())
		for idx, v := range m.Values() {
			require.Equal(t, v.Name(), m.Keys()[idx])
		}
	})

	t.Run("get by key", func(t *testing.T) {
		key := "queue-2"
		item := m.GetByKey(key)
		require.Equal(t, key, item.Name())
		require.Equal(t, QueueIndex(1), item.Pos())
	})

	t.Run("get by empty key returns nil", func(t *testing.T) {
		require.Nil(t, m.GetByKey(""))
		require.Nil(t, m.GetByKey(empty))
	})

	t.Run("get next item based on index must not skip when items are removed", func(t *testing.T) {
		item, err := m.GetNext(-1)
		require.Nil(t, err)
		require.Equal(t, "queue-3", item.Name())
		item, err = m.GetNext(item.Pos())
		require.Nil(t, err)
		require.Equal(t, "queue-2", item.Name())
		m.Remove(item.Name())
		item, err = m.GetNext(item.Pos())
		require.Nil(t, err)
		require.Equal(t, "queue-4", item.Name())
	})

	t.Run("get next item out of range returns ErrOutOfBounds", func(t *testing.T) {
		item, err := m.GetNext(100)
		require.Nil(t, item)
		require.ErrorIs(t, err, ErrOutOfBounds)

	})

	t.Run("get next item skips empty slots", func(t *testing.T) {
		item, err := m.GetNext(-1)
		require.Nil(t, err)
		require.Equal(t, "queue-3", item.Name())
		item, err = m.GetNext(item.Pos())
		require.Nil(t, err)
		require.Equal(t, "queue-4", item.Name())
	})

}
