package consumer

import (
	"context"
	"testing"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
)

func TestPartitionLifecycler(t *testing.T) {
	// We use a mock to record the calls from the lifecycler.
	p := &mockPartitionProcessorLifecycler{}
	l := newPartitionLifecycler(p, log.NewNopLogger())
	require.Nil(t, p.processors)
	// Assign a couple topics and partitions.
	topics := map[string][]int32{
		"topic1": {0, 1, 2},
		"topic2": {3, 4, 5},
	}
	l.Assign(context.TODO(), nil, topics)
	// There should be two topics.
	require.Len(t, p.processors, 2)
	// The first topic should have partitions 0, 1 and 2.
	expected1 := map[int32]struct{}{0: {}, 1: {}, 2: {}}
	require.Equal(t, expected1, p.processors["topic1"])
	// And the second topic should have partitions 3, 4 and 5.
	expected2 := map[int32]struct{}{3: {}, 4: {}, 5: {}}
	require.Equal(t, expected2, p.processors["topic2"])
}

func TestPartitionLifecycler_Revoke(t *testing.T) {
	p := &mockPartitionProcessorLifecycler{}
	l := newPartitionLifecycler(p, log.NewNopLogger())
	require.Nil(t, p.processors)
	// Assign a couple topics and partitions.
	topics := map[string][]int32{
		"topic1": {0, 1, 2},
		"topic2": {3, 4, 5},
	}
	l.Assign(context.TODO(), nil, topics)
	// There should be two topics.
	require.Len(t, p.processors, 2)
	// Revoke a partition.
	revoke1 := map[string][]int32{"topic1": {0}}
	l.Revoke(context.TODO(), nil, revoke1)
	// There should be two topics.
	require.Len(t, p.processors, 2)
	// The first topic should have partitions 1 and 2.
	expected1 := map[int32]struct{}{1: {}, 2: {}}
	require.Equal(t, expected1, p.processors["topic1"])
	// And the second topic should have partitions 3, 4 and 5.
	expected2 := map[int32]struct{}{3: {}, 4: {}, 5: {}}
	require.Equal(t, expected2, p.processors["topic2"])
}

func TestPartitionLifecycler_Stop(t *testing.T) {
	p := &mockPartitionProcessorLifecycler{}
	l := newPartitionLifecycler(p, log.NewNopLogger())
	require.Nil(t, p.processors)
	require.False(t, l.done)
	l.Stop(context.TODO())
	require.True(t, l.done)
	// Once stopped, should not be able to assign partitions.
	topics := map[string][]int32{
		"topic1": {0, 1, 2},
		"topic2": {3, 4, 5},
	}
	l.Assign(context.TODO(), nil, topics)
	require.Nil(t, p.processors)
}
