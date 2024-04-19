package wal

import (
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/tsdb/record"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/clients/pkg/promtail/api"

	"github.com/grafana/loki/v3/pkg/ingester/wal"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util"
)

type testWriteTo struct {
	ReadEntries         []api.Entry
	series              map[uint64]model.LabelSet
	logger              log.Logger
	ReceivedSeriesReset []int
	mu                  sync.Mutex
}

func (t *testWriteTo) StoreSeries(series []record.RefSeries, _ int) {
	for _, seriesRec := range series {
		t.series[uint64(seriesRec.Ref)] = util.MapToModelLabelSet(seriesRec.Labels.Map())
	}
}

func (t *testWriteTo) SeriesReset(segmentNum int) {
	level.Debug(t.logger).Log("msg", fmt.Sprintf("received series reset with %d", segmentNum))
	t.ReceivedSeriesReset = append(t.ReceivedSeriesReset, segmentNum)
}

func (t *testWriteTo) AppendEntries(entries wal.RefEntries) error {
	var entry api.Entry
	if l, ok := t.series[uint64(entries.Ref)]; ok {
		entry.Labels = l
		t.mu.Lock()
		for _, e := range entries.Entries {
			entry.Entry = e
			t.ReadEntries = append(t.ReadEntries, entry)
		}
		t.mu.Unlock()
	} else {
		level.Debug(t.logger).Log("series for entry not found")
	}
	return nil
}

// watcherTestResources contains all resources necessary to test an individual Watcher functionality
type watcherTestResources struct {
	writeEntry             func(entry api.Entry)
	notifyWrite            func()
	startWatcher           func()
	syncWAL                func() error
	nextWALSegment         func() error
	writeTo                *testWriteTo
	notifySegmentReclaimed func(segmentNum int)
}

type watcherTest func(t *testing.T, res *watcherTestResources)

