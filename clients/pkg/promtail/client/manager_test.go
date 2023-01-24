package client

import (
	"os"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/clients/pkg/promtail/api"
	"github.com/grafana/loki/clients/pkg/promtail/wal"

	"github.com/grafana/loki/pkg/logproto"
)

func TestManager_WALEnabled_EntriesAreWrittenToWAL(t *testing.T) {
	walDir := t.TempDir()
	manager, err := NewManager(prometheus.NewRegistry(), log.NewLogfmtLogger(os.Stdout), wal.Config{
		Dir:     walDir,
		Enabled: true,
	})
	require.NoError(t, err)

	var testLabels = model.LabelSet{
		"wal_enabled": "true",
	}
	var lines = []string{
		"Lorem ipsum dolor sit amet, consectetur adipiscing elit.",
		"In eu nisl ac massa ultricies rutrum.",
		"Sed eget felis at ipsum auctor congue.",
	}
	for _, line := range lines {
		manager.Chan() <- api.Entry{
			Labels: testLabels,
			Entry: logproto.Entry{
				Timestamp: time.Now(),
				Line:      line,
			},
		}
	}

	// stop client to flush WAL
	manager.Stop()

	readEntries, err := wal.ReadWAL(walDir)
	require.NoError(t, err, "error reading wal entries")
	require.Len(t, readEntries, len(lines))
	for _, entry := range readEntries {
		require.Equal(t, testLabels, entry.Labels)
		require.Contains(t, lines, entry.Line)
	}
}
