package client

import (
	"fmt"
	"net/url"
	"os"
	"path"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/loki/clients/pkg/promtail/api"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/tsdb/record"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/ingester"
	"github.com/grafana/loki/pkg/logproto"
	util_log "github.com/grafana/loki/pkg/util/log"
)

type simpleConsumer struct {
	Series  []record.RefSeries
	Entries []ingester.RefEntries
}

func newSimpleConsumer() *simpleConsumer {
	return &simpleConsumer{
		Series:  make([]record.RefSeries, 0),
		Entries: make([]ingester.RefEntries, 0),
	}
}

func (s *simpleConsumer) ConsumeSeries(series record.RefSeries) error {
	s.Series = append(s.Series, series)
	return nil
}

func (s *simpleConsumer) ConsumeEntries(entries ingester.RefEntries) error {
	s.Entries = append(s.Entries, entries)
	return nil
}

func (s *simpleConsumer) SegmentEnd(segmentNum int) {
	//TODO implement me
	panic("implement me")
}

func (s *simpleConsumer) Done() <-chan struct{} {
	//TODO implement me
	panic("implement me")
}

func TestWatcher(t *testing.T) {
	logger := log.NewLogfmtLogger(os.Stdout)
	dir := t.TempDir()
	w, err := newWAL(logger, nil, WALConfig{
		Dir:     dir,
		Enabled: true,
	}, "test_client", "123")
	require.NoError(t, err, "error starting wal")
	defer func() {
		_ = w.Delete()
	}()

	watchedWALConsumer := newSimpleConsumer()
	watcher := NewWALWatcher(w.Dir(), watchedWALConsumer, logger)
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
	writeEntryToWAL(testEntry, w, "123", logger)
	require.NoError(t, w.Sync(), "failed to sync wal")

	require.NoError(t, WaitForCondition(func() bool {
		return len(watchedWALConsumer.Entries) == 1
	}, 100*time.Millisecond, 3*time.Second))
}

func Test_WAL_WriteAndConsume(t *testing.T) {
	dir := t.TempDir()
	c, err := New(nilMetrics, Config{
		Name:      "foo",
		URL:       flagext.URLValue{URL: &url.URL{}},
		BatchWait: 100 * time.Millisecond,
		BatchSize: int(1e6),
		WAL: WALConfig{
			Enabled: true,
			Dir:     dir,
		},
	}, nil, 0, log.NewNopLogger())
	require.NoError(t, err)
	sync, err := getSyncCall(c)
	require.NoError(t, err, "client does not implement sync")

	// defer c.Stop()
	c.Chan() <- api.Entry{
		Labels: model.LabelSet{
			"foo": "bar",
		},
		Entry: logproto.Entry{
			Line: "test",
		},
	}
	c.Chan() <- api.Entry{
		Labels: model.LabelSet{
			ReservedLabelTenantID: "tenant1",
		},
		Entry: logproto.Entry{
			Line: "test 2",
		},
	}
	expectedWALPath := path.Join(
		dir,       // wal base dir
		"foo",     // client config name
		"tenant1", // tenant ID
	)
	t.Logf("Expected wal path: %s", expectedWALPath)

	consumer := newSimpleConsumer()
	watcher := NewWALWatcher(expectedWALPath, consumer, util_log.Logger)
	watcher.Start()
	defer watcher.Stop()

	// testing logs
	dirs, err := os.ReadDir(expectedWALPath)
	require.NoError(t, err)
	t.Logf("files in wal dir: %v", dirs)

	require.NoError(t, sync(), "failed to force wal to sync")

	require.Len(t, consumer.Entries, 2)

	// todo assert as well over the series
	t.Logf("series: %v", consumer.Series)
	t.Logf("entries: %v", consumer.Entries)
}

type syncer interface {
	Sync() error
}

// getSyncCall extracts the WAL sync method from the Client implementation, used for doing an fsync on the WAL
// when testing.
func getSyncCall(client Client) (func() error, error) {
	if clientSync, ok := client.(syncer); ok {
		return clientSync.Sync, nil
	}
	return nil, fmt.Errorf("client does not implement Sync() error")
}

func Test_WAL_WriteAfterWatcherIsStarted(t *testing.T) {
	dir := t.TempDir()
	expectedWALPath := path.Join(
		dir,       // wal base dir
		"foo",     // client config name
		"tenant1", // tenant ID
	)
	t.Logf("Expected wal path: %s", expectedWALPath)

	consumer := newSimpleConsumer()

	stdOutLog := log.NewLogfmtLogger(os.Stdout)
	watcher := NewWALWatcher(expectedWALPath, consumer, stdOutLog)
	watcher.Start()
	defer watcher.Stop()

	c, err := New(nilMetrics, Config{
		Name:      "foo",
		URL:       flagext.URLValue{URL: &url.URL{}},
		BatchWait: 100 * time.Millisecond,
		BatchSize: int(1e6),
		WAL: WALConfig{
			Enabled: true,
			Dir:     dir,
		},
	}, nil, 0, log.NewNopLogger())
	require.NoError(t, err)
	sync, err := getSyncCall(c)
	require.NoError(t, err)

	// defer c.Stop()
	c.Chan() <- api.Entry{
		Labels: model.LabelSet{
			"foo": "bar",
		},
		Entry: logproto.Entry{
			Line: "test",
		},
	}
	c.Chan() <- api.Entry{
		Labels: model.LabelSet{
			ReservedLabelTenantID: "tenant1",
		},
		Entry: logproto.Entry{
			Line: "test 2",
		},
	}

	require.NoError(t, sync())
	// Since the wal watcher runs on a loop every 20msec for an open segment read, need a small Sleep here
	require.NoError(t, WaitForCondition(func() bool {
		return len(consumer.Entries) > 0
	}, 100*time.Millisecond, time.Second))

	require.Len(t, consumer.Entries, 2)

	// todo assert as well over the series
	t.Logf("series: %v", consumer.Series)
	t.Logf("entries: %v", consumer.Entries)
}

type Condition func() bool

func WaitForCondition(cond Condition, checkEvery, timeout time.Duration) error {
	checkTicker := time.NewTicker(checkEvery)
	defer checkTicker.Stop()
	timeoutFire := time.After(timeout)
	for {
		select {
		case <-checkTicker.C:
			// perform check, and succeed if condition is met
			if cond() {
				return nil
			}
		case <-timeoutFire:
			return fmt.Errorf("failed to meet condition")
		}
	}
}
