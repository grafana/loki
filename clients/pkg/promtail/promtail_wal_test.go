package promtail

import (
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/discovery"
	"github.com/prometheus/prometheus/discovery/targetgroup"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/clients/pkg/logentry/stages"
	"github.com/grafana/loki/v3/clients/pkg/promtail/client"
	"github.com/grafana/loki/v3/clients/pkg/promtail/config"
	"github.com/grafana/loki/v3/clients/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/v3/clients/pkg/promtail/utils"
	"github.com/grafana/loki/v3/clients/pkg/promtail/wal"

	"github.com/grafana/loki/pkg/push"

	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

const (
	expectedJobName = "testlogs"
)

// createTestLogger creates a debug enabled logger to STDERR
func createTestLogger() log.Logger {
	logger := level.NewFilter(log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr)), level.AllowDebug())
	// add timestamps to logged entries. Useful for debugging events timing
	return log.With(logger, "ts", log.DefaultTimestampUTC)
}

func TestPromtailWithWAL_SingleTenant(t *testing.T) {
	// recycle default registerer
	prometheus.DefaultRegisterer = prometheus.NewRegistry()

	var wg sync.WaitGroup
	dir := t.TempDir()
	walDir := t.TempDir()
	const scrapedFileName = "logs.txt"
	expectedLabelSet := fmt.Sprintf(`{job="%s"}`, expectedJobName)

	// create logger for whole promtail with debug enabled
	util_log.Logger = createTestLogger()

	// create receive channel and start a collect routine
	receivedCh := make(chan utils.RemoteWriteRequest)
	received := map[string][]push.Entry{}
	var mu sync.Mutex
	// Create a channel for log messages
	logCh := make(chan string, 100) // Buffered channel to avoid blocking

	wg.Add(1)
	go func() {
		defer wg.Done()
		for req := range receivedCh {
			mu.Lock()
			// Add some observability to the requests received in the remote write endpoint
			var counts []string
			for _, str := range req.Request.Streams {
				counts = append(counts, fmt.Sprint(len(str.Entries)))
			}
			logCh <- fmt.Sprintf("received request: %s", counts)
			for _, stream := range req.Request.Streams {
				received[stream.Labels] = append(received[stream.Labels], stream.Entries...)
			}
			mu.Unlock()
		}
	}()

	testServer := utils.NewRemoteWriteServer(receivedCh, http.StatusOK)
	t.Logf("started test server at URL %s", testServer.URL)
	testServerURL := flagext.URLValue{}
	_ = testServerURL.Set(testServer.URL)

	cfg, err := createPromtailConfig(dir, testServerURL, scrapedFileName, stages.PipelineStages{stages.PipelineStage{
		stages.StageTypeLabelDrop: []string{
			"filename",
			"localhost",
		},
	}})
	require.NoError(t, err)
	cfg.WAL = wal.Config{
		Enabled:       true,
		Dir:           walDir,
		MaxSegmentAge: time.Second * 30,
		WatchConfig:   wal.DefaultWatchConfig,
	}

	clientMetrics := client.NewMetrics(prometheus.DefaultRegisterer)
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

	// launch routine that will write to the scraped file
	var writerWG sync.WaitGroup
	defer writerWG.Wait()
	writerWG.Add(1)
	go func() {
		defer writerWG.Done()
		for i := 0; i < entriesToWrite; i++ {
			_, err = logsFile.WriteString(fmt.Sprintf("log line # %d\n", i))
			if err != nil {
				logCh <- fmt.Sprintf("error writing to log file. Err: %s", err.Error())
			}
			// not overkill log file
			time.Sleep(1 * time.Millisecond)
		}
	}()

	// Goroutine to handle log messages
	go func() {
		for msg := range logCh {
			t.Log(msg)
		}
	}()

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
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

func TestPromtailWithWAL_MultipleTenants(t *testing.T) {
	// recycle default registerer
	prometheus.DefaultRegisterer = prometheus.NewRegistry()

	var wg sync.WaitGroup
	dir := t.TempDir()
	walDir := t.TempDir()
	const scrapedFileName = "logs.txt"
	expectedLabelSet := fmt.Sprintf(`{job="%s"}`, expectedJobName)

	// create logger for whole promtail with debug enabled
	util_log.Logger = createTestLogger()

	// create receive channel and start a collect routine
	receivedCh := make(chan utils.RemoteWriteRequest)
	// received is a mapping from tenant, string-formatted label set to received entries
	received := map[string]map[string][]push.Entry{}
	var mu sync.Mutex
	var totalReceived = 0
	wg.Add(1)
	go func() {
		defer wg.Done()
		for req := range receivedCh {
			mu.Lock()
			// start received label entries map if first time tenant is seen
			if _, ok := received[req.TenantID]; !ok {
				received[req.TenantID] = map[string][]push.Entry{}
			}
			entriesPerLabel := received[req.TenantID]
			for _, stream := range req.Request.Streams {
				entriesPerLabel[stream.Labels] = append(entriesPerLabel[stream.Labels], stream.Entries...)
				// increment total count
				totalReceived += len(stream.Entries)
			}
			mu.Unlock()
		}
	}()

	testServer := utils.NewRemoteWriteServer(receivedCh, http.StatusOK)
	t.Logf("started test server at URL %s", testServer.URL)
	testServerURL := flagext.URLValue{}
	_ = testServerURL.Set(testServer.URL)

	cfg, err := createPromtailConfig(dir, testServerURL, scrapedFileName, stages.PipelineStages{
		// extract tenant ID from log line
		stages.PipelineStage{stages.StageTypeRegex: stages.RegexConfig{
			Expression: `^msg="(?P<msg>.+)" tenantID="(?P<tenantID>.+)"$`,
		}},
		// drop unnecessary labels
		stages.PipelineStage{stages.StageTypeLabelDrop: []string{
			"filename",
			"localhost",
		}},
		// set output to the msg extracted field
		stages.PipelineStage{stages.StageTypeOutput: stages.OutputConfig{
			Source: "msg",
		}},
		// set tenant to the tenantID extracted field
		stages.PipelineStage{stages.StageTypeTenant: stages.TenantConfig{
			Source: "tenantID",
		}},
	})
	require.NoError(t, err)
	cfg.WAL = wal.Config{
		Enabled:       true,
		Dir:           walDir,
		MaxSegmentAge: time.Second * 30,
		WatchConfig:   wal.DefaultWatchConfig,
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
	const expectedTenantCounts = 4
	logsFile, err := os.Create(filepath.Join(dir, scrapedFileName))
	require.NoError(t, err)

	// this creates an inline func to write log entries that will be read by Promtail
	logFunc := func(msg, tenantID string) {
		logLine := fmt.Sprintf("msg=\"%s\" tenantID=\"%s\"\n", msg, tenantID)
		_, err = logsFile.WriteString(logLine)
		if err != nil {
			t.Logf("failed to write log line. Err: %s", err.Error())
		}
		// not overkill log file
		time.Sleep(1 * time.Millisecond)
	}

	// launch routine that will write to the scraped file
	var writerWG sync.WaitGroup
	defer writerWG.Wait()
	writerWG.Add(1)
	go func() {
		defer writerWG.Done()
		for i := 0; i < entriesToWrite; i++ {
			logFunc(fmt.Sprintf("logging something %d", i), fmt.Sprint(i%expectedTenantCounts))
		}
	}()

	// wait for all entries to be remote written
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return totalReceived == entriesToWrite
	}, time.Second*20, time.Second, "timed out waiting for entries to be remote written")

	// assert over received entries
	require.Len(t, received, expectedTenantCounts, "not expected tenant count")
	mu.Lock()
	for tenantID := 0; tenantID < expectedTenantCounts; tenantID++ {
		// we should've received at least entriesToWrite / expectedTenantCounts
		require.GreaterOrEqual(t, len(received[fmt.Sprint(tenantID)][expectedLabelSet]), entriesToWrite/expectedTenantCounts)
	}
	mu.Unlock()

	pr.Shutdown()
	close(receivedCh)

	t.Log("waiting on test waitgroup")
	wg.Wait()
}

