package promtail

import (
	"context"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/flagext"
	serverww "github.com/grafana/dskit/server"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/discovery"
	"github.com/prometheus/prometheus/discovery/targetgroup"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/textparse"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/clients/pkg/logentry/stages"
	"github.com/grafana/loki/v3/clients/pkg/promtail/client"
	"github.com/grafana/loki/v3/clients/pkg/promtail/config"
	"github.com/grafana/loki/v3/clients/pkg/promtail/positions"
	"github.com/grafana/loki/v3/clients/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/v3/clients/pkg/promtail/server"
	pserver "github.com/grafana/loki/v3/clients/pkg/promtail/server"
	file2 "github.com/grafana/loki/v3/clients/pkg/promtail/targets/file"
	"github.com/grafana/loki/v3/clients/pkg/promtail/targets/testutils"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

var clientMetrics = client.NewMetrics(prometheus.DefaultRegisterer)

func TestPromtail(t *testing.T) {
	// Setup.
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)
	logger = level.NewFilter(logger, level.AllowInfo())
	util_log.Logger = logger

	testutils.InitRandom()
	dirName := filepath.Join(os.TempDir(), "/promtail_test_"+testutils.RandName())
	positionsFileName := dirName + "/positions.yml"

	err := os.MkdirAll(dirName, 0o750)
	if err != nil {
		t.Error(err)
		return
	}

	defer func() { _ = os.RemoveAll(dirName) }()

	testDir := dirName + "/logs"
	err = os.MkdirAll(testDir, 0o750)
	if err != nil {
		t.Error(err)
		return
	}

	handler := &testServerHandler{
		receivedMap:    map[string][]logproto.Entry{},
		receivedLabels: map[string][]labels.Labels{},
		recMtx:         sync.Mutex{},
		t:              t,
	}
	http.Handle("/loki/api/v1/push", handler)
	var (
		wg        sync.WaitGroup
		listenErr error
		server    = &http.Server{Addr: "localhost:3100", Handler: nil}
	)
	defer func() {
		if t.Failed() {
			return // Test has already failed; don't wait for everything to shut down.
		}
		fmt.Fprintf(os.Stdout, "wait close\n")
		wg.Wait()
		if err != nil {
			t.Fatal(err)
		}
		if listenErr != nil && listenErr != http.ErrServerClosed {
			t.Fatal(listenErr)
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		listenErr = server.ListenAndServe()
	}()
	defer func() {
		_ = server.Shutdown(context.Background())
	}()

	p, err := New(buildTestConfig(t, positionsFileName, testDir), nil, clientMetrics, false, nil)
	if err != nil {
		t.Error("error creating promtail", err)
		return
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		err = p.Run()
		if err != nil {
			err = errors.Wrap(err, "Failed to start promtail")
		}
	}()
	defer p.Shutdown() // In case the test fails before the call to Shutdown below.

	svr := p.server.(*pserver.PromtailServer)

	httpListenAddr := svr.Server.HTTPListenAddr()

	expectedCounts := map[string]int{}

	startupMarkerFile := testDir + "/startupMarker.log"
	expectedCounts[startupMarkerFile] = createStartupFile(t, startupMarkerFile)

	// Wait for promtail to startup and send entry from our startup marker file.
	if err := waitForEntries(10, handler, expectedCounts); err != nil {
		t.Fatal("Timed out waiting for promtail to start")
	}

	// Run test file scenarios.

	logFile1 := testDir + "/testSingle.log"
	prefix1 := "single"
	expectedCounts[logFile1] = singleFile(t, logFile1, prefix1)

	logFile2 := testDir + "/testFileRoll.log"
	prefix2 := "roll"
	expectedCounts[logFile2] = fileRoll(t, logFile2, prefix2)

	logFile3 := testDir + "/testSymlinkRoll.log"
	prefix3 := "sym"
	expectedCounts[logFile3] = symlinkRoll(t, testDir, logFile3, prefix3)

	logFile4 := testDir + "/testsubdir/testFile.log"
	prefix4 := "sub"
	expectedCounts[logFile4] = subdirSingleFile(t, logFile4, prefix4)

	logFile5 := testDir + "/testPipeline.log"
	entries := []string{
		`{"log":"11.11.11.11 - frank [25/Jan/2000:14:00:01 -0500] \"GET /1986.js HTTP/1.1\" 200 932 \"-\" \"Mozilla/5.0 (Windows; U; Windows NT 5.1; de; rv:1.9.1.7) Gecko/20091221 Firefox/3.5.7 GTB6\"","stream":"stderr","time":"2019-04-30T02:12:41.8443515Z"}`,
		`{"log":"11.11.11.12 - - [19/May/2015:04:05:16 -0500] \"POST /blog HTTP/1.1\" 200 10975 \"http://grafana.com/test/\" \"Mozilla/5.0 (Windows NT 6.1; WOW64) Gecko/20091221 Firefox/3.5.7 GTB6\"","stream":"stdout","time":"2019-04-30T02:12:42.8443515Z"}`,
	}
	expectedCounts[logFile5] = pipelineFile(t, logFile5, entries)
	expectedEntries := make(map[string]int)
	entriesArray := []string{
		`11.11.11.11 - frank [25/Jan/2000:14:00:01 -0500] "GET /1986.js HTTP/1.1" 200 932 "-" "Mozilla/5.0 (Windows; U; Windows NT 5.1; de; rv:1.9.1.7) Gecko/20091221 Firefox/3.5.7 GTB6"`,
		`11.11.11.12 - - [19/May/2015:04:05:16 -0500] "POST /blog HTTP/1.1" 200 10975 "http://grafana.com/test/" "Mozilla/5.0 (Windows NT 6.1; WOW64) Gecko/20091221 Firefox/3.5.7 GTB6"`,
	}
	for i, entry := range entriesArray {
		expectedEntries[entry] = i
	}
	lbls := []labels.Labels{}
	lbls = append(lbls, labels.FromStrings(
		"action", "GET",
		"filename", dirName+"/logs/testPipeline.log",
		"job", "varlogs",
		"address", "localhost",
		"match", "true",
		"stream", "stderr",
	))

	lbls = append(lbls, labels.FromStrings(
		"action", "POST",
		"filename", dirName+"/logs/testPipeline.log",
		"job", "varlogs",
		"address", "localhost",
		"match", "true",
		"stream", "stdout",
	))
	expectedLabels := make(map[string]int)
	for i, label := range lbls {
		expectedLabels[label.String()] = i
	}

	// Wait for all lines to be received.
	if err := waitForEntries(20, handler, expectedCounts); err != nil {
		t.Fatal("Timed out waiting for log entries: ", err)
	}

	// Delete one of the log files so we can verify metrics are clean up
	err = os.Remove(logFile1)
	if err != nil {
		t.Fatal("Could not delete a log file to verify metrics are removed: ", err)
	}

	// Sync period is 500ms in tests, need to wait for at least one sync period for tailer to be cleaned up
	<-time.After(500 * time.Millisecond)

	// Pull out some prometheus metrics before shutting down
	metricsBytes, contentType := getPromMetrics(t, httpListenAddr)

	p.Shutdown()

	// Verify.
	verifyFile(t, expectedCounts[logFile1], prefix1, handler.receivedMap[logFile1])
	verifyFile(t, expectedCounts[logFile2], prefix2, handler.receivedMap[logFile2])
	verifyFile(t, expectedCounts[logFile3], prefix3, handler.receivedMap[logFile3])
	verifyFile(t, expectedCounts[logFile4], prefix4, handler.receivedMap[logFile4])
	fmt.Println("Verifying pipeline", logFile5)
	verifyPipeline(t, expectedCounts[logFile5], expectedEntries, handler.receivedMap[logFile5], handler.receivedLabels[logFile5], expectedLabels)

	if len(handler.receivedMap) != len(expectedCounts) {
		t.Error("Somehow we ended up tailing more files than we were supposed to, this is likely a bug")
	}

	readBytesMetrics := parsePromMetrics(t, metricsBytes, contentType, "promtail_read_bytes_total", "path")
	fileBytesMetrics := parsePromMetrics(t, metricsBytes, contentType, "promtail_file_bytes_total", "path")

	verifyMetricAbsent(t, readBytesMetrics, "promtail_read_bytes_total", logFile1)
	verifyMetricAbsent(t, fileBytesMetrics, "promtail_file_bytes_total", logFile1)

	verifyMetric(t, readBytesMetrics, "promtail_read_bytes_total", logFile2, 800)
	verifyMetric(t, fileBytesMetrics, "promtail_file_bytes_total", logFile2, 800)

	verifyMetric(t, readBytesMetrics, "promtail_read_bytes_total", logFile3, 700)
	verifyMetric(t, fileBytesMetrics, "promtail_file_bytes_total", logFile3, 700)

	verifyMetric(t, readBytesMetrics, "promtail_read_bytes_total", logFile4, 590)
	verifyMetric(t, fileBytesMetrics, "promtail_file_bytes_total", logFile4, 590)
}

func createStartupFile(t *testing.T, filename string) int {
	f, err := os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString("marker\n")
	if err != nil {
		t.Fatal(err)
	}
	return 1
}

func verifyFile(t *testing.T, expected int, prefix string, entries []logproto.Entry) {
	for i := 0; i < expected; i++ {
		if entries[i].Line != fmt.Sprintf("%s%d", prefix, i) {
			t.Errorf("Received out of order or incorrect log event, expected test%d, received %s", i, entries[i].Line)
		}
	}
}

func verifyPipeline(t *testing.T, expected int, expectedEntries map[string]int, entries []logproto.Entry, labels []labels.Labels, expectedLabels map[string]int) {
	for i := 0; i < expected; i++ {
		if _, ok := expectedLabels[labels[i].String()]; !ok {
			t.Errorf("Did not receive expected labels, expected %v, received %s", expectedLabels, labels[i])
		}
	}

	for i := 0; i < expected; i++ {
		if _, ok := expectedEntries[entries[i].Line]; !ok {
			t.Errorf("Did not receive expected log entry, expected %v, received %s", expectedEntries, entries[i].Line)
		}
	}
}

func verifyMetricAbsent(t *testing.T, metrics map[string]float64, metric string, label string) {
	if _, ok := metrics[label]; ok {
		t.Error("Found metric", metric, "with label", label, "which was not expected, "+
			"this metric should not be present")
	}
}

func verifyMetric(t *testing.T, metrics map[string]float64, metric string, label string, expected float64) {
	if _, ok := metrics[label]; !ok {
		t.Error("Expected to find metric ", metric, " with", label, "but it was not present")
	} else {
		actualBytes := metrics[label]
		assert.Equal(t, expected, actualBytes, "found incorrect value for metric %s and label %s", metric, label)
	}
}

func singleFile(t *testing.T, filename string, prefix string) int {
	f, err := os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	entries := 100
	for i := 0; i < entries; i++ {
		entry := fmt.Sprintf("%s%d\n", prefix, i)
		_, err = f.WriteString(entry)
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(1 * time.Millisecond)
	}

	return entries
}

func pipelineFile(t *testing.T, filename string, entries []string) int {
	f, err := os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}

	for _, entry := range entries {
		line := fmt.Sprintf("%s\n", entry)
		_, err = f.WriteString(line)
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(1 * time.Millisecond)
	}

	return len(entries)
}

