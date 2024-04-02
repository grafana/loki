package heroku

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/google/uuid"
	"github.com/grafana/dskit/server"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/relabel"
	"github.com/stretchr/testify/require"

	lokiClient "github.com/grafana/loki/v3/clients/pkg/promtail/client"
	"github.com/grafana/loki/v3/clients/pkg/promtail/client/fake"
	"github.com/grafana/loki/v3/clients/pkg/promtail/scrapeconfig"
)

const localhost = "127.0.0.1"

const testPayload = `270 <158>1 2022-06-13T14:52:23.622778+00:00 host heroku router - at=info method=GET path="/" host=cryptic-cliffs-27764.herokuapp.com request_id=59da6323-2bc4-4143-8677-cc66ccfb115f fwd="181.167.87.140" dyno=web.1 connect=0ms service=3ms status=200 bytes=6979 protocol=https
`
const testLogLine1 = `140 <190>1 2022-06-13T14:52:23.621815+00:00 host app web.1 - [GIN] 2022/06/13 - 14:52:23 | 200 |    1.428101ms |  181.167.87.140 | GET      "/"
`
const testLogLine1Timestamp = "2022-06-13T14:52:23.621815+00:00"
const testLogLine2 = `156 <190>1 2022-06-13T14:52:23.827271+00:00 host app web.1 - [GIN] 2022/06/13 - 14:52:23 | 200 |      163.92µs |  181.167.87.140 | GET      "/static/main.css"
`