// createPromtailConfig creates a config.Config targeting the provided remote write server, and processing read log lines
// with the provided pipeline.
func createPromtailConfig(dir string, testServerURL flagext.URLValue, scrapedFileName string, pipeline stages.PipelineStages) (config.Config, error) {
	// configure basic server settings
	cfg := config.Config{}
	// apply default values
	flagext.DefaultValues(&cfg)
	const localhost = "localhost"
	cfg.ServerConfig.HTTPListenAddress = localhost
	cfg.ServerConfig.ExternalURL = localhost
	cfg.ServerConfig.GRPCListenAddress = localhost
	cfg.ServerConfig.HTTPListenPort = 0
	cfg.ServerConfig.GRPCListenPort = 0

	// positions file
	internalFilesDir := path.Join(dir, "internal")
	if err := os.Mkdir(internalFilesDir, 0755); err != nil {
		return config.Config{}, err
	}
	cfg.PositionsConfig.PositionsFile = path.Join(internalFilesDir, "positions.txt")

	// configure remote write client
	cfg.ClientConfigs = append(cfg.ClientConfigs, client.Config{
		Name:      "test-client",
		URL:       testServerURL,
		Timeout:   time.Second * 2,
		BatchWait: time.Second,
		BatchSize: 1 << 10,
		BackoffConfig: backoff.Config{
			MaxRetries: 1,
		},
	})

	fixedFileScrapeConfig := scrapeconfig.Config{
		JobName:        "test",
		RelabelConfigs: nil,
		PipelineStages: pipeline,
		ServiceDiscoveryConfig: scrapeconfig.ServiceDiscoveryConfig{
			StaticConfigs: discovery.StaticConfig{
				&targetgroup.Group{
					Targets: []model.LabelSet{{
						localhost: "",
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
	return cfg, nil
}
