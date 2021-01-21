package ingester

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type dumbFlusher struct {
	onInit, onFlush, postFlush func()
}

func newDumbFlusher(onInit, onFlush, postFlush func()) *dumbFlusher {
	return &dumbFlusher{
		onInit:    onInit,
		onFlush:   onFlush,
		postFlush: postFlush,
	}
}

func (f *dumbFlusher) InitFlushQueues() {
	if f.onInit != nil {
		f.onInit()
	}
}
func (f *dumbFlusher) Flush() {
	if f.onFlush != nil {
		f.onFlush()
	}
}
func (f *dumbFlusher) RemoveFlushedChunks() {
	if f.postFlush != nil {
		f.postFlush()
	}
}

func nilMetrics() *ingesterMetrics { return newIngesterMetrics(nil) }

func TestReplayController(t *testing.T) {
	var ops []string
	var opLock sync.Mutex

	var rc *replayController
	flusher := newDumbFlusher(
		func() {
			opLock.Lock()
			defer opLock.Unlock()
			ops = append(ops, "InitFlushQueues")
		},
		func() {
			rc.Sub(100) // simulate flushing 100 bytes
			opLock.Lock()
			defer opLock.Unlock()
			ops = append(ops, "Flush")
		},
		func() {
			opLock.Lock()
			defer opLock.Unlock()
			ops = append(ops, "PostFlush")
		},
	)
	rc = newReplayController(nilMetrics(), WALConfig{ReplayMemoryCeiling: 100}, flusher)

	var wg sync.WaitGroup
	n := 5
	wg.Add(n)

	for i := 0; i < n; i++ {
		// In order to prevent all the goroutines from running before they've added bytes
		// to the internal count, introduce a brief sleep.
		time.Sleep(time.Millisecond)

		// nolint:errcheck
		go rc.WithBackPressure(func() error {
			rc.Add(50)
			opLock.Lock()
			defer opLock.Unlock()
			ops = append(ops, "WithBackPressure")
			wg.Done()
			return nil
		})
	}

	wg.Wait()

	expected := []string{
		"WithBackPressure", // add 50, total 50
		"WithBackPressure", // add 50, total 100
		"InitFlushQueues",
		"Flush", // subtract 100, total 0
		"PostFlush",
		"WithBackPressure", // add 50, total 50
		"WithBackPressure", // add 50, total 100
		"InitFlushQueues",
		"Flush", // subtract 100, total 0
		"PostFlush",
		"WithBackPressure", // add 50, total 50
	}
	require.Equal(t, expected, ops)

}
