package consumer

import (
	"testing"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
)

func TestConsumer_OnRegister(t *testing.T) {
	c := newConsumer(&mockKafka{}, log.NewNopLogger())
	require.Empty(t, c.processors)
	// Register a processor for the topic.
	p1 := &partitionProcessor{}
	c.OnRegister("topic1", 1, p1)
	require.NotNil(t, c.processors["topic1"])
	require.Same(t, p1, c.processors["topic1"][1])
	// Register another processor for the same topic.
	p2 := &partitionProcessor{}
	c.OnRegister("topic1", 2, p2)
	require.Len(t, c.processors["topic1"], 2)
	require.Same(t, p1, c.processors["topic1"][1])
	require.Same(t, p2, c.processors["topic1"][2])
	// Register another processor for a different topic.
	p3 := &partitionProcessor{}
	c.OnRegister("topic2", 1, p3)
	require.NotNil(t, c.processors["topic1"])
	require.Len(t, c.processors["topic1"], 2)
	require.Same(t, p1, c.processors["topic1"][1])
	require.Same(t, p2, c.processors["topic1"][2])
	require.Len(t, c.processors["topic2"], 1)
	require.Same(t, p3, c.processors["topic2"][1])
}

func TestConsumer_OnDeregister(t *testing.T) {
	c := newConsumer(&mockKafka{}, log.NewNopLogger())
	require.Empty(t, c.processors)
	// Register a bunch of processors for different topics.
	p1 := &partitionProcessor{}
	c.OnRegister("topic1", 1, p1)
	p2 := &partitionProcessor{}
	c.OnRegister("topic1", 2, p2)
	require.Len(t, c.processors["topic1"], 2)
	p3 := &partitionProcessor{}
	c.OnRegister("topic2", 1, p3)
	// Check all the processors are as expected.
	require.NotNil(t, c.processors["topic1"])
	require.Len(t, c.processors["topic1"], 2)
	require.Same(t, p1, c.processors["topic1"][1])
	require.Same(t, p2, c.processors["topic1"][2])
	require.Len(t, c.processors["topic2"], 1)
	require.Same(t, p3, c.processors["topic2"][1])
	// Now deregister one and check the remaining processors.
	c.OnDeregister("topic1", 1)
	require.Len(t, c.processors["topic1"], 1)
	require.Nil(t, c.processors["topic1"][1])
	require.Same(t, p2, c.processors["topic1"][2])
	// Now deregister the last one and check the topic is removed.
	c.OnDeregister("topic1", 2)
	require.Nil(t, c.processors["topic1"])
	// The other topic should be unmodified.
	require.Len(t, c.processors["topic2"], 1)
	require.Same(t, p3, c.processors["topic2"][1])
}
