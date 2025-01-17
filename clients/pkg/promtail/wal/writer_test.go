package wal

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/clients/pkg/promtail/api"

	"github.com/grafana/loki/v3/pkg/logproto"
)

func TestWriter_EntriesAreWrittenToWAL(t *testing.T) {
	logger := log.NewLogfmtLogger(os.Stdout)
	dir := t.TempDir()

	writer, err := NewWriter(Config{
		Dir:           dir,
		Enabled:       true,
		MaxSegmentAge: time.Minute,
	}, logger, prometheus.NewRegistry())
	require.NoError(t, err)
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

	// accessing the WAL inside, just for testing!
	require.NoError(t, writer.wal.Sync(), "failed to sync wal")

	// assert over WAL entries
	readEntries, err := ReadWAL(dir)
	require.NoError(t, err)
	require.Len(t, readEntries, len(lines), "written lines and seen differ")
	require.Equal(t, testLabels, readEntries[0].Labels)
}

type notifySegmentsCleanedFunc func(num int)

func (n notifySegmentsCleanedFunc) NotifyWrite() {
}

func (n notifySegmentsCleanedFunc) SeriesReset(segmentNum int) {
	n(segmentNum)
}

func TestWriter_OldSegmentsAreCleanedUp(t *testing.T) {
	logger := level.NewFilter(log.NewLogfmtLogger(os.Stdout), level.AllowDebug())
	dir := t.TempDir()

	maxSegmentAge := time.Second * 2

	var mu1 sync.Mutex
	var mu2 sync.Mutex
	subscriber1 := []int{}
	subscriber2 := []int{}

	writer, err := NewWriter(Config{
		Dir:           dir,
		Enabled:       true,
		MaxSegmentAge: maxSegmentAge,
	}, logger, prometheus.NewRegistry())
	require.NoError(t, err)
	defer func() {
		writer.Stop()
	}()

	// add writer events subscriber. Add multiple to test fanout
	writer.SubscribeCleanup(notifySegmentsCleanedFunc(func(num int) {
		mu1.Lock()
		subscriber1 = append(subscriber1, num)
		mu1.Unlock()
	}))
	writer.SubscribeCleanup(notifySegmentsCleanedFunc(func(num int) {
		mu2.Lock()
		subscriber2 = append(subscriber2, num)
		mu2.Unlock()
	}))

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

	// accessing the WAL inside, just for testing!
	require.NoError(t, writer.wal.Sync(), "failed to sync wal")

	// assert over WAL entries
	readEntries, err := ReadWAL(dir)
	require.NoError(t, err)
	require.Len(t, readEntries, len(lines), "written lines and seen differ")
	require.Equal(t, testLabels, readEntries[0].Labels)

	watchAndLogDirEntries(t, dir)

	// check segment is there
	fileInfo, err := os.Stat(filepath.Join(dir, "00000000"))
	require.NoError(t, err)
	require.GreaterOrEqual(t, fileInfo.Size(), int64(0), "first segment size should be >= 0")

	// force close segment, so that one is eventually cleaned up
	_, err = writer.wal.NextSegment()
	require.NoError(t, err, "error closing current segment")

	// wait for segment to be cleaned
	time.Sleep(maxSegmentAge * 2)

	watchAndLogDirEntries(t, dir)

	_, err = os.Stat(filepath.Join(dir, "00000000"))
	require.Error(t, err)
	require.ErrorIs(t, err, os.ErrNotExist, "expected file not exists error")

	// assert all subscribers were notified
	mu1.Lock()
	require.Len(t, subscriber1, 1, "expected one segment reclaimed notification in subscriber1")
	require.Equal(t, 0, subscriber1[0])
	mu1.Unlock()

	mu2.Lock()
	require.Len(t, subscriber2, 1, "expected one segment reclaimed notification in subscriber2")
	require.Equal(t, 0, subscriber2[0])
	mu2.Unlock()

	// Expect last, or "head" segment to still be alive
	_, err = os.Stat(filepath.Join(dir, "00000001"))
	require.NoError(t, err)
}