func fileRoll(t *testing.T, filename string, prefix string) int {
	f, err := os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		entry := fmt.Sprintf("%s%d\n", prefix, i)
		_, err = f.WriteString(entry)
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(1 * time.Millisecond)
	}

	if err = os.Rename(filename, filename+".1"); err != nil {
		t.Fatal("Failed to rename file for test: ", err)
	}
	f, err = os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	for i := 100; i < 200; i++ {
		entry := fmt.Sprintf("%s%d\n", prefix, i)
		_, err = f.WriteString(entry)
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(1 * time.Millisecond)
	}

	return 200
}

func symlinkRoll(t *testing.T, testDir string, filename string, prefix string) int {
	symlinkDir := testDir + "/symlink"
	if err := os.Mkdir(symlinkDir, 0o750); err != nil {
		t.Fatal(err)
	}

	// Create a file for the logs, make sure it doesn't end in .log
	symlinkFile := symlinkDir + "/log1.notail"
	f, err := os.Create(symlinkFile)
	if err != nil {
		t.Fatal(err)
	}

	// Link to that file with the provided file name.
	if err := os.Symlink(symlinkFile, filename); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		entry := fmt.Sprintf("%s%d\n", prefix, i)
		_, err = f.WriteString(entry)
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(1 * time.Millisecond)
	}

	// Remove the link, make a new file, link to the new file.
	if err := os.Remove(filename); err != nil {
		t.Fatal(err)
	}
	symlinkFile2 := symlinkDir + "/log2.notail"
	f, err = os.Create(symlinkFile2)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(symlinkFile2, filename); err != nil {
		t.Fatal(err)
	}
	for i := 100; i < 200; i++ {
		entry := fmt.Sprintf("%s%d\n", prefix, i)
		_, err = f.WriteString(entry)
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(1 * time.Millisecond)
	}

	return 200
}

