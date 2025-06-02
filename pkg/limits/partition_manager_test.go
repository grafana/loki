package limits

import (
	"context"
	"reflect"
	"testing"

	"github.com/coder/quartz"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func TestPartitionManager_Assign(t *testing.T) {
	m, err := newPartitionManager(prometheus.NewRegistry())
	require.NoError(t, err)
	c := quartz.NewMock(t)
	m.clock = c
	// Advance the clock so we compare with a time that is not the default
	// value.
	c.Advance(1)
	m.Assign(context.Background(), []int32{1, 2, 3})
	// Assert that the partitions were assigned and the timestamps are set to
	// the current time.
	now := c.Now().UnixNano()
	require.Equal(t, map[int32]partitionEntry{
		1: {
			assignedAt: now,
			state:      partitionPending,
		},
		2: {
			assignedAt: now,
			state:      partitionPending,
		},
		3: {
			assignedAt: now,
			state:      partitionPending,
		},
	}, m.partitions)
	// Advance the clock again, re-assign partition #3 and assign a new
	// partition #4. We expect the updated timestamp is equal to the advanced
	// time.
	c.Advance(1)
	m.Assign(context.Background(), []int32{3, 4})
	later := c.Now().UnixNano()
	require.Equal(t, map[int32]partitionEntry{
		1: {
			assignedAt: now,
			state:      partitionPending,
		},
		2: {
			assignedAt: now,
			state:      partitionPending,
		},
		3: {
			assignedAt: later,
			state:      partitionPending,
		},
		4: {
			assignedAt: later,
			state:      partitionPending,
		},
	}, m.partitions)
}

func TestPartitionManager_GetState(t *testing.T) {
	m, err := newPartitionManager(prometheus.NewRegistry())
	require.NoError(t, err)
	c := quartz.NewMock(t)
	m.clock = c
	m.Assign(context.Background(), []int32{1, 2, 3})
	// Getting the state for an assigned partition should return true.
	state, ok := m.GetState(1)
	require.True(t, ok)
	require.Equal(t, partitionPending, state)
	// Getting the state for an unknown partition should return false.
	_, ok = m.GetState(4)
	require.False(t, ok)
}

func TestPartitionManager_TargetOffsetReached(t *testing.T) {
	m, err := newPartitionManager(prometheus.NewRegistry())
	require.NoError(t, err)
	c := quartz.NewMock(t)
	m.clock = c
	m.Assign(context.Background(), []int32{1})
	// Target offset cannot be reached for pending partition.
	require.False(t, m.TargetOffsetReached(1, 0))
	// Target offset has not been reached.
	require.True(t, m.SetReplaying(1, 10))
	require.False(t, m.TargetOffsetReached(1, 9))
	// Target offset has been reached.
	require.True(t, m.SetReplaying(1, 10))
	require.True(t, m.TargetOffsetReached(1, 10))
	// Target offset cannot be reached for ready partition.
	require.True(t, m.SetReady(1))
	require.False(t, m.TargetOffsetReached(1, 10))
}

func TestPartitionManager_Has(t *testing.T) {
	m, err := newPartitionManager(prometheus.NewRegistry())
	require.NoError(t, err)
	c := quartz.NewMock(t)
	m.clock = c
	m.Assign(context.Background(), []int32{1, 2, 3})
	require.True(t, m.Has(1))
	require.True(t, m.Has(2))
	require.True(t, m.Has(3))
	require.False(t, m.Has(4))
}

func TestPartitionManager_List(t *testing.T) {
	m, err := newPartitionManager(prometheus.NewRegistry())
	require.NoError(t, err)
	c := quartz.NewMock(t)
	m.clock = c
	// Advance the clock so we compare with a time that is not the default
	// value.
	c.Advance(1)
	m.Assign(context.Background(), []int32{1, 2, 3})
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

func TestPartitionManager_ListByState(t *testing.T) {
	m, err := newPartitionManager(prometheus.NewRegistry())
	require.NoError(t, err)
	c := quartz.NewMock(t)
	m.clock = c
	// Advance the clock so we compare with a time that is not the default
	// value.
	c.Advance(1)
	m.Assign(context.Background(), []int32{1, 2, 3})
	now := c.Now().UnixNano()
	result := m.ListByState(partitionPending)
	require.Equal(t, map[int32]int64{
		1: now,
		2: now,
		3: now,
	}, result)
	// Assert that m.ListByState() returns a deep-copy, and does not point the same
	// memory.
	p1 := reflect.ValueOf(result).Pointer()
	p2 := reflect.ValueOf(m.partitions).Pointer()
	require.NotEqual(t, p1, p2)
	// Get all ready partitions.
	result = m.ListByState(partitionReady)
	require.Empty(t, result)
	// Mark a partition as ready and then repeat the test.
	require.True(t, m.SetReady(1))
	result = m.ListByState(partitionReady)
	require.Equal(t, map[int32]int64{1: now}, result)
}

func TestPartitionManager_SetReplaying(t *testing.T) {
	m, err := newPartitionManager(prometheus.NewRegistry())
	require.NoError(t, err)
	c := quartz.NewMock(t)
	m.clock = c
	m.Assign(context.Background(), []int32{1, 2, 3})
	// Setting an assigned partition to replaying should return true.
	require.True(t, m.SetReplaying(1, 10))
	state, ok := m.GetState(1)
	require.True(t, ok)
	require.Equal(t, partitionReplaying, state)
	// Setting an unknown partition to replaying should return false.
	require.False(t, m.SetReplaying(4, 10))
}

func TestPartitionManager_SetReady(t *testing.T) {
	m, err := newPartitionManager(prometheus.NewRegistry())
	require.NoError(t, err)
	c := quartz.NewMock(t)
	m.clock = c
	m.Assign(context.Background(), []int32{1, 2, 3})
	// Setting an assigned partition to ready should return true.
	require.True(t, m.SetReady(1))
	state, ok := m.GetState(1)
	require.True(t, ok)
	require.Equal(t, partitionReady, state)
	// Setting an unknown partition to ready should return false.
	require.False(t, m.SetReady(4))
}

func TestPartitionManager_Revoke(t *testing.T) {
	m, err := newPartitionManager(prometheus.NewRegistry())
	require.NoError(t, err)
	c := quartz.NewMock(t)
	m.clock = c
	m.Assign(context.Background(), []int32{1, 2, 3})
	// Assert that the partitions were assigned and the timestamps are set to
	// the current time.
	now := c.Now().UnixNano()
	require.Equal(t, map[int32]partitionEntry{
		1: {
			assignedAt: now,
			state:      partitionPending,
		},
		2: {
			assignedAt: now,
			state:      partitionPending,
		},
		3: {
			assignedAt: now,
			state:      partitionPending,
		},
	}, m.partitions)
	// Revoke partitions 2 and 3.
	m.Revoke(context.Background(), []int32{2, 3})
	require.Equal(t, map[int32]partitionEntry{
		1: {
			assignedAt: now,
			state:      partitionPending,
		},
	}, m.partitions)
}
