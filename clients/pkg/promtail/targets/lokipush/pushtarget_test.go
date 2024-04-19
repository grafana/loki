package lokipush

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/server"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/relabel"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/clients/pkg/promtail/api"
	"github.com/grafana/loki/v3/clients/pkg/promtail/client"
	"github.com/grafana/loki/v3/clients/pkg/promtail/client/fake"
	"github.com/grafana/loki/v3/clients/pkg/promtail/scrapeconfig"

	"github.com/grafana/loki/v3/pkg/logproto"
)

const localhost = "127.0.0.1"

func TestLokiPushTarget(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)

	//Create PushTarget
	eh := fake.New(func() {})
	defer eh.Stop()

	// Get a randomly available port by open and closing a TCP socket
	addr, err := net.ResolveTCPAddr("tcp", localhost+":0")
	require.NoError(t, err)
	l, err := net.ListenTCP("tcp", addr)
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	err = l.Close()
	require.NoError(t, err)

	// Adjust some of the defaults
	defaults := server.Config{}
	defaults.RegisterFlags(flag.NewFlagSet("empty", flag.ContinueOnError))
	defaults.HTTPListenAddress = localhost
	defaults.HTTPListenPort = port
	defaults.GRPCListenAddress = localhost
	defaults.GRPCListenPort = 0 // Not testing GRPC, a random port will be assigned

	config := &scrapeconfig.PushTargetConfig{
		Server: defaults,
		Labels: model.LabelSet{
			"pushserver": "pushserver1",
			"dropme":     "label",
		},
		KeepTimestamp: true,
	}

	rlbl := []*relabel.Config{
		{
			Action: relabel.LabelDrop,
			Regex:  relabel.MustNewRegexp("dropme"),
		},
	}

	pt, err := NewPushTarget(logger, eh, rlbl, "job1", config)
	require.NoError(t, err)

	// Build a client to send logs
	serverURL := flagext.URLValue{}
	err = serverURL.Set("http://" + localhost + ":" + strconv.Itoa(port) + "/loki/api/v1/push")
	require.NoError(t, err)

	ccfg := client.Config{
		URL:       serverURL,
		Timeout:   1 * time.Second,
		BatchWait: 1 * time.Second,
		BatchSize: 100 * 1024,
	}
	m := client.NewMetrics(prometheus.DefaultRegisterer)
	pc, err := client.New(m, ccfg, 0, 0, false, logger)
	require.NoError(t, err)
	defer pc.Stop()

	// Send some logs
	labels := model.LabelSet{
		"stream":             "stream1",
		"__anotherdroplabel": "dropme",
	}
	for i := 0; i < 100; i++ {
		pc.Chan() <- api.Entry{
			Labels: labels,
			Entry: logproto.Entry{
				Timestamp: time.Unix(int64(i), 0),
				Line:      "line" + strconv.Itoa(i),
			},
		}
	}

	// Wait for them to appear in the test handler
	countdown := 10000
	for len(eh.Received()) != 100 && countdown > 0 {
		time.Sleep(1 * time.Millisecond)
		countdown--
	}

	// Make sure we didn't timeout
	require.Equal(t, 100, len(eh.Received()))

	// Verify labels
	expectedLabels := model.LabelSet{
		"pushserver": "pushserver1",
		"stream":     "stream1",
	}
	// Spot check the first value in the result to make sure relabel rules were applied properly
	require.Equal(t, expectedLabels, eh.Received()[0].Labels)

	// With keep timestamp enabled, verify timestamp
	require.Equal(t, time.Unix(99, 0).Unix(), eh.Received()[99].Timestamp.Unix())

	_ = pt.Stop()

}

