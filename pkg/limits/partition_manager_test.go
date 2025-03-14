package limits

import (
	"context"
	"reflect"
	"testing"

	"github.com/coder/quartz"
	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
)

func TestPartitionManager_Assign(t *testing.T) {
	m := NewPartitionManager(log.NewNopLogger())
	c := quartz.NewMock(t)
	m.clock = c
	// Advance the clock so we compare with a time that is not the default
	// value.
	c.Advance(1)
	m.Assign(context.Background(), nil, map[string][]int32{
		"foo": {1, 2, 3},
	})
	// Assert that the partitions were assigned and the timestamps are set to
	// the current time.
	now := c.Now().UnixNano()
	require.Equal(t, map[int32]int64{
		1: now,
		2: now,
		3: now,
	}, m.partitions)
	// Advance the clock again, re-assign partition #3 and assign a new
	// partition #4. We expect the updated timestamp is equal to the advanced
	// time.
	c.Advance(1)
	m.Assign(context.Background(), nil, map[string][]int32{
		"foo": {3, 4},
	})
	later := c.Now().UnixNano()
	require.Equal(t, map[int32]int64{
		1: now,
		2: now,
		3: later,
		4: later,
	}, m.partitions)
}

func TestPartitionManager_Has(t *testing.T) {
	m := NewPartitionManager(log.NewNopLogger())
	c := quartz.NewMock(t)
	m.clock = c
	m.Assign(context.Background(), nil, map[string][]int32{
		"foo": {1, 2, 3},
	})
	require.True(t, m.Has(1))
	require.True(t, m.Has(2))
	require.True(t, m.Has(3))
	require.False(t, m.Has(4))
}

func TestPartitionManager_List(t *testing.T) {
	m := NewPartitionManager(log.NewNopLogger())
	c := quartz.NewMock(t)
	m.clock = c
	// Advance the clock so we compare with a time that is not the default
	// value.
	c.Advance(1)
	m.Assign(context.Background(), nil, map[string][]int32{
		"foo": {1, 2, 3},
	})
	now := c.Now().UnixNano()
	result := m.List()
	require.Equal(t, map[int32]int64{
		1: now,
		2: now,
		3: now,
	}, result)
	// Assert that m.List() returns a deep-copy, and does not point the same
	// memory.
	p1 := reflect.ValueOf(result).Pointer()
	p2 := reflect.ValueOf(m.partitions).Pointer()
	require.NotEqual(t, p1, p2)
}

func TestPartitionManager_Remove(t *testing.T) {
	m := NewPartitionManager(log.NewNopLogger())
	c := quartz.NewMock(t)
	m.clock = c
	m.Assign(context.Background(), nil, map[string][]int32{
		"foo": {1, 2, 3},
	})
	// Assert that the partitions were assigned and the timestamps are set to
	// the current time.
	now := c.Now().UnixNano()
	require.Equal(t, map[int32]int64{
		1: now,
		2: now,
		3: now,
	}, m.partitions)
	// Remove partitions 2 and 3.
	m.Remove(context.Background(), nil, map[string][]int32{
		"foo": {2, 3},
	})
	require.Equal(t, map[int32]int64{
		1: now,
	}, m.partitions)
}
