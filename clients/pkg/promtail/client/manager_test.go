package client

import (
	"fmt"
	"github.com/grafana/loki/clients/pkg/promtail/client/fake"
	"github.com/grafana/loki/clients/pkg/promtail/limit"
	lokiflag "github.com/grafana/loki/pkg/util/flagext"
	util_log "github.com/grafana/loki/pkg/util/log"
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

func (n notifier) SubscribeCleanup(subscriber wal.CleanupEventSubscriber) {
	n(subscriber)
}

func (n notifier) SubscribeWrite(_ wal.WriteEventSubscriber) {
}

var limitsConfig = limit.Config{
	MaxLineSizeTruncate: false,
	MaxStreams:          0,
	MaxLineSize:         0,
}

var (
	nilMetrics  = NewMetrics(nil)
	nilNotifier = notifier(func(_ wal.CleanupEventSubscriber) {

	})
	metrics = NewMetrics(prometheus.DefaultRegisterer)
)

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

func TestNewMulti(t *testing.T) {
	_, err := NewManager(nilMetrics, util_log.Logger, limitsConfig, prometheus.NewRegistry(), wal.Config{Enabled: false}, nilNotifier, []Config{}...)
	require.Error(t, err)

	host1, _ := url.Parse("http://localhost:3100")
	host2, _ := url.Parse("https://grafana.com")
	cc1 := Config{
		BatchSize:      20,
		BatchWait:      1 * time.Second,
		URL:            flagext.URLValue{URL: host1},
		ExternalLabels: lokiflag.LabelSet{LabelSet: model.LabelSet{"order": "yaml"}},
	}
	cc2 := Config{
		BatchSize:      10,
		BatchWait:      1 * time.Second,
		URL:            flagext.URLValue{URL: host2},
		ExternalLabels: lokiflag.LabelSet{LabelSet: model.LabelSet{"hi": "there"}},
	}

	manager, err := NewManager(nilMetrics, util_log.Logger, limitsConfig, prometheus.NewRegistry(), wal.Config{Enabled: false}, nilNotifier, cc1, cc2)
	require.NoError(t, err)
	require.Len(t, manager.clients, 2, "expected two clients")
	require.Equal(t, Config{
		BatchSize:      20,
		BatchWait:      1 * time.Second,
		URL:            flagext.URLValue{URL: host1},
		ExternalLabels: lokiflag.LabelSet{LabelSet: model.LabelSet{"order": "yaml"}},
	}, manager.clients[0].(*client).cfg)
}

func TestNewMulti_BlockDuplicates(t *testing.T) {
	host1, _ := url.Parse("http://localhost:3100")
	cc1 := Config{
		BatchSize:      20,
		BatchWait:      1 * time.Second,
		URL:            flagext.URLValue{URL: host1},
		ExternalLabels: lokiflag.LabelSet{LabelSet: model.LabelSet{"order": "yaml"}},
	}
	cc1Copy := cc1

	_, err := NewManager(
		nilMetrics,
		util_log.Logger,
		limitsConfig,
		prometheus.NewRegistry(),
		wal.Config{Enabled: false},
		nilNotifier,
		cc1,
		cc1Copy,
	)
	require.Error(t, err, "expected NewMulti to reject duplicate client configs")

	cc1Copy.Name = "copy"
	manager, err := NewManager(
		nilMetrics,
		util_log.Logger,
		limitsConfig,
		prometheus.NewRegistry(),
		wal.Config{Enabled: false},
		nilNotifier,
		cc1,
		cc1Copy,
	)
	require.NoError(t, err, "expected NewMulti to reject duplicate client configs")

	require.Len(t, manager.clients, 2)
	require.Equal(t, Config{
		BatchSize:      20,
		BatchWait:      1 * time.Second,
		URL:            flagext.URLValue{URL: host1},
		ExternalLabels: lokiflag.LabelSet{LabelSet: model.LabelSet{"order": "yaml"}},
	}, manager.clients[0].(*client).cfg)
}

func TestMultiClient_Stop(t *testing.T) {
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

func TestMultiClient_Handle(t *testing.T) {
	f := fake.New(func() {})
	clients := []Client{f, f, f, f, f, f}
	m := &Manager{
		clients: clients,
		entries: make(chan api.Entry),
	}
	m.startWithForward()

	m.Chan() <- api.Entry{Labels: model.LabelSet{"foo": "bar"}, Entry: logproto.Entry{Line: "foo"}}

	m.Stop()

	if len(f.Received()) != len(clients) {
		t.Fatal("missing handle call")
	}
}

func TestMultiClient_Handle_Race(t *testing.T) {
	u := flagext.URLValue{}
	require.NoError(t, u.Set("http://localhost"))
	c1, err := New(nilMetrics, Config{URL: u, BackoffConfig: backoff.Config{MaxRetries: 1}, Timeout: time.Microsecond}, 0, 0, false, log.NewNopLogger())
	require.NoError(t, err)
	c2, err := New(nilMetrics, Config{URL: u, BackoffConfig: backoff.Config{MaxRetries: 1}, Timeout: time.Microsecond}, 0, 0, false, log.NewNopLogger())
	require.NoError(t, err)
	clients := []Client{c1, c2}
	m := &Manager{
		clients: clients,
		entries: make(chan api.Entry),
	}
	m.startWithForward()

	m.Chan() <- api.Entry{Labels: model.LabelSet{"foo": "bar", ReservedLabelTenantID: "1"}, Entry: logproto.Entry{Line: "foo"}}

	m.Stop()
}
