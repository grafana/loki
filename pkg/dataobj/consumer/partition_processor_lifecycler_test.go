package consumer

import (
	"context"
	"testing"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func TestPartitionProcessorLifecycler_AddListener(t *testing.T) {
	l := newPartitionProcessorLifecycler(nil, log.NewNopLogger(), prometheus.NewRegistry())
	m := &mockPartitionProcessorListener{}
	require.Empty(t, l.listeners)
	l.AddListener(m)
	require.Len(t, l.listeners, 1)
	require.Same(t, l.listeners[0], m)
}

func TestPartitionProcessorLifecycler_RemoveListener(t *testing.T) {
	l := newPartitionProcessorLifecycler(nil, log.NewNopLogger(), prometheus.NewRegistry())
	m1 := &mockPartitionProcessorListener{}
	require.Empty(t, l.listeners)
	l.AddListener(m1)
	m2 := &mockPartitionProcessorListener{}
	l.AddListener(m2)
	require.Len(t, l.listeners, 2)
	require.Equal(t, l.listeners[0], m1)
	require.Equal(t, l.listeners[1], m2)
	// Remove a listener.
	l.RemoveListener(m1)
	require.Len(t, l.listeners, 1)
	require.Same(t, l.listeners[0], m2)
	// Remove the last listener.
	l.RemoveListener(m2)
	require.Empty(t, l.listeners)
}

func TestPartitionProcessorLifecycler_Register(t *testing.T) {
	m := &mockPartitionProcessorFactory{}
	l := newPartitionProcessorLifecycler(m, log.NewNopLogger(), prometheus.NewRegistry())
	require.Empty(t, l.processors)
	// Register a topic and partition.
	require.Equal(t, 0, m.calls)
	l.Register(context.TODO(), nil, "topic1", 0)
	require.Len(t, l.processors["topic1"], 1)
	require.Equal(t, 1, m.calls)
	// Registering the same topic and partition a second time should not call
	// the factory.
	l.Register(context.TODO(), nil, "topic1", 0)
	require.Len(t, l.processors["topic1"], 1)
	require.Equal(t, 1, m.calls)
	// Register another partition for the same tenant.
	l.Register(context.TODO(), nil, "topic1", 1)
	require.Len(t, l.processors["topic1"], 2)
	require.Equal(t, 2, m.calls)
	// Register another topic and partition for a different tenant.
	l.Register(context.TODO(), nil, "topic2", 1)
	require.Len(t, l.processors["topic1"], 2)
	require.Len(t, l.processors["topic2"], 1)
	require.Equal(t, 3, m.calls)
}

func TestPartitionProcessorLifecycler_Deregister(t *testing.T) {
	m := &mockPartitionProcessorFactory{}
	l := newPartitionProcessorLifecycler(m, log.NewNopLogger(), prometheus.NewRegistry())
	require.Empty(t, l.processors)
	// Register a topic and partition.
	require.Equal(t, 0, m.calls)
	l.Register(context.TODO(), nil, "topic1", 0)
	l.Register(context.TODO(), nil, "topic1", 1)
	l.Register(context.TODO(), nil, "topic2", 0)
	require.Len(t, l.processors["topic1"], 2)
	require.Len(t, l.processors["topic2"], 1)
	// Deregister a partition for a tenant.
	l.Deregister(context.TODO(), "topic1", 0)
	require.Len(t, l.processors["topic1"], 1)
	// Check that the correct partition was deregistered.
	require.Nil(t, l.processors["topic1"][0])
	require.NotNil(t, l.processors["topic1"][1])
	require.Len(t, l.processors["topic2"], 1)
	// Deregister the last partition for the tenant.
	l.Deregister(context.TODO(), "topic1", 1)
	require.Nil(t, l.processors["topic1"])
	require.Len(t, l.processors["topic2"], 1)
}
