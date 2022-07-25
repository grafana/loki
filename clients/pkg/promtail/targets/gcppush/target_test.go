package gcppush

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/relabel"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/server"

	"github.com/grafana/loki/clients/pkg/promtail/client/fake"
	"github.com/grafana/loki/clients/pkg/promtail/scrapeconfig"
)

const localhost = "127.0.0.1"

/*
message.data for the testPayload example below
*/
const expectedMessageData = `{
  "severity": "DEBUG",
  "textPayload": "Function execution took 1198 ms. Finished with status code: 200",
  "trace": "projects/wired-height/traces/54aa8a20875253c6b47caa8bd28ad652"
}`
const testPayload = `
{
	"message": {
		"attributes": {
			"logging.googleapis.com/timestamp": "2022-07-25T22:19:09.903683708Z"
		},
		"data": "ewogICJzZXZlcml0eSI6ICJERUJVRyIsCiAgInRleHRQYXlsb2FkIjogIkZ1bmN0aW9uIGV4ZWN1dGlvbiB0b29rIDExOTggbXMuIEZpbmlzaGVkIHdpdGggc3RhdHVzIGNvZGU6IDIwMCIsCiAgInRyYWNlIjogInByb2plY3RzL3dpcmVkLWhlaWdodC90cmFjZXMvNTRhYThhMjA4NzUyNTNjNmI0N2NhYThiZDI4YWQ2NTIiCn0=",
		"messageId": "5187581549398349",
		"message_id": "5187581549398349",
		"publishTime": "2022-07-25T22:19:15.56Z",
		"publish_time": "2022-07-25T22:19:15.56Z"
	},
	"subscription": "projects/wired-height-350515/subscriptions/to-heroku"
}`

func makeDrainRequest(host string, bodies ...string) (*http.Request, error) {
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/gcp/api/v1/push", host), strings.NewReader(strings.Join(bodies, "")))
	if err != nil {
		return nil, err
	}
	return req, nil
}

func TestHerokuDrainTarget(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)

	type expectedEntry struct {
		labels model.LabelSet
		line   string
	}
	type args struct {
		RequestBodies  []string
		RelabelConfigs []*relabel.Config
		Labels         model.LabelSet
	}

	cases := map[string]struct {
		args            args
		expectedEntries []expectedEntry
	}{
		"simplified cloud functions log line": {
			args: args{
				RequestBodies: []string{testPayload},
				Labels: model.LabelSet{
					"job": "some_job_name",
				},
			},
			expectedEntries: []expectedEntry{
				{
					labels: model.LabelSet{
						"job": "some_job_name",
					},
					line: expectedMessageData,
				},
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			// Create fake promtail client
			eh := fake.New(func() {})
			defer eh.Stop()

			serverConfig, port, err := getServerConfigWithAvailablePort()
			require.NoError(t, err, "error generating server config or finding open port")
			config := &scrapeconfig.GCPPushTargetConfig{
				Server:               serverConfig,
				Labels:               tc.args.Labels,
				UseIncomingTimestamp: false,
			}

			prometheus.DefaultRegisterer = prometheus.NewRegistry()
			metrics := NewMetrics(prometheus.DefaultRegisterer)
			pt, err := NewTarget(metrics, logger, eh, "test_job", config, tc.args.RelabelConfigs)
			require.NoError(t, err)
			defer func() {
				_ = pt.Stop()
			}()

			// Clear received lines after test case is ran
			defer eh.Clear()

			// Send some logs
			ts := time.Now()

			req, err := makeDrainRequest(fmt.Sprintf("http://%s:%d", localhost, port), tc.args.RequestBodies...)
			require.NoError(t, err, "expected test drain request to be successfully created")
			res, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			require.Equal(t, http.StatusNoContent, res.StatusCode, "expected no-content status code")

			waitForMessages(eh)

			// Make sure we didn't timeout
			require.Equal(t, len(tc.args.RequestBodies), len(eh.Received()))

			require.Equal(t, len(eh.Received()), len(tc.expectedEntries), "expected to receive equal amount of expected label sets")
			for i, expectedEntry := range tc.expectedEntries {
				// TODO: Add assertion over propagated timestamp
				actualEntry := eh.Received()[i]

				require.Equal(t, expectedEntry.line, actualEntry.Line, "expected line to be equal for %d-th entry", i)

				expectedLS := expectedEntry.labels
				actualLS := actualEntry.Labels
				for label, value := range expectedLS {
					require.Equal(t, expectedLS[label], actualLS[label], "expected label %s to be equal to %s in %d-th entry", label, value, i)
				}

				// Timestamp is always set in the handler, we expect received timestamps to be slightly higher than the timestamp when we started sending logs.
				require.GreaterOrEqual(t, actualEntry.Timestamp.Unix(), ts.Unix(), "expected %d-th entry to have a received timestamp greater than publish time", i)
			}
		})
	}
}

func waitForMessages(eh *fake.Client) {
	countdown := 1000
	for len(eh.Received()) != 1 && countdown > 0 {
		time.Sleep(1 * time.Millisecond)
		countdown--
	}
}

func getServerConfigWithAvailablePort() (cfg server.Config, port int, err error) {
	// Get a randomly available port by open and closing a TCP socket
	addr, err := net.ResolveTCPAddr("tcp", localhost+":0")
	if err != nil {
		return
	}
	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return
	}
	port = l.Addr().(*net.TCPAddr).Port
	err = l.Close()
	if err != nil {
		return
	}

	// Adjust some of the defaults
	cfg.RegisterFlags(flag.NewFlagSet("empty", flag.ContinueOnError))
	cfg.HTTPListenAddress = localhost
	cfg.HTTPListenPort = port
	cfg.GRPCListenAddress = localhost
	cfg.GRPCListenPort = 0 // Not testing GRPC, a random port will be assigned

	return
}
