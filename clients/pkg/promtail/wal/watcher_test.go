package wal

import (
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/loki/clients/pkg/promtail/api"
	"github.com/grafana/loki/pkg/ingester/wal"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/util"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/tsdb/record"
	"github.com/stretchr/testify/require"
	"os"
	"testing"
	"time"
)

//to test
//- TestWatcher_ReadEntries
//- TestWatcher_ContinueReadingEntriesInNextSegmentAfterPreviousSegmentClose
//- TestWatcher_ReadFromLastSegmentOnStart

type testWriteTo struct {
	ReadEntries []api.Entry
	series      map[uint64]model.LabelSet
	logger      log.Logger
}

func (t *testWriteTo) StoreSeries(series []record.RefSeries, i int) {
	for _, seriesRec := range series {
		t.series[uint64(seriesRec.Ref)] = util.MapToModelLabelSet(seriesRec.Labels.Map())
	}
}

func (t *testWriteTo) AppendEntries(entries wal.RefEntries) error {
	var entry api.Entry
	if l, ok := t.series[uint64(entries.Ref)]; ok {
		entry.Labels = l
		for _, e := range entries.Entries {
			entry.Entry = e
			t.ReadEntries = append(t.ReadEntries, entry)
		}
	} else {
		level.Debug(t.logger).Log("series for entry not found")
	}
	return nil
}

func Test_ReadEntries(t *testing.T) {
	reg := prometheus.NewRegistry()
	logger := level.NewFilter(log.NewLogfmtLogger(os.Stdout), level.AllowDebug())
	dir := t.TempDir()
	metrics := NewWatcherMetrics(reg)
	writeTo := &testWriteTo{
		series: map[uint64]model.LabelSet{},
		logger: logger,
	}

	watcher := NewWatcher(dir, "test", metrics, writeTo, logger)
	watcher.Start()
	defer watcher.Stop()

	wl, err := New(Config{
		Enabled: true,
		Dir:     dir,
	}, logger, reg)
	require.NoError(t, err)
	ew := newEntryWriter()

	lines := []string{
		"holis",
		"holus",
		"chau",
	}
	testLabels := model.LabelSet{
		"test": "watcher_read",
	}

	for _, line := range lines {
		ew.WriteEntry(api.Entry{
			Labels: testLabels,
			Entry: logproto.Entry{
				Timestamp: time.Now(),
				Line:      line,
			},
		}, wl, logger)
	}
	require.NoError(t, wl.Sync())

	require.Eventually(t, func() bool {
		return len(writeTo.ReadEntries) == 3
	}, time.Second*10, time.Second, "expected watcher to catch up with written entries")
	for _, readEntry := range writeTo.ReadEntries {
		require.Contains(t, lines, readEntry.Line, "not expected log line")
	}
}

func TestWatcher_ContinueReadingEntriesInNextSegmentAfterPreviousSegmentClose(t *testing.T) {
	reg := prometheus.NewRegistry()
	logger := level.NewFilter(log.NewLogfmtLogger(os.Stdout), level.AllowDebug())
	dir := t.TempDir()
	metrics := NewWatcherMetrics(reg)
	writeTo := &testWriteTo{
		series: map[uint64]model.LabelSet{},
		logger: logger,
	}

	watcher := NewWatcher(dir, "test", metrics, writeTo, logger)
	watcher.Start()
	defer watcher.Stop()

	wl, err := New(Config{
		Enabled: true,
		Dir:     dir,
	}, logger, reg)
	require.NoError(t, err)
	ew := newEntryWriter()

	lines := []string{
		"holis",
		"holus",
		"chau",
	}
	linesAfter := []string{
		"holis2",
		"holus2",
		"chau2",
	}
	testLabels := model.LabelSet{
		"test": "watcher_read",
	}

	for _, line := range lines {
		ew.WriteEntry(api.Entry{
			Labels: testLabels,
			Entry: logproto.Entry{
				Timestamp: time.Now(),
				Line:      line,
			},
		}, wl, logger)
	}
	require.NoError(t, wl.Sync())

	require.Eventually(t, func() bool {
		return len(writeTo.ReadEntries) == 3
	}, time.Second*10, time.Second, "expected watcher to catch up with written entries")
	for _, readEntry := range writeTo.ReadEntries {
		require.Contains(t, lines, readEntry.Line, "not expected log line")
	}

	_, err = wl.NextSegment()
	require.NoError(t, err, "expected no error when moving to next wal segment")

	for _, line := range linesAfter {
		ew.WriteEntry(api.Entry{
			Labels: testLabels,
			Entry: logproto.Entry{
				Timestamp: time.Now(),
				Line:      line,
			},
		}, wl, logger)
	}
	require.NoError(t, wl.Sync())

	require.Eventually(t, func() bool {
		return len(writeTo.ReadEntries) == 6
	}, time.Second*10, time.Second, "expected watcher to catch up after new wal segment is cut")
	// assert over second half of entries
	for _, readEntry := range writeTo.ReadEntries[3:] {
		require.Contains(t, linesAfter, readEntry.Line, "not expected log line")
	}
}
func TestWatcher_ReadFromLastSegmentOnStart(t *testing.T) {
	reg := prometheus.NewRegistry()
	logger := level.NewFilter(log.NewLogfmtLogger(os.Stdout), level.AllowDebug())
	dir := t.TempDir()
	metrics := NewWatcherMetrics(reg)
	writeTo := &testWriteTo{
		series: map[uint64]model.LabelSet{},
		logger: logger,
	}

	watcher := NewWatcher(dir, "test", metrics, writeTo, logger)

	wl, err := New(Config{
		Enabled: true,
		Dir:     dir,
	}, logger, reg)
	require.NoError(t, err)
	ew := newEntryWriter()

	linesAfter := []string{
		"holis2",
		"holus2",
		"chau2",
	}
	testLabels := model.LabelSet{
		"test": "watcher_read",
	}

	// write something to first segment
	ew.WriteEntry(api.Entry{
		Labels: testLabels,
		Entry: logproto.Entry{
			Timestamp: time.Now(),
			Line:      "this shouldn't be read",
		},
	}, wl, logger)

	require.NoError(t, wl.Sync())

	_, err = wl.NextSegment()
	require.NoError(t, err, "expected no error when moving to next wal segment")

	watcher.Start()
	defer watcher.Stop()

	for _, line := range linesAfter {
		ew.WriteEntry(api.Entry{
			Labels: testLabels,
			Entry: logproto.Entry{
				Timestamp: time.Now(),
				Line:      line,
			},
		}, wl, logger)
	}
	require.NoError(t, wl.Sync())

	require.Eventually(t, func() bool {
		return len(writeTo.ReadEntries) == 3
	}, time.Second*10, time.Second, "expected watcher to catch up after new wal segment is cut")
	// assert over second half of entries
	for _, readEntry := range writeTo.ReadEntries[3:] {
		require.Contains(t, linesAfter, readEntry.Line, "not expected log line")
	}
}
