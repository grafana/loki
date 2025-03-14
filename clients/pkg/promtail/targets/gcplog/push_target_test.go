package gcplog_test

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/grafana/loki/v3/clients/pkg/promtail/api"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/relabel"
	"github.com/stretchr/testify/require"

	lokiClient "github.com/grafana/loki/v3/clients/pkg/promtail/client"
	"github.com/grafana/loki/v3/clients/pkg/promtail/client/fake"
	"github.com/grafana/loki/v3/clients/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/v3/clients/pkg/promtail/targets/gcplog"
)

const localhost = "127.0.0.1"

const expectedMessageData = `{"insertId":"4affa858-e5f2-47f7-9254-e609b5c014d0","labels":{},"logName":"projects/test-project/logs/cloudaudit.googleapis.com%2Fdata_access","receiveTimestamp":"2022-09-06T18:07:43.417714046Z","resource":{"labels":{"cluster_name":"dev-us-central-42","location":"us-central1","project_id":"test-project"},"type":"k8s_cluster"},"timestamp":"2022-09-06T18:07:42.363113Z"}
`
const testPayload = `
{
	"message": {
		"attributes": {
			"logging.googleapis.com/timestamp": "2022-07-25T22:19:09.903683708Z"
		},
		"data": "eyJpbnNlcnRJZCI6IjRhZmZhODU4LWU1ZjItNDdmNy05MjU0LWU2MDliNWMwMTRkMCIsImxhYmVscyI6e30sImxvZ05hbWUiOiJwcm9qZWN0cy90ZXN0LXByb2plY3QvbG9ncy9jbG91ZGF1ZGl0Lmdvb2dsZWFwaXMuY29tJTJGZGF0YV9hY2Nlc3MiLCJyZWNlaXZlVGltZXN0YW1wIjoiMjAyMi0wOS0wNlQxODowNzo0My40MTc3MTQwNDZaIiwicmVzb3VyY2UiOnsibGFiZWxzIjp7ImNsdXN0ZXJfbmFtZSI6ImRldi11cy1jZW50cmFsLTQyIiwibG9jYXRpb24iOiJ1cy1jZW50cmFsMSIsInByb2plY3RfaWQiOiJ0ZXN0LXByb2plY3QifSwidHlwZSI6Ims4c19jbHVzdGVyIn0sInRpbWVzdGFtcCI6IjIwMjItMDktMDZUMTg6MDc6NDIuMzYzMTEzWiJ9Cg==",
		"messageId": "5187581549398349",
		"message_id": "5187581549398349",
		"publishTime": "2022-07-25T22:19:15.56Z",
		"publish_time": "2022-07-25T22:19:15.56Z"
	},
	"subscription": "projects/test-project/subscriptions/test"
}`

func makeGCPPushRequest(host string, body string) (*http.Request, error) {
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/gcp/api/v1/push", host), strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	return req, nil
}