func subdirSingleFile(t *testing.T, filename string, prefix string) int {
	if err := os.MkdirAll(filepath.Dir(filename), 0o750); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	entries := 100
	for i := 0; i < entries; i++ {
		entry := fmt.Sprintf("%s%d\n", prefix, i)
		_, err = f.WriteString(entry)
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(1 * time.Millisecond)
	}

	return entries
}

func waitForEntries(timeoutSec int, handler *testServerHandler, expectedCounts map[string]int) error {
	timeout := timeoutSec * 10
	for timeout > 0 {
		countReady := 0
		for file, expectedCount := range expectedCounts {
			handler.recMtx.Lock()
			if rcvd, ok := handler.receivedMap[file]; ok && len(rcvd) == expectedCount {
				countReady++
			}
			handler.recMtx.Unlock()
		}
		if countReady == len(expectedCounts) {
			break
		}
		time.Sleep(100 * time.Millisecond)
		timeout--
	}

	if timeout <= 0 {
		waiting := ""
		for file, expectedCount := range expectedCounts {
			if rcvd, ok := handler.receivedMap[file]; !ok || len(rcvd) != expectedCount {
				waiting = waiting + " " + file
				for _, e := range rcvd {
					level.Info(util_log.Logger).Log("file", file, "entry", e.Line)
				}
			}
		}
		return errors.New("still waiting for logs from" + waiting)
	}
	return nil
}

