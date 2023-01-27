package wal

import (
	"os"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/clients/pkg/promtail/api"

	"github.com/grafana/loki/pkg/logproto"
)

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

func TestWriter_EntriesAreWrittenToWALAndForwardedToClients(t *testing.T) {
	logger := log.NewLogfmtLogger(os.Stdout)
	dir := t.TempDir()
	wl, err := New(Config{
		Dir:     dir,
		Enabled: true,
	}, logger, prometheus.NewRegistry())
	require.NoError(t, err)

	eh := newSavingEntryHandler()

	writer := NewWriter(wl, logger, eh)
	defer func() {
		writer.Stop()
	}()
	// write entries to wal and sync

	var testLabels = model.LabelSet{
		"testing": "log",
	}
	var lines = []string{
		"some line",
		"some other line",
		"some other other line",
	}

	for _, line := range lines {
		writer.Chan() <- api.Entry{
			Labels: testLabels,
			Entry: logproto.Entry{
				Timestamp: time.Now(),
				Line:      line,
			},
		}
	}

	require.NoError(t, wl.Sync(), "failed to sync wal")

	// assert over WAL entries
	readEntries, err := ReadWAL(dir)
	require.NoError(t, err)
	require.Len(t, readEntries, len(lines), "written lines and seen differ")
	require.Equal(t, testLabels, readEntries[0].Labels)

	// assert over entries written to client
	require.Len(t, eh.Received, len(lines), "count of entries received in client differ")
}