func TestPushTarget(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)

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
					// Internal GCP Log entry attributes and labels
					{
						SourceLabels: model.LabelNames{"__gcp_logname"},
						Regex:        relabel.MustNewRegexp("(.*)"),
						Replacement:  "$1",
						TargetLabel:  "log_name",
						Action:       relabel.Replace,
					},
					{
						SourceLabels: model.LabelNames{"__gcp_resource_type"},
						Regex:        relabel.MustNewRegexp("(.*)"),
						Replacement:  "$1",
						TargetLabel:  "resource_type",
						Action:       relabel.Replace,
					},
					{
						SourceLabels: model.LabelNames{"__gcp_resource_labels_cluster_name"},
						Regex:        relabel.MustNewRegexp("(.*)"),
						Replacement:  "$1",
						TargetLabel:  "cluster",
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
						"subscription":     "projects/test-project/subscriptions/test",
						"log_name":         "projects/test-project/logs/cloudaudit.googleapis.com%2Fdata_access",
						"resource_type":    "k8s_cluster",
						"cluster":          "dev-us-central-42",
					},
					line: expectedMessageData,
				},
			},
		},
	}
	for name, tc := range cases {
		outerName := t.Name()
		t.Run(name, func(t *testing.T) {
			// Create fake promtail client
			eh := fake.New(func() {})
			defer eh.Stop()

			serverConfig, port, err := gcplog.GetServerConfigWithAvailablePort()
			require.NoError(t, err, "error generating server config or finding open port")
			config := &scrapeconfig.GcplogTargetConfig{
				Server:               serverConfig,
				Labels:               tc.args.Labels,
				UseIncomingTimestamp: false,
				SubscriptionType:     "push",
			}

			prometheus.DefaultRegisterer = prometheus.NewRegistry()
			metrics := gcplog.NewMetrics(prometheus.DefaultRegisterer)
			pt, err := gcplog.NewGCPLogTarget(metrics, logger, eh, tc.args.RelabelConfigs, outerName+"_test_job", config)
			require.NoError(t, err)
			defer func() {
				_ = pt.Stop()
			}()

			// Clear received lines after test case is ran
			defer eh.Clear()

			// Send some logs
			ts := time.Now()

			req, err := makeGCPPushRequest(fmt.Sprintf("http://%s:%d", localhost, port), tc.args.RequestBody)
			require.NoError(t, err, "expected request to be created successfully")
			res, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			require.Equal(t, http.StatusNoContent, res.StatusCode, "expected no-content status code")

			waitForMessages(eh)

			// Make sure we didn't timeout
			require.Equal(t, 1, len(eh.Received()))

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

func TestPushTarget_UseIncomingTimestamp(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)

	// Create fake promtail client
	eh := fake.New(func() {})
	defer eh.Stop()

	serverConfig, port, err := gcplog.GetServerConfigWithAvailablePort()
	require.NoError(t, err, "error generating server config or finding open port")
	config := &scrapeconfig.GcplogTargetConfig{
		Server:               serverConfig,
		Labels:               nil,
		UseIncomingTimestamp: true,
		SubscriptionType:     "push",
	}

	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	metrics := gcplog.NewMetrics(prometheus.DefaultRegisterer)
	pt, err := gcplog.NewGCPLogTarget(metrics, logger, eh, nil, t.Name()+"_test_job", config)
	require.NoError(t, err)
	defer func() {
		_ = pt.Stop()
	}()

	// Clear received lines after test case is ran
	defer eh.Clear()

	req, err := makeGCPPushRequest(fmt.Sprintf("http://%s:%d", localhost, port), testPayload)
	require.NoError(t, err, "expected request to be created successfully")
	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusNoContent, res.StatusCode, "expected no-content status code")

	waitForMessages(eh)

	// Make sure we didn't timeout
	require.Equal(t, 1, len(eh.Received()))

	expectedTs, err := time.Parse(time.RFC3339Nano, "2022-09-06T18:07:42.363113Z")
	require.NoError(t, err, "expected expected timestamp to be parse correctly")
	require.Equal(t, expectedTs, eh.Received()[0].Timestamp, "expected entry timestamp to be overridden by received one")
}

func TestPushTarget_UseTenantIDHeaderIfPresent(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)

	// Create fake promtail client
	eh := fake.New(func() {})
	defer eh.Stop()

	serverConfig, port, err := gcplog.GetServerConfigWithAvailablePort()
	require.NoError(t, err, "error generating server config or finding open port")
	config := &scrapeconfig.GcplogTargetConfig{
		Server:               serverConfig,
		Labels:               nil,
		UseIncomingTimestamp: true,
		SubscriptionType:     "push",
	}

	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	metrics := gcplog.NewMetrics(prometheus.DefaultRegisterer)
	tenantIDRelabelConfig := []*relabel.Config{
		{
			SourceLabels: model.LabelNames{"__tenant_id__"},
			Regex:        relabel.MustNewRegexp("(.*)"),
			Replacement:  "$1",
			TargetLabel:  "tenant_id",
			Action:       relabel.Replace,
		},
	}
	pt, err := gcplog.NewGCPLogTarget(metrics, logger, eh, tenantIDRelabelConfig, t.Name()+"_test_job", config)
	require.NoError(t, err)
	defer func() {
		_ = pt.Stop()
	}()

	// Clear received lines after test case is ran
	defer eh.Clear()

	req, err := makeGCPPushRequest(fmt.Sprintf("http://%s:%d", localhost, port), testPayload)
	require.NoError(t, err, "expected request to be created successfully")
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

func TestPushTarget_ErroneousPayloadsAreRejected(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)

	// Create fake promtail client
	eh := fake.New(func() {})
	defer eh.Stop()

	serverConfig, port, err := gcplog.GetServerConfigWithAvailablePort()
	require.NoError(t, err, "error generating server config or finding open port")
	config := &scrapeconfig.GcplogTargetConfig{
		Server:           serverConfig,
		Labels:           nil,
		SubscriptionType: "push",
	}

	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	metrics := gcplog.NewMetrics(prometheus.DefaultRegisterer)
	pt, err := gcplog.NewGCPLogTarget(metrics, logger, eh, nil, t.Name()+"_test_job", config)
	require.NoError(t, err)
	defer func() {
		_ = pt.Stop()
	}()

	// Clear received lines after test case is ran
	defer eh.Clear()

	for caseName, testPayload := range map[string]string{
		"invalid JSON": "{",
		"empty":        "{}",
		"missing subscription": `{
			"message": {
				"message_id": "123",
				"data": "some data"
			}
		}`,
		"missing message ID": `{
			"subscription": "sub",
			"message": {
				"data": "data"
			}
		}`,
		"missing data": `{
			"subscription": "sub",
			"message": {
				"data": "",
				"message_id":"123"
			}
		}`,
	} {
		t.Run(caseName, func(t *testing.T) {
			req, err := makeGCPPushRequest(fmt.Sprintf("http://%s:%d", localhost, port), testPayload)
			require.NoError(t, err, "expected request to be created successfully")
			res, err := http.DefaultClient.Do(req)
			res.Request.Body.Close()
			require.NoError(t, err)
			require.Equal(t, http.StatusBadRequest, res.StatusCode, "expected bad request status code")
		})
	}
}

