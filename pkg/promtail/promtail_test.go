package promtail

import (
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/flagext"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/discovery"
	"github.com/prometheus/prometheus/discovery/targetgroup"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/pkg/textparse"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logentry/stages"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/promtail/client"
	"github.com/grafana/loki/pkg/promtail/config"
	"github.com/grafana/loki/pkg/promtail/positions"
	"github.com/grafana/loki/pkg/promtail/scrapeconfig"
	file2 "github.com/grafana/loki/pkg/promtail/targets/file"
)

const httpTestPort = 9080

func TestPromtail(t *testing.T) {

	// Setup.
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)
	logger = level.NewFilter(logger, level.AllowInfo())
	util.Logger = logger

	initRandom()
	dirName := "/tmp/promtail_test_" + randName()
	positionsFileName := dirName + "/positions.yml"

	err := os.MkdirAll(dirName, 0750)
	if err != nil {
		t.Error(err)
		return
	}

	defer func() { _ = os.RemoveAll(dirName) }()

	testDir := dirName + "/logs"
	err = os.MkdirAll(testDir, 0750)
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
	defer func() {
		if err != nil {
			t.Fatal(err)
		}
	}()
	go func() {
		if err = http.ListenAndServe("localhost:3100", nil); err != nil {
			err = errors.Wrap(err, "Failed to start web server to receive logs")
		}
	}()

	// Run.

	p, err := New(buildTestConfig(t, positionsFileName, testDir), false)
	if err != nil {
		t.Error("error creating promtail", err)
		return
	}

	go func() {
		err = p.Run()
		if err != nil {
			err = errors.Wrap(err, "Failed to start promtail")
		}
	}()

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
	lbls = append(lbls, labels.Labels{
		labels.Label{Name: "action", Value: "GET"},
		labels.Label{Name: "filename", Value: dirName + "/logs/testPipeline.log"},
		labels.Label{Name: "job", Value: "varlogs"},
		labels.Label{Name: "localhost", Value: ""},
		labels.Label{Name: "match", Value: "true"},
		labels.Label{Name: "stream", Value: "stderr"},
	})

	lbls = append(lbls, labels.Labels{
		labels.Label{Name: "action", Value: "POST"},
		labels.Label{Name: "filename", Value: dirName + "/logs/testPipeline.log"},
		labels.Label{Name: "job", Value: "varlogs"},
		labels.Label{Name: "localhost", Value: ""},
		labels.Label{Name: "match", Value: "true"},
		labels.Label{Name: "stream", Value: "stdout"},
	})
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

	//Pull out some prometheus metrics before shutting down
	metricsBytes, contentType := getPromMetrics(t)

	p.Shutdown()

	// Verify.
	verifyFile(t, expectedCounts[logFile1], prefix1, handler.receivedMap[logFile1])
	verifyFile(t, expectedCounts[logFile2], prefix2, handler.receivedMap[logFile2])
	verifyFile(t, expectedCounts[logFile3], prefix3, handler.receivedMap[logFile3])
	verifyFile(t, expectedCounts[logFile4], prefix4, handler.receivedMap[logFile4])
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
	if err := os.Mkdir(symlinkDir, 0750); err != nil {
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
	if err := os.MkdirAll(filepath.Dir(filename), 0750); err != nil {
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
					level.Info(util.Logger).Log("file", file, "entry", e.Line)
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

func getPromMetrics(t *testing.T) ([]byte, string) {
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/metrics", httpTestPort))
	if err != nil {
		t.Fatal("Could not query metrics endpoint", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatal("Received a non 200 status code from /metrics endpoint", resp.StatusCode)
	}

	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Fatal("Error reading response body from /metrics endpoint", err)
	}
	ct := resp.Header.Get("Content-Type")
	return b, ct
}

func parsePromMetrics(t *testing.T, bytes []byte, contentType string, metricName string, label string) map[string]float64 {
	rb := map[string]float64{}

	pr := textparse.New(bytes, contentType)
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
	cfg.ServerConfig.HTTPListenPort = httpTestPort

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
			"localhost": "",
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

func initRandom() {
	rand.Seed(time.Now().UnixNano())
}

var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func randName() string {
	b := make([]rune, 10)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

func Test_DryRun(t *testing.T) {

	f, err := ioutil.TempFile("/tmp", "Test_DryRun")
	require.NoError(t, err)
	defer os.Remove(f.Name())

	_, err = New(config.Config{}, true)
	require.Error(t, err)

	prometheus.DefaultRegisterer = prometheus.NewRegistry() // reset registry, otherwise you can't create 2 weavework server.
	_, err = New(config.Config{
		ClientConfig: client.Config{URL: flagext.URLValue{URL: &url.URL{Host: "string"}}},
		PositionsConfig: positions.Config{
			PositionsFile: f.Name(),
			SyncPeriod:    time.Second,
		},
	}, true)
	require.NoError(t, err)

	prometheus.DefaultRegisterer = prometheus.NewRegistry()

	p, err := New(config.Config{
		ClientConfig: client.Config{URL: flagext.URLValue{URL: &url.URL{Host: "string"}}},
		PositionsConfig: positions.Config{
			PositionsFile: f.Name(),
			SyncPeriod:    time.Second,
		},
	}, false)
	require.NoError(t, err)
	require.IsType(t, &client.MultiClient{}, p.client)
}