// cases defines the watcher test cases
var cases = map[string]watcherTest{
	"read entries from WAL": func(t *testing.T, res *watcherTestResources) {
		res.startWatcher()

		lines := []string{
			"holis",
			"holus",
			"chau",
		}
		testLabels := model.LabelSet{
			"test": "watcher_read",
		}

		for _, line := range lines {
			res.writeEntry(api.Entry{
				Labels: testLabels,
				Entry: logproto.Entry{
					Timestamp: time.Now(),
					Line:      line,
				},
			})
		}
		require.NoError(t, res.syncWAL())

		// notify watcher that entries have been written
		res.notifyWrite()

		require.Eventually(t, func() bool {
			res.writeTo.mu.Lock()
			defer res.writeTo.mu.Unlock()
			return len(res.writeTo.ReadEntries) == 3
		}, time.Second*10, time.Second, "expected watcher to catch up with written entries")
		res.writeTo.mu.Lock()
		for _, readEntry := range res.writeTo.ReadEntries {
			require.Contains(t, lines, readEntry.Line, "not expected log line")
		}
		res.writeTo.mu.Unlock()
	},

	"read entries from WAL, just using backup timer to trigger reads": func(t *testing.T, res *watcherTestResources) {
		res.startWatcher()

		lines := []string{
			"holis",
			"holus",
			"chau",
		}
		testLabels := model.LabelSet{
			"test": "watcher_read",
		}

		for _, line := range lines {
			res.writeEntry(api.Entry{
				Labels: testLabels,
				Entry: logproto.Entry{
					Timestamp: time.Now(),
					Line:      line,
				},
			})
		}
		require.NoError(t, res.syncWAL())

		// do not notify, let the backup timer trigger the watcher reads

		require.Eventually(t, func() bool {
			res.writeTo.mu.Lock()
			defer res.writeTo.mu.Unlock()
			return len(res.writeTo.ReadEntries) == 3
		}, time.Second*10, time.Second, "expected watcher to catch up with written entries")
		res.writeTo.mu.Lock()
		for _, readEntry := range res.writeTo.ReadEntries {
			require.Contains(t, lines, readEntry.Line, "not expected log line")
		}
		res.writeTo.mu.Unlock()
	},

	"continue reading entries in next segment after initial segment is closed": func(t *testing.T, res *watcherTestResources) {
		res.startWatcher()
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
			res.writeEntry(api.Entry{
				Labels: testLabels,
				Entry: logproto.Entry{
					Timestamp: time.Now(),
					Line:      line,
				},
			})
		}
		require.NoError(t, res.syncWAL())

		res.notifyWrite()

		require.Eventually(t, func() bool {
			res.writeTo.mu.Lock()
			defer res.writeTo.mu.Unlock()
			return len(res.writeTo.ReadEntries) == 3
		}, time.Second*10, time.Second, "expected watcher to catch up with written entries")
		res.writeTo.mu.Lock()
		for _, readEntry := range res.writeTo.ReadEntries {
			require.Contains(t, lines, readEntry.Line, "not expected log line")
		}
		res.writeTo.mu.Unlock()

		err := res.nextWALSegment()
		require.NoError(t, err, "expected no error when moving to next wal segment")

		for _, line := range linesAfter {
			res.writeEntry(api.Entry{
				Labels: testLabels,
				Entry: logproto.Entry{
					Timestamp: time.Now(),
					Line:      line,
				},
			})
		}
		require.NoError(t, res.syncWAL())
		res.notifyWrite()

		require.Eventually(t, func() bool {
			res.writeTo.mu.Lock()
			defer res.writeTo.mu.Unlock()
			return len(res.writeTo.ReadEntries) == 6
		}, time.Second*10, time.Second, "expected watcher to catch up after new wal segment is cut")
		// assert over second half of entries
		res.writeTo.mu.Lock()
		for _, readEntry := range res.writeTo.ReadEntries[3:] {
			require.Contains(t, linesAfter, readEntry.Line, "not expected log line")
		}
		res.writeTo.mu.Unlock()
	},

	"start reading from last segment": func(t *testing.T, res *watcherTestResources) {
		linesAfter := []string{
			"holis2",
			"holus2",
			"chau2",
		}
		testLabels := model.LabelSet{
			"test": "watcher_read",
		}

		// write something to first segment
		res.writeEntry(api.Entry{
			Labels: testLabels,
			Entry: logproto.Entry{
				Timestamp: time.Now(),
				Line:      "this shouldn't be read",
			},
		})

		require.NoError(t, res.syncWAL())

		err := res.nextWALSegment()
		require.NoError(t, err, "expected no error when moving to next wal segment")

		res.startWatcher()

		for _, line := range linesAfter {
			res.writeEntry(api.Entry{
				Labels: testLabels,
				Entry: logproto.Entry{
					Timestamp: time.Now(),
					Line:      line,
				},
			})
		}
		require.NoError(t, res.syncWAL())

		res.notifyWrite()

		require.Eventually(t, func() bool {
			res.writeTo.mu.Lock()
			defer res.writeTo.mu.Unlock()
			return len(res.writeTo.ReadEntries) == 3
		}, time.Second*10, time.Second, "expected watcher to catch up after new wal segment is cut")
		// assert over second half of entries
		res.writeTo.mu.Lock()
		for _, readEntry := range res.writeTo.ReadEntries[3:] {
			require.Contains(t, linesAfter, readEntry.Line, "not expected log line")
		}
		res.writeTo.mu.Unlock()
	},

	"watcher receives segments reclaimed notifications correctly": func(t *testing.T, res *watcherTestResources) {
		res.startWatcher()
		testLabels := model.LabelSet{
			"test": "watcher_read",
		}

		writeAndWaitForWatcherToCatchUp := func(line string, expectedReadEntries int) {
			res.writeEntry(api.Entry{
				Labels: testLabels,
				Entry: logproto.Entry{
					Timestamp: time.Now(),
					Line:      line,
				},
			})
			require.NoError(t, res.syncWAL())
			res.notifyWrite()
			require.Eventually(t, func() bool {
				res.writeTo.mu.Lock()
				defer res.writeTo.mu.Unlock()
				return len(res.writeTo.ReadEntries) == expectedReadEntries
			}, time.Second*10, time.Second, "expected watcher to catch up with written entries")
		}

		// writing segment 0
		writeAndWaitForWatcherToCatchUp("segment 0", 1)

		// moving to segment 1
		require.NoError(t, res.nextWALSegment(), "expected no error when moving to next wal segment")

		// moving on to segment 1
		writeAndWaitForWatcherToCatchUp("segment 1", 2)

		// collecting segment 0
		res.notifySegmentReclaimed(0)
		require.Eventually(t, func() bool {
			res.writeTo.mu.Lock()
			defer res.writeTo.mu.Unlock()
			return len(res.writeTo.ReceivedSeriesReset) == 1 && res.writeTo.ReceivedSeriesReset[0] == 0
		}, time.Second*10, time.Second, "timed out waiting to receive series reset")

		// moving and writing to segment 2
		require.NoError(t, res.nextWALSegment(), "expected no error when moving to next wal segment")
		writeAndWaitForWatcherToCatchUp("segment 2", 3)
		time.Sleep(time.Millisecond)
		// moving and writing to segment 3
		require.NoError(t, res.nextWALSegment(), "expected no error when moving to next wal segment")
		writeAndWaitForWatcherToCatchUp("segment 3", 4)

		// collecting all segments up to 2
		res.notifySegmentReclaimed(2)
		// Expect second SeriesReset call to have the highest numbered deleted segment, 2
		require.Eventually(t, func() bool {
			res.writeTo.mu.Lock()
			defer res.writeTo.mu.Unlock()
			t.Logf("received series reset: %v", res.writeTo.ReceivedSeriesReset)
			return len(res.writeTo.ReceivedSeriesReset) == 2 && res.writeTo.ReceivedSeriesReset[1] == 2
		}, time.Second*10, time.Second, "timed out waiting to receive series reset")
	},
}

// TestWatcher is the main test function, that works as framework to test different scenarios of the Watcher. It bootstraps
// necessary test components.
func TestWatcher(t *testing.T) {
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			// start test global resources
			reg := prometheus.NewRegistry()
			logger := level.NewFilter(log.NewLogfmtLogger(os.Stdout), level.AllowDebug())
			dir := t.TempDir()
			metrics := NewWatcherMetrics(reg)
			writeTo := &testWriteTo{
				series: map[uint64]model.LabelSet{},
				logger: logger,
			}
			// create new watcher, and defer stop
			watcher := NewWatcher(dir, "test", metrics, writeTo, logger, DefaultWatchConfig)
			defer watcher.Stop()
			wl, err := New(Config{
				Enabled: true,
				Dir:     dir,
			}, logger, reg)
			require.NoError(t, err)
			ew := newEntryWriter()
			// run test case injecting resources
			testCase(
				t,
				&watcherTestResources{
					writeEntry: func(entry api.Entry) {
						_ = ew.WriteEntry(entry, wl, logger)
					},
					notifyWrite: func() {
						watcher.NotifyWrite()
					},
					startWatcher: func() {
						watcher.Start()
					},
					syncWAL: func() error {
						return wl.Sync()
					},
					nextWALSegment: func() error {
						_, err := wl.NextSegment()
						return err
					},
					writeTo: writeTo,
					notifySegmentReclaimed: func(segmentNum int) {
						writeTo.SeriesReset(segmentNum)
					},
				},
			)
		})
	}
}
