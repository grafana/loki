package ingesterrf1

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestIngester_cleanIdleStreams(t *testing.T) {
	i := &Ingester{
		instancesMtx: sync.RWMutex{},
		instances:    make(map[string]*instance),
		cfg:          Config{StreamRetainPeriod: time.Minute},
	}
	instance := &instance{
		instanceID: "test",
		streams:    newStreamsMap(),
	}
	stream := &stream{
		labelsString: "test,label",
		highestTs:    time.Now(),
	}
	instance.streams.Store(stream.labelsString, stream)
	i.instances[instance.instanceID] = instance

	require.Len(t, i.instances, 1)
	require.Equal(t, 1, instance.streams.Len())

	// No-op
	i.cleanIdleStreams()

	require.Len(t, i.instances, 1)
	require.Equal(t, 1, instance.streams.Len())

	// Pretend stream is old and retry
	stream.highestTs = time.Now().Add(-time.Minute * 2)
	i.cleanIdleStreams()

	require.Len(t, i.instances, 1)
	require.Equal(t, 0, instance.streams.Len())
}
