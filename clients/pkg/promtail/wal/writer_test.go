package wal

import (
	"os"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/clients/pkg/promtail/api"

	"github.com/grafana/loki/pkg/logproto"
)

func TestWriter(t *testing.T) {
	logger := log.NewLogfmtLogger(os.Stdout)
	dir := t.TempDir()
	wl, err := New(Config{
		Dir:     dir,
		Enabled: true,
	}, logger, prometheus.NewRegistry())
	require.NoError(t, err)

	writer := NewWriter(wl, logger)
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

	readEntries, err := ReadWAL(dir)
	require.NoError(t, err)
	require.Len(t, readEntries, len(lines), "written lines and seen differ")
	require.Equal(t, testLabels, readEntries[0].Labels)
}