func TestPlaintextPushTarget(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)

	//Create PushTarget
	eh := fake.New(func() {})
	defer eh.Stop()

	// Get a randomly available port by open and closing a TCP socket
	addr, err := net.ResolveTCPAddr("tcp", localhost+":0")
	require.NoError(t, err)
	l, err := net.ListenTCP("tcp", addr)
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	err = l.Close()
	require.NoError(t, err)

	// Adjust some of the defaults
	defaults := server.Config{}
	defaults.RegisterFlags(flag.NewFlagSet("empty", flag.ContinueOnError))
	defaults.HTTPListenAddress = localhost
	defaults.HTTPListenPort = port
	defaults.GRPCListenAddress = localhost
	defaults.GRPCListenPort = 0 // Not testing GRPC, a random port will be assigned

	config := &scrapeconfig.PushTargetConfig{
		Server: defaults,
		Labels: model.LabelSet{
			"pushserver": "pushserver2",
			"keepme":     "label",
		},
		KeepTimestamp: true,
	}

	pt, err := NewPushTarget(logger, eh, []*relabel.Config{}, "job2", config)
	require.NoError(t, err)

	// Send some logs
	ts := time.Now()
	body := new(bytes.Buffer)
	for i := 0; i < 100; i++ {
		body.WriteString("line" + strconv.Itoa(i))
		_, err := http.Post(fmt.Sprintf("http://%s:%d/promtail/api/v1/raw", localhost, port), "text/json", body)
		require.NoError(t, err)
		body.Reset()
	}

	// Wait for them to appear in the test handler
	countdown := 10000
	for len(eh.Received()) != 100 && countdown > 0 {
		time.Sleep(1 * time.Millisecond)
		countdown--
	}

	// Make sure we didn't timeout
	require.Equal(t, 100, len(eh.Received()))

	// Verify labels
	expectedLabels := model.LabelSet{
		"pushserver": "pushserver2",
		"keepme":     "label",
	}
	// Spot check the first value in the result to make sure relabel rules were applied properly
	require.Equal(t, expectedLabels, eh.Received()[0].Labels)

	// Timestamp is always set in the handler, we expect received timestamps to be slightly higher than the timestamp when we started sending logs.
	require.GreaterOrEqual(t, eh.Received()[99].Timestamp.Unix(), ts.Unix())

	_ = pt.Stop()

}

func TestReady(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)

	//Create PushTarget
	eh := fake.New(func() {})
	defer eh.Stop()

	// Get a randomly available port by open and closing a TCP socket
	addr, err := net.ResolveTCPAddr("tcp", localhost+":0")
	require.NoError(t, err)
	l, err := net.ListenTCP("tcp", addr)
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	err = l.Close()
	require.NoError(t, err)

	// Adjust some of the defaults
	defaults := server.Config{}
	defaults.RegisterFlags(flag.NewFlagSet("empty", flag.ContinueOnError))
	defaults.HTTPListenAddress = localhost
	defaults.HTTPListenPort = port
	defaults.GRPCListenAddress = localhost
	defaults.GRPCListenPort = 0 // Not testing GRPC, a random port will be assigned

	config := &scrapeconfig.PushTargetConfig{
		Server: defaults,
		Labels: model.LabelSet{
			"pushserver": "pushserver2",
			"keepme":     "label",
		},
		KeepTimestamp: true,
	}

	pt, err := NewPushTarget(logger, eh, []*relabel.Config{}, "job3", config)
	require.NoError(t, err)

	url := fmt.Sprintf("http://%s:%d/ready", localhost, port)
	response, err := http.Get(url)
	if err != nil {
		require.NoError(t, err)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		require.NoError(t, err)
	}
	responseCode := fmt.Sprint(response.StatusCode)
	responseBody := string(body)

	fmt.Println(responseBody)
	wantedResponse := "ready"
	if responseBody != wantedResponse {
		t.Errorf("got the response %q, want %q", responseBody, wantedResponse)
	}
	wantedCode := "200"
	if responseCode != wantedCode {
		t.Errorf("Got the response code %q, want %q", responseCode, wantedCode)
	}

	t.Cleanup(func() {
		_ = pt.Stop()
	})
}