func TestWriter_NoSegmentIsCleanedUpIfTheresOnlyOne(t *testing.T) {
	logger := level.NewFilter(log.NewLogfmtLogger(os.Stdout), level.AllowDebug())
	dir := t.TempDir()

	maxSegmentAge := time.Second * 2

	segmentsReclaimedNotificationsReceived := []int{}

	writer, err := NewWriter(Config{
		Dir:           dir,
		Enabled:       true,
		MaxSegmentAge: maxSegmentAge,
	}, logger, prometheus.NewRegistry())
	require.NoError(t, err)
	defer func() {
		writer.Stop()
	}()

	// add writer events subscriber
	writer.SubscribeCleanup(notifySegmentsCleanedFunc(func(num int) {
		segmentsReclaimedNotificationsReceived = append(segmentsReclaimedNotificationsReceived, num)
	}))

	// write entries to wal and sync
	var testLabels = model.LabelSet{
		"testing": "log",
	}
	var lines = []string{
		"some line",
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

	// accessing the WAL inside, just for testing!
	require.NoError(t, writer.wal.Sync(), "failed to sync wal")

	// assert over WAL entries
	readEntries, err := ReadWAL(dir)
	require.NoError(t, err)
	require.Len(t, readEntries, len(lines), "written lines and seen differ")
	require.Equal(t, testLabels, readEntries[0].Labels)

	watchAndLogDirEntries(t, dir)

	// check segment is there
	fileInfo, err := os.Stat(filepath.Join(dir, "00000000"))
	require.NoError(t, err)
	require.GreaterOrEqual(t, fileInfo.Size(), int64(0), "first segment size should be >= 0")

	// wait for segment to be cleaned
	time.Sleep(maxSegmentAge * 2)

	watchAndLogDirEntries(t, dir)

	_, err = os.Stat(filepath.Join(dir, "00000000"))
	require.NoError(t, err)
	require.Len(t, segmentsReclaimedNotificationsReceived, 0, "expected no notification")
}

func watchAndLogDirEntries(t *testing.T, path string) {
	dirs, err := os.ReadDir(path)
	if len(dirs) == 0 {
		t.Log("no dirs found")
		return
	}
	require.NoError(t, err)
	for _, dir := range dirs {
		t.Logf("dir entry found: %s", dir.Name())
	}
}

func BenchmarkWriter_WriteEntries(b *testing.B) {
	type testCase struct {
		lines          int
		labelSetsCount int
	}
	var cases = []testCase{
		{
			lines:          1000,
			labelSetsCount: 1,
		},
		{
			lines:          1000,
			labelSetsCount: 4,
		},
		{
			lines:          1e6,
			labelSetsCount: 1,
		},
		{
			lines:          1e6,
			labelSetsCount: 100,
		},
		{
			lines:          1e7,
			labelSetsCount: 1,
		},
		{
			lines:          1e7,
			labelSetsCount: 1e3,
		},
	}
	for _, testCase := range cases {
		b.Run(fmt.Sprintf("%d lines, %d different label sets", testCase.lines, testCase.labelSetsCount), func(b *testing.B) {
			for n := 0; n < b.N; n++ {
				benchWriteEntries(b, testCase.lines, testCase.labelSetsCount)
			}
		})
	}
}

func benchWriteEntries(b *testing.B, lines, labelSetCount int) {
	logger := log.NewLogfmtLogger(os.Stdout)
	dir := b.TempDir()

	writer, err := NewWriter(Config{
		Dir:           dir,
		Enabled:       true,
		MaxSegmentAge: time.Minute,
	}, logger, prometheus.NewRegistry())
	require.NoError(b, err)
	defer func() {
		writer.Stop()
	}()

	for i := 0; i < lines; i++ {
		writer.Chan() <- api.Entry{
			Labels: model.LabelSet{
				"someLabel": model.LabelValue(fmt.Sprint(i % labelSetCount)),
			},
			Entry: logproto.Entry{
				Timestamp: time.Now(),
				Line:      fmt.Sprintf("some line being written %d", i),
			},
		}
	}
}
