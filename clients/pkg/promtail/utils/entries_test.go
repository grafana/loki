package utils

import (
	"sync"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/clients/pkg/promtail/api"
	"github.com/grafana/loki/pkg/logproto"
)

func TestFanoutEntryHandler(t *testing.T) {
	eh1 := newSavingEntryHandler()
	eh2 := newSavingEntryHandler()
	fanout := NewFanoutEntryHandler(eh1, eh2)

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
		return len(eh1.Received) == len(expectedLines) && len(eh2.Received) == len(expectedLines)
	}, time.Second*10, time.Second, "expected entries to be received by fanned out channels")
}

type savingEntryHandler struct {
	entries  chan api.Entry
	Received []api.Entry
	wg       sync.WaitGroup
}

func newSavingEntryHandler() *savingEntryHandler {
	eh := &savingEntryHandler{
		entries:  make(chan api.Entry),
		Received: []api.Entry{},
	}
	eh.wg.Add(1)
	go func() {
		for e := range eh.entries {
			eh.Received = append(eh.Received, e)
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
