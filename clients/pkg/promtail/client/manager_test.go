package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/clients/pkg/promtail/api"
	"github.com/grafana/loki/v3/clients/pkg/promtail/client/fake"
	"github.com/grafana/loki/v3/clients/pkg/promtail/limit"
	"github.com/grafana/loki/v3/clients/pkg/promtail/utils"
	"github.com/grafana/loki/v3/clients/pkg/promtail/wal"

	"github.com/grafana/loki/v3/pkg/logproto"
	lokiflag "github.com/grafana/loki/v3/pkg/util/flagext"
)

var testLimitsConfig = limit.Config{
	MaxLineSizeTruncate: false,
	MaxStreams:          0,
	MaxLineSize:         0,
}

var (
	nilMetrics = NewMetrics(nil)
	metrics    = NewMetrics(prometheus.DefaultRegisterer)
)

func TestManager_ErrorCreatingWhenNoClientConfigsProvided(t *testing.T) {
	for _, walEnabled := range []bool{true, false} {
		t.Run(fmt.Sprintf("wal-enabled = %t", walEnabled), func(t *testing.T) {
			walDir := t.TempDir()
			_, err := NewManager(nilMetrics, log.NewLogfmtLogger(os.Stdout), testLimitsConfig, prometheus.NewRegistry(), wal.Config{
				Dir:         walDir,
				Enabled:     walEnabled,
				WatchConfig: wal.DefaultWatchConfig,
			}, NilNotifier)
			require.Error(t, err)
		})
	}
}

func TestManager_ErrorCreatingWhenRepeatedConfigs(t *testing.T) {
	host1, _ := url.Parse("http://localhost:3100")
	config1 := Config{
		BatchSize:      20,
		BatchWait:      1 * time.Second,
		URL:            flagext.URLValue{URL: host1},
		ExternalLabels: lokiflag.LabelSet{LabelSet: model.LabelSet{"order": "yaml"}},
	}
	config1Copy := config1
	for _, walEnabled := range []bool{true, false} {
		t.Run(fmt.Sprintf("wal-enabled = %t", walEnabled), func(t *testing.T) {
			walDir := t.TempDir()
			_, err := NewManager(nilMetrics, log.NewLogfmtLogger(os.Stdout), testLimitsConfig, prometheus.NewRegistry(), wal.Config{
				Dir:         walDir,
				Enabled:     walEnabled,
				WatchConfig: wal.DefaultWatchConfig,
			}, NilNotifier, config1, config1Copy)
			require.Error(t, err)
		})
	}
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

func TestManager_WALEnabled(t *testing.T) {
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
	manager, err := NewManager(clientMetrics, logger, testLimitsConfig, prometheus.NewRegistry(), walConfig, writer, testClientConfig)
	require.NoError(t, err)
	require.Equal(t, "wal:test-client", manager.Name())

	var mu sync.Mutex
	receivedRequests := []utils.RemoteWriteRequest{}
	go func() {
		for req := range rwReceivedReqs {
			mu.Lock()
			receivedRequests = append(receivedRequests, req)
			mu.Unlock()
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
		mu.Lock()
		defer mu.Unlock()
		return len(receivedRequests) == totalLines
	}, 5*time.Second, time.Second, "timed out waiting for requests to be received")

	var seenEntries = map[string]struct{}{}
	// assert over rw client received entries
	mu.Lock()
	for _, req := range receivedRequests {
		require.Len(t, req.Request.Streams, 1, "expected 1 stream requests to be received")
		require.Len(t, req.Request.Streams[0].Entries, 1, "expected 1 entry in the only stream received per request")
		require.Equal(t, `{wal_enabled="true"}`, req.Request.Streams[0].Labels)
		seenEntries[req.Request.Streams[0].Entries[0].Line] = struct{}{}
	}
	mu.Unlock()
	require.Len(t, seenEntries, totalLines)
}

func TestManager_WALDisabled(t *testing.T) {
	walConfig := wal.Config{}
	// start all necessary resources
	reg := prometheus.NewRegistry()
	logger := log.NewLogfmtLogger(os.Stdout)
	testClientConfig, rwReceivedReqs, closeServer := newServerAndClientConfig(t)
	clientMetrics := NewMetrics(reg)

	// start writer and manager
	manager, err := NewManager(clientMetrics, logger, testLimitsConfig, prometheus.NewRegistry(), walConfig, NilNotifier, testClientConfig)
	require.NoError(t, err)
	require.Equal(t, "multi:test-client", manager.Name())

	var mu sync.Mutex
	receivedRequests := []utils.RemoteWriteRequest{}
	go func() {
		for req := range rwReceivedReqs {
			mu.Lock()
			receivedRequests = append(receivedRequests, req)
			mu.Unlock()
		}
	}()

	defer func() {
		manager.Stop()
		closeServer.Close()
	}()

	var testLabels = model.LabelSet{
		"pizza-flavour": "fugazzeta",
	}
	var totalLines = 100
	for i := 0; i < totalLines; i++ {
		manager.Chan() <- api.Entry{
			Labels: testLabels,
			Entry: logproto.Entry{
				Timestamp: time.Now(),
				Line:      fmt.Sprintf("line%d", i),
			},
		}
	}

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(receivedRequests) == totalLines
	}, 5*time.Second, time.Second, "timed out waiting for requests to be received")

	var seenEntries = map[string]struct{}{}
	// assert over rw client received entries
	mu.Lock()
	for _, req := range receivedRequests {
		require.Len(t, req.Request.Streams, 1, "expected 1 stream requests to be received")
		require.Len(t, req.Request.Streams[0].Entries, 1, "expected 1 entry in the only stream received per request")
		require.Equal(t, `{pizza-flavour="fugazzeta"}`, req.Request.Streams[0].Labels)
		seenEntries[req.Request.Streams[0].Entries[0].Line] = struct{}{}
	}
	mu.Unlock()
	require.Len(t, seenEntries, totalLines)
}