type testServerHandler struct {
	receivedMap    map[string][]logproto.Entry
	receivedLabels map[string][]labels.Labels
	recMtx         sync.Mutex
	t              *testing.T
}

func (h *testServerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req logproto.PushRequest
	if err := util.ParseProtoReader(r.Context(), r.Body, int(r.ContentLength), math.MaxInt32, &req, util.RawSnappy); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	h.recMtx.Lock()
	for _, s := range req.Streams {
		parsedLabels, err := parser.ParseMetric(s.Labels)
		if err != nil {
			h.t.Error("Failed to parse incoming labels", err)
			return
		}
		file := ""
		for _, label := range parsedLabels {
			if label.Name == file2.FilenameLabel {
				file = label.Value
				continue
			}
		}
		if file == "" {
			h.t.Error("Expected to find a label with name `filename` but did not!")
			return
		}

		h.receivedMap[file] = append(h.receivedMap[file], s.Entries...)
		h.receivedLabels[file] = append(h.receivedLabels[file], parsedLabels)

	}

	h.recMtx.Unlock()
}

func getPromMetrics(t *testing.T, httpListenAddr net.Addr) ([]byte, string) {
	resp, err := http.Get(fmt.Sprintf("http://%s/metrics", httpListenAddr))
	if err != nil {
		t.Fatal("Could not query metrics endpoint", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatal("Received a non 200 status code from /metrics endpoint", resp.StatusCode)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal("Error reading response body from /metrics endpoint", err)
	}
	ct := resp.Header.Get("Content-Type")
	return b, ct
}

func parsePromMetrics(t *testing.T, bytes []byte, contentType string, metricName string, label string) map[string]float64 {
	rb := map[string]float64{}

	pr, err := textparse.New(bytes, contentType, false, nil)
	require.NoError(t, err)
	for {
		et, err := pr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal("Failed to parse prometheus metrics", err)
		}
		switch et {
		case textparse.EntrySeries:
			var res labels.Labels
			_, _, v := pr.Series()
			pr.Metric(&res)
			switch res.Get(labels.MetricName) {
			case metricName:
				rb[res.Get(label)] = v
				continue
			default:
				continue
			}
		default:
			continue
		}
	}
	return rb
}

