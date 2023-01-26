package client

import (
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/clients/pkg/promtail/api"
	"github.com/grafana/loki/clients/pkg/promtail/wal"

	"github.com/grafana/loki/pkg/logproto"
)

func TestManager_ErrorCreatingWhenNoClientConfigsProvided(t *testing.T) {
	walDir := t.TempDir()
	_, err := NewManager(nil, log.NewLogfmtLogger(os.Stdout), 0, 0, prometheus.NewRegistry(), wal.Config{
		Dir:     walDir,
		Enabled: true,
	})
	require.Error(t, err)
}

type closer interface {
	Close()
}

type closerFunc func()

func (c closerFunc) Close() {
	c()
}

func newServerAncClientConfig(t *testing.T) (Config, chan receivedReq, closer) {
	receivedReqsChan := make(chan receivedReq, 10)

	// Start a local HTTP server
	server := newTestRemoteWriteServer(receivedReqsChan, http.StatusOK)
	require.NotNil(t, server)

	testClientURL, _ := url.Parse(server.URL)
	testClientConfig := Config{
		Name:      "test-client",
		URL:       flagext.URLValue{URL: testClientURL},
		Timeout:   time.Second * 2,
		BatchSize: 1,
		BackoffConfig: backoff.Config{
			MaxRetries: 0,
		},
	}
	return testClientConfig, receivedReqsChan, closerFunc(func() {
		server.Close()
		close(receivedReqsChan)
	})
}

func TestManager_WALEnabled_EntriesAreWrittenToWALAndClients(t *testing.T) {
	walDir := t.TempDir()
	reg := prometheus.NewRegistry()
	testClientConfig, rwReceivedReqs, closeServer := newServerAncClientConfig(t)
	clientMetrics := NewMetrics(reg)
	manager, err := NewManager(clientMetrics, log.NewLogfmtLogger(os.Stdout), 0, 0, reg, wal.Config{
		Dir:     walDir,
		Enabled: true,
	}, testClientConfig)
	require.NoError(t, err)
	require.Equal(t, "wal:test-client", manager.Name())

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

	// stop client to flush WAL, stop rw server
	manager.Stop()
	closeServer.Close()

	// assert over entries written to WAL
	readEntries, err := wal.ReadWAL(walDir)
	require.NoError(t, err, "error reading wal entries")
	require.Len(t, readEntries, len(lines))
	for _, entry := range readEntries {
		require.Equal(t, testLabels, entry.Labels)
		require.Contains(t, lines, entry.Line)
	}
	// assert over rw client received entries
	rwSeenEntriesCount := 0
	for req := range rwReceivedReqs {
		rwSeenEntriesCount++
		require.Len(t, req.pushReq.Streams, 1, "expected 1 stream requests to be received")
		require.Len(t, req.pushReq.Streams[0].Entries, 1, "expected 1 entry in the only stream received per request")
		require.Equal(t, `{wal_enabled="true"}`, req.pushReq.Streams[0].Labels)
		require.Contains(t, lines, req.pushReq.Streams[0].Entries[0].Line)
	}
	require.Equal(t, 3, rwSeenEntriesCount)
}