func TestManager_WALDisabled_MultipleConfigs(t *testing.T) {
	walConfig := wal.Config{}
	// start all necessary resources
	reg := prometheus.NewRegistry()
	logger := log.NewLogfmtLogger(os.Stdout)
	testClientConfig, rwReceivedReqs, closeServer := newServerAndClientConfig(t)
	// add client identifier to entry
	testClientConfig.ExternalLabels = lokiflag.LabelSet{
		LabelSet: model.LabelSet{
			"client-name": "test-client",
		},
	}
	testClientConfig2, rwReceivedReqs2, closeServer2 := newServerAndClientConfig(t)
	testClientConfig2.Name = "test-client-2"
	// add client identifier to entry
	testClientConfig2.ExternalLabels = lokiflag.LabelSet{
		LabelSet: model.LabelSet{
			"client-name": "test-client-2",
		},
	}
	clientMetrics := NewMetrics(reg)

	// start writer and manager
	manager, err := NewManager(clientMetrics, logger, testLimitsConfig, prometheus.NewRegistry(), walConfig, NilNotifier, testClientConfig, testClientConfig2)
	require.NoError(t, err)
	require.Equal(t, "multi:test-client,test-client-2", manager.Name())

	var mu sync.Mutex
	receivedRequests := []utils.RemoteWriteRequest{}
	ctx, cancel := context.WithCancel(context.Background())
	go func(ctx context.Context) {
		for {
			select {
			case req := <-rwReceivedReqs:
				mu.Lock()
				receivedRequests = append(receivedRequests, req)
				mu.Unlock()
			case req := <-rwReceivedReqs2:
				mu.Lock()
				receivedRequests = append(receivedRequests, req)
				mu.Unlock()
			case <-ctx.Done():
				return
			}
		}
	}(ctx)

	defer func() {
		manager.Stop()
		closeServer.Close()
		closeServer2.Close()
		cancel()
	}()

	var testLabels = model.LabelSet{
		"pizza-flavour": "fugazzeta",
	}
	var totalLines = 100
	for i := 0; i < totalLines; i++ {
		manager.Chan() <- api.Entry{
			Labels: testLabels,
			Entry: logproto.Entry{
				Timestamp: time.Now(),
				Line:      fmt.Sprintf("line%d", i),
			},
		}
	}

	// times 2 due to clients being run
	expectedTotalLines := totalLines * 2
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(receivedRequests) == expectedTotalLines
	}, 5*time.Second, time.Second, "timed out waiting for requests to be received")

	var seenEntries = map[string]struct{}{}
	// assert over rw client received entries
	mu.Lock()
	for _, req := range receivedRequests {
		require.Len(t, req.Request.Streams, 1, "expected 1 stream requests to be received")
		require.Len(t, req.Request.Streams[0].Entries, 1, "expected 1 entry in the only stream received per request")
		seenEntries[fmt.Sprintf("%s-%s", req.Request.Streams[0].Labels, req.Request.Streams[0].Entries[0].Line)] = struct{}{}
	}
	mu.Unlock()
	require.Len(t, seenEntries, expectedTotalLines)
}

func TestManager_StopClients(t *testing.T) {
	var stopped int

	stopping := func() {
		stopped++
	}
	fc := fake.New(stopping)
	clients := []Client{fc, fc, fc, fc}
	m := &Manager{
		clients: clients,
		entries: make(chan api.Entry),
	}
	m.startWithForward()
	m.Stop()

	if stopped != len(clients) {
		t.Fatal("missing stop call")
	}
}
