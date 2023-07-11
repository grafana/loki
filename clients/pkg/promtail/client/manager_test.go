package client

import (
	"fmt"
	"github.com/grafana/loki/clients/pkg/promtail/limit"
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
	"github.com/grafana/loki/clients/pkg/promtail/utils"
	"github.com/grafana/loki/clients/pkg/promtail/wal"

	"github.com/grafana/loki/pkg/logproto"
)

type notifier func(subscriber wal.CleanupEventSubscriber)

var limitsConfig = limit.Config{
	MaxLineSizeTruncate: false,
	MaxStreams:          0,
	MaxLineSize:         0,
}

func (n notifier) SubscribeCleanup(subscriber wal.CleanupEventSubscriber) {
	n(subscriber)
}

func (n notifier) SubscribeWrite(_ wal.WriteEventSubscriber) {
}

func TestManager_ErrorCreatingWhenNoClientConfigsProvided(t *testing.T) {
	walDir := t.TempDir()
	_, err := NewManager(nil, log.NewLogfmtLogger(os.Stdout), limitsConfig, prometheus.NewRegistry(), wal.Config{
		Dir:         walDir,
		Enabled:     true,
		WatchConfig: wal.DefaultWatchConfig,
	}, notifier(func(subscriber wal.CleanupEventSubscriber) {}))
	require.Error(t, err)
}

type closer interface {
	Close()
}

type closerFunc func()

func (c closerFunc) Close() {
	c()
}

func newServerAndClientConfig(t *testing.T) (Config, chan utils.RemoteWriteRequest, closer) {
	receivedReqsChan := make(chan utils.RemoteWriteRequest, 10)

	// Start a local HTTP server
	server := utils.NewRemoteWriteServer(receivedReqsChan, http.StatusOK)
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

func TestManager_WriteAndReadEntriesFromWAL(t *testing.T) {
	walDir := t.TempDir()
	walConfig := wal.Config{
		Dir:           walDir,
		Enabled:       true,
		MaxSegmentAge: time.Second * 10,
		WatchConfig:   wal.DefaultWatchConfig,
	}
	// start all necessary resources
	reg := prometheus.NewRegistry()
	logger := log.NewLogfmtLogger(os.Stdout)
	testClientConfig, rwReceivedReqs, closeServer := newServerAndClientConfig(t)
	clientMetrics := NewMetrics(reg)

	// start writer and manager
	writer, err := wal.NewWriter(walConfig, logger, reg)
	require.NoError(t, err)
	manager, err := NewManager(clientMetrics, logger, limitsConfig, prometheus.NewRegistry(), walConfig, writer, testClientConfig)
	require.NoError(t, err)
	require.Equal(t, "wal:test-client", manager.Name())

	receivedRequests := []utils.RemoteWriteRequest{}
	go func() {
		for req := range rwReceivedReqs {
			receivedRequests = append(receivedRequests, req)
		}
	}()

	defer func() {
		writer.Stop()
		manager.Stop()
		closeServer.Close()
	}()

	var testLabels = model.LabelSet{
		"wal_enabled": "true",
	}
	var totalLines = 100
	for i := 0; i < totalLines; i++ {
		writer.Chan() <- api.Entry{
			Labels: testLabels,
			Entry: logproto.Entry{
				Timestamp: time.Now(),
				Line:      fmt.Sprintf("line%d", i),
			},
		}
	}

	require.Eventually(t, func() bool {
		return len(receivedRequests) == totalLines
	}, 5*time.Second, time.Second, "timed out waiting for requests to be received")

	var seenEntries = map[string]struct{}{}
	// assert over rw client received entries
	for _, req := range receivedRequests {
		require.Len(t, req.Request.Streams, 1, "expected 1 stream requests to be received")
		require.Len(t, req.Request.Streams[0].Entries, 1, "expected 1 entry in the only stream received per request")
		require.Equal(t, `{wal_enabled="true"}`, req.Request.Streams[0].Labels)
		seenEntries[req.Request.Streams[0].Entries[0].Line] = struct{}{}
	}
	require.Len(t, seenEntries, totalLines)
}

func TestManager_DryRun(t *testing.T) {
	walDir := t.TempDir()
	walConfig := wal.Config{
		Dir:           walDir,
		Enabled:       true,
		MaxSegmentAge: time.Second * 10,
		WatchConfig:   wal.DefaultWatchConfig,
	}
	// start all necessary resources
	reg := prometheus.NewRegistry()
	logger := log.NewLogfmtLogger(os.Stdout)
	testClientConfig, rwReceivedReqs, closeServer := newServerAndClientConfig(t)
	clientMetrics := NewMetrics(reg)

	// start writer and manager
	writer, err := wal.NewWriter(walConfig, logger, reg)
	require.NoError(t, err)
	manager, err := NewManager(clientMetrics, logger, limitsConfig, prometheus.NewRegistry(), walConfig, writer, testClientConfig)
	require.NoError(t, err)
	require.Equal(t, "wal:test-client", manager.Name())

	receivedRequests := []utils.RemoteWriteRequest{}
	go func() {
		for req := range rwReceivedReqs {
			receivedRequests = append(receivedRequests, req)
		}
	}()

	defer func() {
		writer.Stop()
		manager.Stop()
		closeServer.Close()
	}()

	var testLabels = model.LabelSet{
		"wal_enabled": "true",
	}
	var totalLines = 100
	for i := 0; i < totalLines; i++ {
		writer.Chan() <- api.Entry{
			Labels: testLabels,
			Entry: logproto.Entry{
				Timestamp: time.Now(),
				Line:      fmt.Sprintf("line%d", i),
			},
		}
	}

	require.Eventually(t, func() bool {
		return len(receivedRequests) == totalLines
	}, 5*time.Second, time.Second, "timed out waiting for requests to be received")

	var seenEntries = map[string]struct{}{}
	// assert over rw client received entries
	for _, req := range receivedRequests {
		require.Len(t, req.Request.Streams, 1, "expected 1 stream requests to be received")
		require.Len(t, req.Request.Streams[0].Entries, 1, "expected 1 entry in the only stream received per request")
		require.Equal(t, `{wal_enabled="true"}`, req.Request.Streams[0].Labels)
		seenEntries[req.Request.Streams[0].Entries[0].Line] = struct{}{}
	}
	require.Len(t, seenEntries, totalLines)
}