func buildTestConfig(t *testing.T, positionsFileName string, logDirName string) config.Config {
	var clientURL flagext.URLValue
	err := clientURL.Set("http://localhost:3100/loki/api/v1/push")
	if err != nil {
		t.Fatal("Failed to parse client URL")
	}

	cfg := config.Config{}
	// Init everything with default values.
	flagext.RegisterFlags(&cfg)

	const hostname = "localhost"
	cfg.ServerConfig.HTTPListenAddress = hostname
	cfg.ServerConfig.ExternalURL = hostname
	cfg.ServerConfig.GRPCListenAddress = hostname

	// NOTE: setting port to `0` makes it bind to some unused random port.
	// enabling tests run more self contained and easy to run tests in parallel.
	cfg.ServerConfig.HTTPListenPort = 0
	cfg.ServerConfig.GRPCListenPort = 0

	// Override some of those defaults
	cfg.ClientConfig.URL = clientURL
	cfg.ClientConfig.BatchWait = 10 * time.Millisecond
	cfg.ClientConfig.BatchSize = 10 * 1024

	cfg.PositionsConfig.SyncPeriod = 100 * time.Millisecond
	cfg.PositionsConfig.PositionsFile = positionsFileName

	pipeline := stages.PipelineStages{
		stages.PipelineStage{
			stages.StageTypeMatch: stages.MatcherConfig{
				PipelineName: nil,
				Selector:     "{match=\"true\"}",
				Stages: stages.PipelineStages{
					stages.PipelineStage{
						stages.StageTypeDocker: nil,
					},
					stages.PipelineStage{
						stages.StageTypeRegex: stages.RegexConfig{
							Expression: "^(?P<ip>\\S+) (?P<identd>\\S+) (?P<user>\\S+) \\[(?P<timestamp>[\\w:/]+\\s[+\\-]\\d{4})\\] \"(?P<action>\\S+)\\s?(?P<path>\\S+)?\\s?(?P<protocol>\\S+)?\" (?P<status>\\d{3}|-) (?P<size>\\d+|-)\\s?\"?(?P<referer>[^\"]*)\"?\\s?\"?(?P<useragent>[^\"]*)?\"?$",
							Source:     nil,
						},
					},
					stages.PipelineStage{
						stages.StageTypeTimestamp: stages.TimestampConfig{
							Source: "timestamp",
							Format: "02/Jan/2006:15:04:05 -0700",
						},
					},
					stages.PipelineStage{
						stages.StageTypeLabel: stages.LabelsConfig{
							"action": nil,
						},
					},
				},
			},
		},
	}

	targetGroup := targetgroup.Group{
		Targets: []model.LabelSet{{
			"address": "localhost",
		}},
		Labels: model.LabelSet{
			"job":      "varlogs",
			"match":    "true",
			"__path__": model.LabelValue(logDirName + "/**/*.log"),
		},
		Source: "",
	}
	scrapeConfig := scrapeconfig.Config{
		JobName:        "",
		PipelineStages: pipeline,
		RelabelConfigs: nil,
		ServiceDiscoveryConfig: scrapeconfig.ServiceDiscoveryConfig{
			StaticConfigs: discovery.StaticConfig{
				&targetGroup,
			},
		},
	}

	cfg.ScrapeConfig = append(cfg.ScrapeConfig, scrapeConfig)

	// Make sure the SyncPeriod is fast for test purposes, but not faster than the poll interval (250ms)
	// to avoid a race between the sync() function and the tailers noticing when files are deleted
	cfg.TargetConfig.SyncPeriod = 500 * time.Millisecond

	return cfg
}

