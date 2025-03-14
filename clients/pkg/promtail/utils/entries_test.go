package utils

import (
	"sync"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/clients/pkg/promtail/api"

	"github.com/grafana/loki/v3/pkg/logproto"
)

func TestFanoutEntryHandler_SuccessfulFanout(t *testing.T) {
	eh1 := newSavingEntryHandler()
	eh2 := newSavingEntryHandler()
	fanout := NewFanoutEntryHandler(time.Second*10, eh1, eh2)

	defer func() {
		fanout.Stop()
		eh2.Stop()
		eh1.Stop()
	}()

	var expectedLines = []string{
		"some line",
		"some other line",
		"some other other line",
	}

	for _, line := range expectedLines {
		fanout.Chan() <- api.Entry{
			Labels: model.LabelSet{
				"test": "fanout",
			},
			Entry: logproto.Entry{
				Timestamp: time.Now(),
				Line:      line,
			},
		}
	}

	require.Eventually(t, func() bool {
		eh1.mu.Lock()
		len1 := len(eh1.Received)
		eh1.mu.Unlock()
		eh2.mu.Lock()
		len2 := len(eh2.Received)
		eh2.mu.Unlock()

		return len1 == len(expectedLines) && len2 == len(expectedLines)
	}, time.Second*10, time.Second, "expected entries to be received by fanned out channels")
}

type blockingEntryHanlder struct {
	entries chan api.Entry
}

func (b *blockingEntryHanlder) Chan() chan<- api.Entry {
	return b.entries
}

func (b *blockingEntryHanlder) Stop() {
	close(b.entries)
}

func TestFanoutEntryHandler_TimeoutWaitingForEntriesToBeSent(t *testing.T) {
	eh1 := &blockingEntryHanlder{make(chan api.Entry)}
	controlEH := newSavingEntryHandler()
	fanout := NewFanoutEntryHandler(time.Second*2, eh1, controlEH)

	go func() {
		fanout.Chan() <- api.Entry{
			Labels: model.LabelSet{
				"test": "fanout",
			},
			Entry: logproto.Entry{
				Timestamp: time.Now(),
				Line:      "holis",
			},
		}
	}()

	require.Eventually(t, func() bool {
		controlEH.mu.Lock()
		defer controlEH.mu.Unlock()
		return len(controlEH.Received) == 1
	}, time.Second*5, time.Second, "expected control entry handler to receive an entry")

	now := time.Now()
	fanout.Stop()
	require.InDelta(t, time.Second*2, time.Since(now), float64(time.Millisecond*100), "expected fanout entry handler to stop before")
}

type savingEntryHandler struct {
	entries  chan api.Entry
	Received []api.Entry
	wg       sync.WaitGroup
	mu       sync.Mutex
}

func newSavingEntryHandler() *savingEntryHandler {
	eh := &savingEntryHandler{
		entries:  make(chan api.Entry),
		Received: []api.Entry{},
	}
	eh.wg.Add(1)
	go func() {
		for e := range eh.entries {
			eh.mu.Lock()
			eh.Received = append(eh.Received, e)
			eh.mu.Unlock()
		}
		eh.wg.Done()
	}()
	return eh
}

func (eh *savingEntryHandler) Chan() chan<- api.Entry {
	return eh.entries
}

func (eh *savingEntryHandler) Stop() {
	close(eh.entries)
	eh.wg.Wait()
}
