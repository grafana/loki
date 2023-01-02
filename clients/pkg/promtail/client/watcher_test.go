package client

import (
	"os"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/loki/clients/pkg/promtail/api"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/tsdb/record"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/ingester"
	"github.com/grafana/loki/pkg/logproto"
)

const (
	waitFor     = time.Second
	checkTicker = 100 * time.Millisecond
)

type mockConsumer struct {
	Series            []record.RefSeries
	Entries           []ingester.RefEntries
	LastSegmentClosed int
}

func newMockConsumer() *mockConsumer {
	return &mockConsumer{
		Series:            make([]record.RefSeries, 0),
		Entries:           make([]ingester.RefEntries, 0),
		LastSegmentClosed: -1,
	}
}

func (s *mockConsumer) ConsumeSeries(series record.RefSeries) error {
	s.Series = append(s.Series, series)
	return nil
}

func (s *mockConsumer) ConsumeEntries(entries ingester.RefEntries) error {
	s.Entries = append(s.Entries, entries)
	return nil
}

func (s *mockConsumer) SegmentEnd(segmentNum int) {
	s.LastSegmentClosed = segmentNum
}

var debugLogger = log.NewLogfmtLogger(os.Stdout)

func TestWatcher_SimpleWriteAndRead(t *testing.T) {
	dir := t.TempDir()
	entryWriter := NewWALEntryWriter()
	w, err := newWAL(debugLogger, nil, WALConfig{
		Dir:     dir,
		Enabled: true,
	}, "test_client", "123")
	require.NoError(t, err, "error starting wal")
	defer func() {
		_ = w.Delete()
	}()

	watchedWALConsumer := newMockConsumer()
	watcher := NewWALWatcher(w.Dir(), watchedWALConsumer, debugLogger)
	watcher.Start()
	defer watcher.Stop()

	testEntry := api.Entry{
		Labels: model.LabelSet{
			"job": "wal_test",
		},
		Entry: logproto.Entry{
			Line:      "holis",
			Timestamp: time.Now(),
		},
	}
	entryWriter.WriteEntry(testEntry, w, debugLogger)
	require.NoError(t, w.Sync(), "failed to sync wal")

	require.Eventually(t, func() bool {
		return len(watchedWALConsumer.Entries) == 1
	}, waitFor, checkTicker)
}

func TestWatcher_CallbackOnSegmentClosed(t *testing.T) {
	dir := t.TempDir()
	entryWriter := NewWALEntryWriter()
	w, err := newWAL(debugLogger, nil, WALConfig{
		Dir:     dir,
		Enabled: true,
	}, "test_client", "123")
	require.NoError(t, err, "error starting wal")
	defer func() {
		_ = w.Delete()
	}()

	consumer := newMockConsumer()
	watcher := NewWALWatcher(w.Dir(), consumer, debugLogger)
	watcher.Start()
	defer watcher.Stop()

	testEntry := api.Entry{
		Labels: model.LabelSet{
			"job": "wal_test",
		},
		Entry: logproto.Entry{
			Line:      "holis",
			Timestamp: time.Now(),
		},
	}

	// check pre
	require.Equal(t, -1, consumer.LastSegmentClosed)

	entryWriter.WriteEntry(testEntry, w, debugLogger)
	require.NoError(t, w.Sync(), "failed to sync wal")

	require.Eventually(t, func() bool {
		return len(consumer.Entries) == 1
	}, waitFor, checkTicker)

	entryWriter.WriteEntry(testEntry, w, debugLogger)
	_, err = w.NextSegment()
	require.NoError(t, err, "error moving to next segment")

	require.Eventually(t, func() bool {
		return len(consumer.Entries) == 1 && consumer.LastSegmentClosed == 0
	}, waitFor, checkTicker)
}
