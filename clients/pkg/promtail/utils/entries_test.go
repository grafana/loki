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

func TestEntryHandlerFanouter(t *testing.T) {
	eh1 := newSavingEntryHandler()
	eh2 := newSavingEntryHandler()
	fanouter := NewEntryHandlerFanouter(eh1, eh2)

	var expectedLines = []string{
		"some line",
		"some other line",
		"some other other line",
	}

	for _, line := range expectedLines {
		fanouter.Chan() <- api.Entry{
			Labels: model.LabelSet{
				"test": "fanouter",
			},
			Entry: logproto.Entry{
				Timestamp: time.Now(),
				Line:      line,
			},
		}
	}

	fanouter.Stop()
	eh2.Stop()
	eh1.Stop()

	require.Len(t, eh1.Received, len(expectedLines))
	require.Len(t, eh2.Received, len(expectedLines))
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

func (x *savingEntryHandler) Chan() chan<- api.Entry {
	return x.entries
}

func (x *savingEntryHandler) Stop() {
	close(x.entries)
	x.wg.Wait()
}
