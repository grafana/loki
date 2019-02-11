package promtail

import (
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/flagext"
	"github.com/go-kit/kit/log"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/promtail/api"
	"github.com/grafana/loki/pkg/promtail/client"
	"github.com/grafana/loki/pkg/promtail/config"
	"github.com/grafana/loki/pkg/promtail/positions"
	"github.com/grafana/loki/pkg/promtail/scrape"
	"github.com/grafana/loki/pkg/promtail/targets"
	"github.com/prometheus/common/model"
	sd_config "github.com/prometheus/prometheus/discovery/config"
	"github.com/prometheus/prometheus/discovery/targetgroup"
	"github.com/weaveworks/common/server"
)

func TestPromtailRun(t *testing.T) {

	// Setup.

	w := log.NewSyncWriter(os.Stderr)
	util.Logger = log.NewLogfmtLogger(w)

	initRandom()
	dirName := "/tmp/promtail_test_" + randName()
	positionsFileName := dirName + "/positions.yml"

	err := os.MkdirAll(dirName, 0750)
	if err != nil {
		t.Error(err)
		return
	}
	defer os.RemoveAll(dirName)

	logDirName := dirName + "/logs"
	err = os.MkdirAll(logDirName, 0750)
	if err != nil {
		t.Error(err)
		return
	}

	handler := &testServerHandler{
		pushRequests: make([]logproto.PushRequest, 0),
	}
	http.Handle("/api/prom/push", handler)
	go func() {
		if err := http.ListenAndServe(":3100", nil); err != nil {
			t.Fatal("Failed to start web server to receive logs", err)
		}
	}()

	// Run.

	p, err := New(buildTestConfig(t, positionsFileName, logDirName))
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

	f, err := os.Create(logDirName + "/test.log")
	if err != nil {
		t.Error(err)
		return
	}

	for i := 0; i < 1000; i++ {
		entry := fmt.Sprintf("test%d\n", i)
		_, err = f.WriteString(entry)
		if err != nil {
			t.Error(err)
			return
		}
		time.Sleep(1 * time.Millisecond)
	}

	// Wait for results.  200 * 100 = 20000ms = 20s max

	timeout := 200
	var logsReceived int
	for logsReceived != 1000 && timeout > 0 {
		logsReceived = 0
		for _, req := range handler.pushRequests {
			if len(req.Streams) > 0 {
				logsReceived += len(req.Streams[0].Entries)
			}
		}
		time.Sleep(100 * time.Millisecond)
		timeout--
	}

	if timeout <= 0 {
		t.Error("Did not receive the correct number of logs within timeout, expected 1000, received:", logsReceived)
	}

	p.Shutdown()

	entries := make([]string, 1000)
	i := 0
	for j := range handler.pushRequests {
		if len(handler.pushRequests[j].Streams) > 0 {
			for k := range handler.pushRequests[j].Streams[0].Entries {
				entries[i] = handler.pushRequests[j].Streams[0].Entries[k].Line
				i++
			}
		}
	}

	// Verify.

	for i := 0; i < 1000; i++ {
		if entries[i] != fmt.Sprintf("test%d", i) {
			t.Errorf("Received out of order log event, expected test%d, received %s", i, entries[i])
		}
	}

	expectedLabels := "{__filename__=\"" + dirName + "/logs/test.log\", job=\"varlogs\", localhost=\"\"}"
	receivedLables := handler.pushRequests[0].Streams[0].Labels
	if receivedLables != expectedLabels {
		t.Error("Did not received the expected Labels on the stream, expected:", expectedLabels, "received:", receivedLables)
	}

}

type testServerHandler struct {
	pushRequests []logproto.PushRequest
}

func (s *testServerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req logproto.PushRequest
	if _, err := util.ParseProtoReader(r.Context(), r.Body, &req, util.RawSnappy); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.pushRequests = append(s.pushRequests, req)
}

func buildTestConfig(t *testing.T, positionsFileName string, logDirName string) config.Config {
	var clientURL flagext.URLValue
	err := clientURL.Set("http://localhost:3100/api/prom/push")
	if err != nil {
		t.Fatal("Failed to parse client URL")
	}

	clientConfig := client.Config{
		URL:            clientURL,
		BatchWait:      10 * time.Millisecond,
		BatchSize:      10 * 1024,
		ExternalLabels: nil,
	}

	positionsConfig := positions.Config{
		SyncPeriod:    100 * time.Millisecond,
		PositionsFile: positionsFileName,
	}

	targetGroup := targetgroup.Group{
		Targets: []model.LabelSet{{
			"localhost": "",
		}},
		Labels: model.LabelSet{
			"job":      "varlogs",
			"__path__": model.LabelValue(logDirName + "/*.log"),
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

	targetConfig := targets.Config{
		SyncPeriod: 10 * time.Millisecond,
	}

	return config.Config{
		ServerConfig:    server.Config{},
		ClientConfig:    clientConfig,
		PositionsConfig: positionsConfig,
		ScrapeConfig: []scrape.Config{
			scrapeConfig,
		},
		TargetConfig: targetConfig,
	}

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
