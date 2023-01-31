package wal

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/clients/pkg/promtail/api"
	"github.com/grafana/loki/pkg/logproto"
)

func TestWriter_EntriesAreWrittenToWALAndForwardedToClients(t *testing.T) {
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

func TestWriter_OldSegmentsAreCleanedUp(t *testing.T) {
	logger := level.NewFilter(log.NewLogfmtLogger(os.Stdout), level.AllowDebug())
	dir := t.TempDir()

	maxSegmentAge := time.Second * 2

	writer, err := NewWriter(Config{
		Dir:           dir,
		Enabled:       true,
		MaxSegmentAge: maxSegmentAge,
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
	// Expect last, or "head" segment to still be alive
	_, err = os.Stat(filepath.Join(dir, "00000001"))
	require.NoError(t, err)
}

func TestWriter_NoSegmentIsCleanedUpIfTheresOnlyOne(t *testing.T) {
	logger := level.NewFilter(log.NewLogfmtLogger(os.Stdout), level.AllowDebug())
	dir := t.TempDir()

	maxSegmentAge := time.Second * 2

	writer, err := NewWriter(Config{
		Dir:           dir,
		Enabled:       true,
		MaxSegmentAge: maxSegmentAge,
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