func makeDrainRequest(host string, params map[string][]string, bodies ...string) (*http.Request, error) {
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/heroku/api/v1/drain", host), strings.NewReader(strings.Join(bodies, "")))
	if err != nil {
		return nil, err
	}

	drainToken := uuid.New().String()
	frameID := uuid.New().String()

	values := url.Values{}
	for name, params := range params {
		for _, p := range params {
			values.Add(name, p)
		}
	}
	req.URL.RawQuery = values.Encode()

	req.Header.Set("Content-Type", "application/heroku_drain-1")
	req.Header.Set("Logplex-Drain-Token", fmt.Sprintf("d.%s", drainToken))
	req.Header.Set("Logplex-Frame-Id", frameID)
	req.Header.Set("Logplex-Msg-Count", fmt.Sprintf("%d", len(bodies)))

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
		RequestParams  map[string][]string
		RelabelConfigs []*relabel.Config
		Labels         model.LabelSet
	}

	cases := map[string]struct {
		args            args
		expectedEntries []expectedEntry
	}{
		"heroku request with a single log line, internal labels dropped, and fixed are propagated": {
			args: args{
				RequestBodies: []string{testPayload},
				RequestParams: map[string][]string{},
				Labels: model.LabelSet{
					"job": "some_job_name",
				},
			},
			expectedEntries: []expectedEntry{
				{
					labels: model.LabelSet{
						"job": "some_job_name",
					},
					line: `at=info method=GET path="/" host=cryptic-cliffs-27764.herokuapp.com request_id=59da6323-2bc4-4143-8677-cc66ccfb115f fwd="181.167.87.140" dyno=web.1 connect=0ms service=3ms status=200 bytes=6979 protocol=https
`,
				},
			},
		},
		"heroku request with a single log line and query parameters, internal labels dropped, and fixed are propagated": {
			args: args{
				RequestBodies: []string{testPayload},
				RequestParams: map[string][]string{
					"some_query_param": {"app_123", "app_456"},
				},
				Labels: model.LabelSet{
					"job": "some_job_name",
				},
			},
			expectedEntries: []expectedEntry{
				{
					labels: model.LabelSet{
						"job": "some_job_name",
					},
					line: `at=info method=GET path="/" host=cryptic-cliffs-27764.herokuapp.com request_id=59da6323-2bc4-4143-8677-cc66ccfb115f fwd="181.167.87.140" dyno=web.1 connect=0ms service=3ms status=200 bytes=6979 protocol=https
`,
				},
			},
		},
		"heroku request with two log lines, internal labels dropped, and fixed are propagated": {
			args: args{
				RequestBodies: []string{testLogLine1, testLogLine2},
				RequestParams: map[string][]string{},
				Labels: model.LabelSet{
					"job": "multiple_line_job",
				},
			},
			expectedEntries: []expectedEntry{
				{
					labels: model.LabelSet{
						"job": "multiple_line_job",
					},
					line: `[GIN] 2022/06/13 - 14:52:23 | 200 |    1.428101ms |  181.167.87.140 | GET      "/"
`,
				},
				{
					labels: model.LabelSet{
						"job": "multiple_line_job",
					},
					line: `[GIN] 2022/06/13 - 14:52:23 | 200 |      163.92µs |  181.167.87.140 | GET      "/static/main.css"
`,
				},
			},
		},
		"heroku request with two log lines and query parameters, internal labels dropped, and fixed are propagated": {
			args: args{
				RequestBodies: []string{testLogLine1, testLogLine2},
				RequestParams: map[string][]string{
					"some_query_param": {"app_123", "app_456"},
				},
				Labels: model.LabelSet{
					"job": "multiple_line_job",
				},
			},
			expectedEntries: []expectedEntry{
				{
					labels: model.LabelSet{
						"job": "multiple_line_job",
					},
					line: `[GIN] 2022/06/13 - 14:52:23 | 200 |    1.428101ms |  181.167.87.140 | GET      "/"
`,
				},
				{
					labels: model.LabelSet{
						"job": "multiple_line_job",
					},
					line: `[GIN] 2022/06/13 - 14:52:23 | 200 |      163.92µs |  181.167.87.140 | GET      "/static/main.css"
`,
				},
			},
		},
		"heroku request with a single log line, with internal labels relabeled, and fixed labels": {
			args: args{
				RequestBodies: []string{testLogLine1},
				RequestParams: map[string][]string{},
				Labels: model.LabelSet{
					"job": "relabeling_job",
				},
				RelabelConfigs: []*relabel.Config{
					{
						SourceLabels: model.LabelNames{"__heroku_drain_host"},
						TargetLabel:  "host",
						Replacement:  "$1",
						Action:       relabel.Replace,
						Regex:        relabel.MustNewRegexp("(.*)"),
					},
					{
						SourceLabels: model.LabelNames{"__heroku_drain_app"},
						TargetLabel:  "app",
						Replacement:  "$1",
						Action:       relabel.Replace,
						Regex:        relabel.MustNewRegexp("(.*)"),
					},
					{
						SourceLabels: model.LabelNames{"__heroku_drain_proc"},
						TargetLabel:  "procID",
						Replacement:  "$1",
						Action:       relabel.Replace,
						Regex:        relabel.MustNewRegexp("(.*)"),
					},
				},
			},
			expectedEntries: []expectedEntry{
				{
					line: `[GIN] 2022/06/13 - 14:52:23 | 200 |    1.428101ms |  181.167.87.140 | GET      "/"
`,
					labels: model.LabelSet{
						"host":   "host",
						"app":    "app",
						"procID": "web.1",
					},
				},
			},
		},
		"heroku request with a single log line and query parameters, with internal labels relabeled, and fixed labels": {
			args: args{
				RequestBodies: []string{testLogLine1},
				RequestParams: map[string][]string{
					"some_query_param": {"app_123", "app_456"},
				},
				Labels: model.LabelSet{
					"job": "relabeling_job",
				},
				RelabelConfigs: []*relabel.Config{
					{
						SourceLabels: model.LabelNames{"__heroku_drain_host"},
						TargetLabel:  "host",
						Replacement:  "$1",
						Action:       relabel.Replace,
						Regex:        relabel.MustNewRegexp("(.*)"),
					},
					{
						SourceLabels: model.LabelNames{"__heroku_drain_app"},
						TargetLabel:  "app",
						Replacement:  "$1",
						Action:       relabel.Replace,
						Regex:        relabel.MustNewRegexp("(.*)"),
					},
					{
						SourceLabels: model.LabelNames{"__heroku_drain_proc"},
						TargetLabel:  "procID",
						Replacement:  "$1",
						Action:       relabel.Replace,
						Regex:        relabel.MustNewRegexp("(.*)"),
					},
					{
						SourceLabels: model.LabelNames{"__heroku_drain_param_some_query_param"},
						TargetLabel:  "query_param",
						Replacement:  "$1",
						Action:       relabel.Replace,
						Regex:        relabel.MustNewRegexp("(.*)"),
					},
				},
			},
			expectedEntries: []expectedEntry{
				{
					line: `[GIN] 2022/06/13 - 14:52:23 | 200 |    1.428101ms |  181.167.87.140 | GET      "/"
`,
					labels: model.LabelSet{
						"host":        "host",
						"app":         "app",
						"procID":      "web.1",
						"query_param": "app_123,app_456",
					},
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
			config := &scrapeconfig.HerokuDrainTargetConfig{
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

			req, err := makeDrainRequest(fmt.Sprintf("http://%s:%d", localhost, port), tc.args.RequestParams, tc.args.RequestBodies...)
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

func TestHerokuDrainTarget_UseIncomingTimestamp(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)

	// Create fake promtail client
	eh := fake.New(func() {})
	defer eh.Stop()

	serverConfig, port, err := getServerConfigWithAvailablePort()
	require.NoError(t, err, "error generating server config or finding open port")
	config := &scrapeconfig.HerokuDrainTargetConfig{
		Server:               serverConfig,
		Labels:               nil,
		UseIncomingTimestamp: true,
	}

	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	metrics := NewMetrics(prometheus.DefaultRegisterer)
	pt, err := NewTarget(metrics, logger, eh, "test_job", config, nil)
	require.NoError(t, err)
	defer func() {
		_ = pt.Stop()
	}()

	// Clear received lines after test case is ran
	defer eh.Clear()

	req, err := makeDrainRequest(fmt.Sprintf("http://%s:%d", localhost, port), make(map[string][]string), testLogLine1)
	require.NoError(t, err, "expected test drain request to be successfully created")
	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusNoContent, res.StatusCode, "expected no-content status code")

	waitForMessages(eh)

	// Make sure we didn't timeout
	require.Equal(t, 1, len(eh.Received()))

	expectedTs, err := time.Parse(time.RFC3339Nano, testLogLine1Timestamp)
	require.NoError(t, err, "expected expected timestamp to be parse correctly")
	require.Equal(t, expectedTs, eh.Received()[0].Timestamp, "expected entry timestamp to be overridden by received one")
}

func TestHerokuDrainTarget_ErrorOnNotPrometheusCompatibleJobName(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)

	// Create fake promtail client
	eh := fake.New(func() {})
	defer eh.Stop()

	serverConfig, _, err := getServerConfigWithAvailablePort()
	require.NoError(t, err, "error generating server config or finding open port")
	config := &scrapeconfig.HerokuDrainTargetConfig{
		Server:               serverConfig,
		Labels:               nil,
		UseIncomingTimestamp: true,
	}

	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	metrics := NewMetrics(prometheus.DefaultRegisterer)
	pt, err := NewTarget(metrics, logger, eh, "test-job", config, nil)
	require.Error(t, err, "expected an error from creating a heroku target with an invalid job name")
	// Cleanup target in the case test failed and target started correctly
	if err == nil {
		_ = pt.Stop()
	}
}

func TestHerokuDrainTarget_UseTenantIDHeaderIfPresent(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)

	// Create fake promtail client
	eh := fake.New(func() {})
	defer eh.Stop()

	serverConfig, port, err := getServerConfigWithAvailablePort()
	require.NoError(t, err, "error generating server config or finding open port")
	config := &scrapeconfig.HerokuDrainTargetConfig{
		Server:               serverConfig,
		Labels:               nil,
		UseIncomingTimestamp: true,
	}

	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	metrics := NewMetrics(prometheus.DefaultRegisterer)
	tenantIDRelabelConfig := []*relabel.Config{
		{
			SourceLabels: model.LabelNames{"__tenant_id__"},
			TargetLabel:  "tenant_id",
			Replacement:  "$1",
			Action:       relabel.Replace,
			Regex:        relabel.MustNewRegexp("(.*)"),
		},
	}
	pt, err := NewTarget(metrics, logger, eh, "test_job", config, tenantIDRelabelConfig)
	require.NoError(t, err)
	defer func() {
		_ = pt.Stop()
	}()

	// Clear received lines after test case is ran
	defer eh.Clear()

	req, err := makeDrainRequest(fmt.Sprintf("http://%s:%d", localhost, port), make(map[string][]string), testLogLine1)
	require.NoError(t, err, "expected test drain request to be successfully created")
	req.Header.Set("X-Scope-OrgID", "42")
	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusNoContent, res.StatusCode, "expected no-content status code")

	waitForMessages(eh)

	// Make sure we didn't timeout
	require.Equal(t, 1, len(eh.Received()))

	require.Equal(t, model.LabelValue("42"), eh.Received()[0].Labels[lokiClient.ReservedLabelTenantID])
	require.Equal(t, model.LabelValue("42"), eh.Received()[0].Labels["tenant_id"])
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