// blockingEntryHandler implements an api.EntryHandler that has no space in it's receive channel, blocking when an api.Entry
// is sent down the pipe.
type blockingEntryHandler struct {
	ch   chan api.Entry
	once sync.Once
}

func newBlockingEntryHandler() *blockingEntryHandler {
	filledChannel := make(chan api.Entry)
	return &blockingEntryHandler{ch: filledChannel}
}

func (t *blockingEntryHandler) Chan() chan<- api.Entry {
	return t.ch
}

func (t *blockingEntryHandler) Stop() {
	t.once.Do(func() { close(t.ch) })
}

func TestPushTarget_UsePushTimeout(t *testing.T) {
	w := log.NewSyncWriter(os.Stderr)
	logger := log.NewLogfmtLogger(w)

	eh := newBlockingEntryHandler()
	defer eh.Stop()

	serverConfig, port, err := gcplog.GetServerConfigWithAvailablePort()
	require.NoError(t, err, "error generating server config or finding open port")
	config := &scrapeconfig.GcplogTargetConfig{
		Server:               serverConfig,
		Labels:               nil,
		UseIncomingTimestamp: true,
		SubscriptionType:     "push",
		PushTimeout:          time.Second,
	}

	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	metrics := gcplog.NewMetrics(prometheus.DefaultRegisterer)
	tenantIDRelabelConfig := []*relabel.Config{
		{
			SourceLabels: model.LabelNames{"__tenant_id__"},
			Regex:        relabel.MustNewRegexp("(.*)"),
			Replacement:  "$1",
			TargetLabel:  "tenant_id",
			Action:       relabel.Replace,
		},
	}
	pt, err := gcplog.NewGCPLogTarget(metrics, logger, eh, tenantIDRelabelConfig, t.Name()+"_test_job", config)
	require.NoError(t, err)
	defer func() {
		_ = pt.Stop()
	}()

	req, err := makeGCPPushRequest(fmt.Sprintf("http://%s:%d", localhost, port), testPayload)
	require.NoError(t, err, "expected request to be created successfully")
	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusServiceUnavailable, res.StatusCode, "expected timeout response")
}

func waitForMessages(eh *fake.Client) {
	countdown := 1000
	for len(eh.Received()) != 1 && countdown > 0 {
		time.Sleep(1 * time.Millisecond)
		countdown--
	}
}