func Test_DryRun(t *testing.T) {
	f, err := os.CreateTemp("", "Test_DryRun")
	require.NoError(t, err)
	defer os.Remove(f.Name())

	_, err = New(config.Config{}, nil, clientMetrics, true, nil)
	require.Error(t, err)

	// Set the minimum config needed to start a server. We need to do this since we
	// aren't doing any CLI parsing ala RegisterFlags and thus don't get the defaults.
	// Required because a hardcoded value became a configuration setting in this commit
	// https://github.com/weaveworks/common/commit/c44eeb028a671c5931b047976f9a0171910571ce
	serverCfg := server.Config{
		Config: serverww.Config{
			HTTPListenNetwork: serverww.DefaultNetwork,
			GRPCListenNetwork: serverww.DefaultNetwork,
			HTTPListenAddress: localhostConfig.HTTPListenAddress,
			GRPCListenAddress: localhostConfig.GRPCListenAddress,
		},
	}

	prometheus.DefaultRegisterer = prometheus.NewRegistry()

	_, err = New(config.Config{
		ServerConfig: serverCfg,
		ClientConfig: client.Config{URL: flagext.URLValue{URL: &url.URL{Host: "string"}}},
		PositionsConfig: positions.Config{
			PositionsFile: f.Name(),
			SyncPeriod:    time.Second,
		},
	}, nil, clientMetrics, true, nil)
	require.NoError(t, err)

	prometheus.DefaultRegisterer = prometheus.NewRegistry()

	p, err := New(config.Config{
		ServerConfig: serverCfg,
		ClientConfig: client.Config{URL: flagext.URLValue{URL: &url.URL{Host: "string"}}},
		PositionsConfig: positions.Config{
			PositionsFile: f.Name(),
			SyncPeriod:    time.Second,
		},
	}, nil, clientMetrics, false, nil)
	require.NoError(t, err)
	require.IsType(t, &client.Manager{}, p.client)
}

var localhostConfig = serverww.Config{
	HTTPListenAddress: "localhost",
	GRPCListenAddress: "localhost",
}

func Test_Reload(t *testing.T) {
	f, err := os.CreateTemp("", "Test_Reload")
	require.NoError(t, err)
	defer os.Remove(f.Name())

	cfg := config.Config{
		ServerConfig: server.Config{
			Reload: true,
			Config: localhostConfig,
		},
		ClientConfig: client.Config{URL: flagext.URLValue{URL: &url.URL{Host: "string"}}},
		PositionsConfig: positions.Config{
			PositionsFile: f.Name(),
			SyncPeriod:    time.Second,
		},
	}

	expectCfgStr := cfg.String()

	expectedConfig := &config.Config{
		ServerConfig: server.Config{
			Reload: true,
			Config: localhostConfig,
		},
		ClientConfig: client.Config{URL: flagext.URLValue{URL: &url.URL{Host: "reloadtesturl"}}},
		PositionsConfig: positions.Config{
			PositionsFile: f.Name(),
			SyncPeriod:    time.Second,
		},
	}

	expectedConfigReloaded := expectedConfig.String()

	prometheus.DefaultRegisterer = prometheus.NewRegistry() // reset registry, otherwise you can't create 2 weavework server.
	promtailServer, err := New(cfg, func() (*config.Config, error) {
		return expectedConfig, nil
	}, clientMetrics, true, nil)
	require.NoError(t, err)
	require.Equal(t, len(expectCfgStr), len(promtailServer.configLoaded))
	require.Equal(t, expectCfgStr, promtailServer.configLoaded)
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		err = promtailServer.Run()
		if err != nil {
			err = errors.Wrap(err, "Failed to start promtail")
		}
	}()
	defer promtailServer.Shutdown() // In case the test fails before the call to Shutdown below.

	svr := promtailServer.server.(*pserver.PromtailServer)

	require.NotEqual(t, len(expectedConfig.String()), len(svr.PromtailConfig()))
	require.NotEqual(t, expectedConfig.String(), svr.PromtailConfig())
	result, err := reload(t, svr.Server.HTTPListenAddr())
	require.NoError(t, err)
	expectedReloadResult := ""
	require.Equal(t, expectedReloadResult, result)
	require.Equal(t, len(expectedConfig.String()), len(svr.PromtailConfig()))
	require.Equal(t, expectedConfig.String(), svr.PromtailConfig())
	require.Equal(t, len(expectedConfigReloaded), len(promtailServer.configLoaded))
	require.Equal(t, expectedConfigReloaded, promtailServer.configLoaded)

	pb := &dto.Metric{}
	err = reloadSuccessTotal.Write(pb)
	require.NoError(t, err)
	require.Equal(t, 1.0, pb.Counter.GetValue())
}

