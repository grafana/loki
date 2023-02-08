package promtail

import (
	"fmt"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/loki/clients/pkg/logentry/stages"
	"github.com/grafana/loki/clients/pkg/promtail/client"
	"github.com/grafana/loki/clients/pkg/promtail/config"
	"github.com/grafana/loki/clients/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/clients/pkg/promtail/wal"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/push"
	"github.com/grafana/loki/pkg/util"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/discovery"
	"github.com/prometheus/prometheus/discovery/targetgroup"
	"github.com/stretchr/testify/require"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type receivedReq struct {
	tenantID string
	pushReq  logproto.PushRequest
}

// newTestMoreWriteServer creates and starts a new httpserver.Server that can handle remote write request. When a request is handled,
// the received entries are written to receivedChan, and status is responded.
func newTestRemoteWriteServer(receivedChan chan receivedReq, status int) *httptest.Server {
	server := httptest.NewServer(createServerHandler(receivedChan, status))
	return server
}

func createServerHandler(receivedReqsChan chan receivedReq, receivedOKStatus int) http.HandlerFunc {
	return func(rw http.ResponseWriter, req *http.Request) {
		// Parse the request
		var pushReq logproto.PushRequest
		if err := util.ParseProtoReader(req.Context(), req.Body, int(req.ContentLength), math.MaxInt32, &pushReq, util.RawSnappy); err != nil {
			rw.WriteHeader(500)
			return
		}

		receivedReqsChan <- receivedReq{
			tenantID: req.Header.Get("X-Scope-OrgID"),
			pushReq:  pushReq,
		}

		rw.WriteHeader(receivedOKStatus)
	}
}

func createTestLogger() log.Logger {
	return level.NewFilter(log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr)), level.AllowDebug())
}

func TestPromtailWithWAL(t *testing.T) {
	var wg sync.WaitGroup
	dir := t.TempDir()
	walDir := t.TempDir()
	const scrapedFileName = "logs.txt"
	const expectedLabelSet = `{job="testlogs"}`

	// create logger for whole promtail with debug enabled
	util_log.Logger = createTestLogger()

	// create receive channel and start a collect routine
	receivedCh := make(chan receivedReq)
	received := map[string][]push.Entry{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for req := range receivedCh {
			for _, stream := range req.pushReq.Streams {
				received[stream.Labels] = append(received[stream.Labels], stream.Entries...)
			}
		}
	}()

	testServer := newTestRemoteWriteServer(receivedCh, http.StatusOK)
	t.Logf("started test server at URL %s", testServer.URL)
	testServerURL := flagext.URLValue{}
	testServerURL.Set(testServer.URL)

	cfg := createPromtailConfig(dir, testServerURL, scrapedFileName)
	cfg.WAL = wal.Config{
		Enabled:       true,
		Dir:           walDir,
		MaxSegmentAge: time.Second * 30,
	}

	clientMetrics := client.NewMetrics(nil)
	pr, err := New(cfg, nil, clientMetrics, false)
	require.NoError(t, err)

	wg.Add(1)
	go func() {
		defer wg.Done()
		err := pr.Run()
		require.NoError(t, err, "expected promtail run to succeed")
	}()
	defer pr.Shutdown()
	defer testServer.Close()

	const entriesToWrite = 100
	logsFile, err := os.Create(filepath.Join(dir, scrapedFileName))
	require.NoError(t, err)
	for i := 0; i < entriesToWrite; i++ {
		_, err = logsFile.WriteString(fmt.Sprintf("log line # %d\n", i))
		require.NoError(t, err, "error writing log line")
		// not overkill log file
		time.Sleep(1 * time.Millisecond)
	}

	require.Eventually(t, func() bool {
		if seen, ok := received[expectedLabelSet]; ok {
			return len(seen) == entriesToWrite
		}
		return false
	}, time.Second*20, time.Second, "timed out waiting for entries to be remote written")

	pr.Shutdown()
	close(receivedCh)

	t.Log("waiting on test waitgroup")
	wg.Wait()
}

func createPromtailConfig(dir string, testServerURL flagext.URLValue, scrapedFileName string) config.Config {
	// configure basic server settings
	cfg := config.Config{}
	// apply default values
	flagext.DefaultValues(&cfg)
	cfg.ServerConfig.HTTPListenAddress = "localhost"
	cfg.ServerConfig.ExternalURL = "localhost"
	cfg.ServerConfig.GRPCListenAddress = "localhost"
	cfg.ServerConfig.HTTPListenPort = 0
	cfg.ServerConfig.GRPCListenPort = 0

	// positions file
	cfg.PositionsConfig.PositionsFile = path.Join(dir, "positions.txt")

	// configure remote write client
	cfg.ClientConfigs = append(cfg.ClientConfigs, client.Config{
		Name:      "test-client",
		URL:       testServerURL,
		Timeout:   time.Second * 2,
		BatchWait: time.Second * 5,
		BatchSize: 1 << 10,
		BackoffConfig: backoff.Config{
			MaxRetries: 1,
		},
	})

	fixedFileScrapeConfig := scrapeconfig.Config{
		JobName:        "test",
		RelabelConfigs: nil,
		PipelineStages: stages.PipelineStages{
			stages.PipelineStage{
				stages.StageTypeLabelDrop: []string{
					"filename",
					"localhost",
				},
			},
		},
		ServiceDiscoveryConfig: scrapeconfig.ServiceDiscoveryConfig{
			StaticConfigs: discovery.StaticConfig{
				&targetgroup.Group{
					Targets: []model.LabelSet{{
						"localhost": "",
					}},
					Labels: model.LabelSet{
						"job":      "testlogs",
						"__path__": model.LabelValue(path.Join(dir, scrapedFileName)),
					},
				},
			},
		},
	}

	cfg.ScrapeConfig = append(cfg.ScrapeConfig, fixedFileScrapeConfig)
	return cfg
}
