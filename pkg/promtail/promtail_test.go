package promtail

import (
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/flagext"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/common/model"
	sd_config "github.com/prometheus/prometheus/discovery/config"
	"github.com/prometheus/prometheus/discovery/targetgroup"
	"github.com/weaveworks/common/server"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/parser"
	"github.com/grafana/loki/pkg/promtail/api"
	"github.com/grafana/loki/pkg/promtail/client"
	"github.com/grafana/loki/pkg/promtail/config"
	"github.com/grafana/loki/pkg/promtail/positions"
	"github.com/grafana/loki/pkg/promtail/scrape"
	"github.com/grafana/loki/pkg/promtail/targets"
)

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
	defer os.RemoveAll(dirName)

	testDir := dirName + "/logs"
	err = os.MkdirAll(testDir, 0750)
	if err != nil {
		t.Error(err)
		return
	}

	handler := &testServerHandler{
		receivedMap: map[string][]logproto.Entry{},
		recMtx:      sync.Mutex{},
		t:           t,
	}
	http.Handle("/api/prom/push", handler)
	go func() {
		if err := http.ListenAndServe("127.0.0.1:3100", nil); err != nil {
			t.Fatal("Failed to start web server to receive logs", err)
		}
	}()

	// Run.

	p, err := New(buildTestConfig(t, positionsFileName, testDir))
	if err != nil {
		t.Error("error creating promtail", err)
		return
	}

	go func() {
		err := p.Run()
		if err != nil {
			t.Fatal("Failed to start promtail", err)
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

	// Wait for all lines to be received.
	if err := waitForEntries(20, handler, expectedCounts); err != nil {
		t.Fatal("Timed out waiting all log entries to be sent, still waiting for ", err)
	}

	p.Shutdown()

	// Verify.

	verifyFile(t, expectedCounts[logFile1], prefix1, handler.receivedMap[logFile1])
	verifyFile(t, expectedCounts[logFile2], prefix2, handler.receivedMap[logFile2])
	verifyFile(t, expectedCounts[logFile3], prefix3, handler.receivedMap[logFile3])
	verifyFile(t, expectedCounts[logFile4], prefix4, handler.receivedMap[logFile4])

	if len(handler.receivedMap) != len(expectedCounts) {
		t.Error("Somehow we ended up tailing more files than we were supposed to, this is likely a bug")
	}

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
			}
		}
		return errors.New("Did not receive the correct number of logs within timeout, still waiting for logs from" + waiting)
	}
	return nil
}

type testServerHandler struct {
	receivedMap map[string][]logproto.Entry
	recMtx      sync.Mutex
	t           *testing.T
}

func (h *testServerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req logproto.PushRequest
	if _, err := util.ParseProtoReader(r.Context(), r.Body, &req, util.RawSnappy); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	h.recMtx.Lock()
	for _, s := range req.Streams {
		labels, err := parser.Labels(s.Labels)
		if err != nil {
			h.t.Error("Failed to parse incoming labels", err)
			return
		}
		file := ""
		for _, label := range labels {
			if label.Name == "__filename__" {
				file = string(label.Value)
				continue
			}
		}
		if file == "" {
			h.t.Error("Expected to find a label with name __filename__ but did not!")
			return
		}
		if _, ok := h.receivedMap[file]; ok {
			h.receivedMap[file] = append(h.receivedMap[file], s.Entries...)
		} else {
			h.receivedMap[file] = s.Entries
		}
	}
	h.recMtx.Unlock()
}

func buildTestConfig(t *testing.T, positionsFileName string, logDirName string) config.Config {
	var clientURL flagext.URLValue
	err := clientURL.Set("http://localhost:3100/api/prom/push")
	if err != nil {
		t.Fatal("Failed to parse client URL")
	}

	cfg := config.Config{
		ServerConfig:    server.Config{},
		ClientConfig:    client.Config{},
		PositionsConfig: positions.Config{},
		ScrapeConfig:    []scrape.Config{},
		TargetConfig:    targets.Config{},
	}
	// Init everything with default values.
	flagext.RegisterFlags(&cfg)

	// Override some of those defaults
	cfg.ClientConfig.URL = clientURL
	cfg.ClientConfig.BatchWait = 10 * time.Millisecond
	cfg.ClientConfig.BatchSize = 10 * 1024

	cfg.PositionsConfig.SyncPeriod = 100 * time.Millisecond
	cfg.PositionsConfig.PositionsFile = positionsFileName

	targetGroup := targetgroup.Group{
		Targets: []model.LabelSet{{
			"localhost": "",
		}},
		Labels: model.LabelSet{
			"job":      "varlogs",
			"__path__": model.LabelValue(logDirName + "/**/*.log"),
		},
		Source: "",
	}

	serviceConfig := sd_config.ServiceDiscoveryConfig{
		StaticConfigs: []*targetgroup.Group{
			&targetGroup,
		},
	}

	scrapeConfig := scrape.Config{
		JobName:                "",
		EntryParser:            api.Raw,
		RelabelConfigs:         nil,
		ServiceDiscoveryConfig: serviceConfig,
	}
	cfg.ScrapeConfig = append(cfg.ScrapeConfig, scrapeConfig)

	cfg.TargetConfig.SyncPeriod = 10 * time.Millisecond

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