func Test_ReloadFail_NotPanic(t *testing.T) {
	f, err := os.CreateTemp("", "Test_Reload")
	require.NoError(t, err)
	defer os.Remove(f.Name())

	cfg := config.Config{
		ServerConfig: server.Config{
			Reload: true,
			Config: localhostConfig,
		},
		ClientConfig: client.Config{URL: flagext.URLValue{URL: &url.URL{Host: "string"}}},
		PositionsConfig: positions.Config{
			PositionsFile: f.Name(),
			SyncPeriod:    time.Second,
		},
	}

	expectedConfig := &config.Config{
		ServerConfig: server.Config{
			Reload: true,
			Config: localhostConfig,
		},
		ClientConfig: client.Config{URL: flagext.URLValue{URL: &url.URL{Host: "reloadtesturl"}}},
		PositionsConfig: positions.Config{
			PositionsFile: f.Name(),
			SyncPeriod:    time.Second,
		},
	}

	newConfigErr := errors.New("load config fail")

	prometheus.DefaultRegisterer = prometheus.NewRegistry() // reset registry, otherwise you can't create 2 weavework server.
	promtailServer, err := New(cfg, func() (*config.Config, error) {
		return nil, newConfigErr
	}, clientMetrics, true, nil)
	require.NoError(t, err)

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		err = promtailServer.Run()
		if err != nil {
			err = errors.Wrap(err, "Failed to start promtail")
		}
	}()
	defer promtailServer.Shutdown() // In case the test fails before the call to Shutdown below.

	svr := promtailServer.server.(*pserver.PromtailServer)
	httpListenAddr := svr.Server.HTTPListenAddr()
	require.NotEqual(t, len(expectedConfig.String()), len(svr.PromtailConfig()))
	require.NotEqual(t, expectedConfig.String(), svr.PromtailConfig())
	result, err := reload(t, httpListenAddr)
	require.Error(t, err)
	expectedReloadResult := fmt.Sprintf("failed to reload config: Error new Config: %s\n", newConfigErr)
	require.Equal(t, expectedReloadResult, result)

	pb := &dto.Metric{}
	err = reloadFailTotal.Write(pb)
	require.NoError(t, err)
	require.Equal(t, 1.0, pb.Counter.GetValue())

	promtailServer.newConfig = func() (*config.Config, error) {
		return &cfg, nil
	}
	result, err = reload(t, httpListenAddr)
	require.Error(t, err)
	require.Equal(t, fmt.Sprintf("failed to reload config: %s\n", errConfigNotChange), result)
}

func reload(t *testing.T, httpListenAddr net.Addr) (string, error) {
	resp, err := http.Get(fmt.Sprintf("http://%s/reload", httpListenAddr))
	if err != nil {
		t.Fatal("Could not query reload endpoint", err)
	}
	if resp.StatusCode == http.StatusInternalServerError {
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal("Error reading response body from /reload endpoint", err)
		}
		return string(b), errors.New("Received a 500 status code from /reload endpoint")
	}
	if resp.StatusCode != http.StatusOK {
		return "", errors.New("Received a non 200 status code from /reload endpoint")
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal("Error reading response body from /reload endpoint", err)
	}
	return string(b), nil
}
