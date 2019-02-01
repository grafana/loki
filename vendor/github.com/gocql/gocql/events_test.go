package gocql

import (
	"net"
	"sync"
	"testing"
)

func TestEventDebounce(t *testing.T) {
	const eventCount = 150
	wg := &sync.WaitGroup{}
	wg.Add(1)

	eventsSeen := 0
	debouncer := newEventDebouncer("testDebouncer", func(events []frame) {
		defer wg.Done()
		eventsSeen += len(events)
	})
	defer debouncer.stop()

	for i := 0; i < eventCount; i++ {
		debouncer.debounce(&statusChangeEventFrame{
			change: "UP",
			host:   net.IPv4(127, 0, 0, 1),
			port:   9042,
		})
	}

	wg.Wait()
	if eventCount != eventsSeen {
		t.Fatalf("expected to see %d events but got %d", eventCount, eventsSeen)
	}
}
