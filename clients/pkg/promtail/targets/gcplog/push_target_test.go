package gcplog_test

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

	lokiClient "github.com/grafana/loki/clients/pkg/promtail/client"
	"github.com/grafana/loki/clients/pkg/promtail/client/fake"
	"github.com/grafana/loki/clients/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/clients/pkg/promtail/targets/gcplog"
)

const localhost = "127.0.0.1"

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
	"subscription": "projects/wired-height-350515/subscriptions/test"
}`

func TestPushTarget(t *testing.T) {
	type expectedEntry struct {
		labels model.LabelSet
		line   string
	}
	type args struct {
		RequestBody    string
		RelabelConfigs []*relabel.Config
		Labels         model.LabelSet
	}

	cases := map[string]struct {
		args            args
		expectedEntries []expectedEntry
	}{
		"simplified cloud functions log line": {
			args: args{
				RequestBody: testPayload,
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
		"simplified cloud functions log line, with relabeling custom attribute and message id": {
			args: args{
				RequestBody: testPayload,
				Labels: model.LabelSet{
					"job": "some_job_name",
				},
				RelabelConfigs: []*relabel.Config{
					{
						SourceLabels: model.LabelNames{"__gcp_attributes_logging_googleapis_com_timestamp"},
						Regex:        relabel.MustNewRegexp("(.*)"),
						Replacement:  "$1",
						TargetLabel:  "google_timestamp",
						Action:       relabel.Replace,
					},
					{
						SourceLabels: model.LabelNames{"__gcp_message_id"},
						Regex:        relabel.MustNewRegexp("(.*)"),
						Replacement:  "$1",
						TargetLabel:  "message_id",
						Action:       relabel.Replace,
					},
					{
						SourceLabels: model.LabelNames{"__gcp_subscription_name"},
						Regex:        relabel.MustNewRegexp("(.*)"),
						Replacement:  "$1",
						TargetLabel:  "subscription",
						Action:       relabel.Replace,
					},
				},
			},
			expectedEntries: []expectedEntry{
				{
					labels: model.LabelSet{
						"job":              "some_job_name",
						"google_timestamp": "2022-07-25T22:19:09.903683708Z",
						"message_id":       "5187581549398349",
						"subscription":     "projects/wired-height-350515/subscriptions/test",
					},
					line: expectedMessageData,
				},
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			eh, sendRequestToTarget, stop, err := startTargetWithConfig(&scrapeconfig.GcplogTargetConfig{
				Labels:               tc.args.Labels,
				UseIncomingTimestamp: false,
				SubscriptionType:     "push",
			}, tc.args.RelabelConfigs...)
			require.NoError(t, err, "Failed to start target")
			defer stop()

			// Send some logs
			ts := time.Now()

			res, err := sendRequestToTarget(tc.args.RequestBody)
			require.NoError(t, err)
			require.Equal(t, http.StatusNoContent, res.StatusCode, "expected no-content status code")

			waitForMessages(eh)

			// Make sure we didn't timeout
			require.Equal(t, 1, len(eh.Received()))

			require.Equal(t, len(eh.Received()), len(tc.expectedEntries), "expected to receive equal amount of expected label sets")
			for i, expectedEntry := range tc.expectedEntries {
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

func TestPushTarget_UseIncomingTimestamp(t *testing.T) {
	eh, sendRequestToTarget, stop, err := startTargetWithConfig(&scrapeconfig.GcplogTargetConfig{
		Labels:               nil,
		UseIncomingTimestamp: true,
		SubscriptionType:     "push",
	})
	require.NoError(t, err, "Failed to start target")
	defer stop()

	res, err := sendRequestToTarget(testPayload)
	require.NoError(t, err)
	require.Equal(t, http.StatusNoContent, res.StatusCode, "expected no-content status code")

	waitForMessages(eh)

	// Make sure we didn't timeout
	require.Equal(t, 1, len(eh.Received()))

	expectedTs, err := time.Parse(time.RFC3339Nano, "2022-07-25T22:19:15.56Z")
	require.NoError(t, err, "expected expected timestamp to be parse correctly")
	require.Equal(t, expectedTs, eh.Received()[0].Timestamp, "expected entry timestamp to be overridden by received one")
}

func TestPushTarget_UseTenantIDHeaderIfPresent(t *testing.T) {
	eh, sendRequestToTarget, stop, err := startTargetWithConfig(&scrapeconfig.GcplogTargetConfig{
		Labels:           nil,
		SubscriptionType: "push",
	})
	require.NoError(t, err, "Failed to start target")
	defer stop()

	res, err := sendRequestToTarget(testPayload, func(req *http.Request) {
		req.Header.Set("X-Scope-OrgID", "42")
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusNoContent, res.StatusCode, "expected no-content status code")

	waitForMessages(eh)

	// Make sure we didn't timeout
	require.Equal(t, 1, len(eh.Received()))

	require.Equal(t, model.LabelValue("42"), eh.Received()[0].Labels[lokiClient.ReservedLabelTenantID])
}

func TestPushTarget_ErroneousPayloadsAreRejected(t *testing.T) {
	testCases := []struct {
		Name               string
		Payload            string
		ExpectedStatusCode int
	}{
		{
			Name:               "Non JSON payload",
			Payload:            `{`,
			ExpectedStatusCode: http.StatusBadRequest,
		},
		{
			Name:               "empty JSON",
			Payload:            `{}`,
			ExpectedStatusCode: http.StatusBadRequest,
		},
		{
			Name: "JSON without logs line",
			Payload: `{
				"message": {"message_id": "123"},
				"subscription": "hi"
			}`,
			ExpectedStatusCode: http.StatusBadRequest,
		},
		{
			Name: "JSON without message ID",
			Payload: `{
				"message": {"data": "some log here 123"},
				"subscription": "hi"
			}`,
			ExpectedStatusCode: http.StatusBadRequest,
		},
		{
			Name: "JSON without subscription info",
			Payload: `{
				"message": {"data": "some log here 123", "message_id": "123"}
			}`,
			ExpectedStatusCode: http.StatusBadRequest,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			eh, sendRequestToTarget, stop, err := startTargetWithConfig(&scrapeconfig.GcplogTargetConfig{
				Labels:           nil,
				SubscriptionType: "push",
			})
			require.NoError(t, err, "Failed to start target")
			defer stop()

			res, err := sendRequestToTarget(tc.Payload)
			require.NoError(t, err)
			require.Equal(t, tc.ExpectedStatusCode, res.StatusCode, "expected %s status code", tc.ExpectedStatusCode)
			// Wait some time to allow message propagation
			time.Sleep(100 * time.Millisecond)
			// Expect no received messages
			require.Equal(t, 0, len(eh.Received()))
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

func makeGCPPushRequest(host string, body string) (*http.Request, error) {
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/gcp/api/v1/push", host), strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	return req, nil
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

type stopper = func()
type pushRequestSender = func(string, ...func(*http.Request)) (*http.Response, error)

func startTargetWithConfig(cfg *scrapeconfig.GcplogTargetConfig, relabels ...*relabel.Config) (*fake.Client, pushRequestSender, stopper, error) {
	// Collect all stoppables instantiated here
	type stopFunc = func()
	var stoppables = []stopFunc{}

	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)

	// Create fake promtail client
	eh := fake.New(func() {})
	stoppables = append(stoppables, eh.Stop)

	serverConfig, port, err := getServerConfigWithAvailablePort()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("error generating server config or finding open port: %w", err)
	}

	cfg.Server = serverConfig
	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	metrics := gcplog.NewMetrics(prometheus.DefaultRegisterer)
	pt, err := gcplog.NewGCPLogTarget(metrics, logger, eh, relabels, "test_job", cfg)
	if err != nil {
		return nil, nil, nil, err
	}
	stoppables = append(stoppables, func() { _ = pt.Stop() })

	stopAll := func() {
		for _, stop := range stoppables {
			stop()
		}
	}
	sendRequest := func(payload string, modifiers ...func(*http.Request)) (*http.Response, error) {
		req, err := makeGCPPushRequest(fmt.Sprintf("http://%s:%d", localhost, port), payload)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
		for _, mod := range modifiers {
			mod(req)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to make HTTP request: %w", err)
		}
		return res, nil
	}

	// Return apart from utilities, a function that stops all collected stoppables.
	return eh, sendRequest, stopAll, nil
}
