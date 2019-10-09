package client

import (
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/flagext"
	"github.com/grafana/loki/pkg/logproto"
	lokiflag "github.com/grafana/loki/pkg/util/flagext"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
)

var (
	logEntries = []entry{
		{labels: model.LabelSet{}, Entry: logproto.Entry{Timestamp: time.Unix(1, 0).UTC(), Line: "line1"}},
		{labels: model.LabelSet{}, Entry: logproto.Entry{Timestamp: time.Unix(2, 0).UTC(), Line: "line2"}},
		{labels: model.LabelSet{}, Entry: logproto.Entry{Timestamp: time.Unix(3, 0).UTC(), Line: "line3"}},
		{labels: model.LabelSet{"__tenant_id__": "tenant-1"}, Entry: logproto.Entry{Timestamp: time.Unix(4, 0).UTC(), Line: "line4"}},
		{labels: model.LabelSet{"__tenant_id__": "tenant-1"}, Entry: logproto.Entry{Timestamp: time.Unix(5, 0).UTC(), Line: "line5"}},
		{labels: model.LabelSet{"__tenant_id__": "tenant-2"}, Entry: logproto.Entry{Timestamp: time.Unix(6, 0).UTC(), Line: "line6"}},
	}
)

type receivedReq struct {
	tenantID string
	pushReq  logproto.PushRequest
}

func TestClient_Handle(t *testing.T) {
	tests := map[string]struct {
		clientBatchSize      int
		clientBatchWait      time.Duration
		clientMaxRetries     int
		clientTenantID       string
		serverResponseStatus int
		inputEntries         []entry
		inputDelay           time.Duration
		expectedReqs         []receivedReq
		expectedMetrics      string
	}{
		"batch log entries together until the batch size is reached": {
			clientBatchSize:      10,
			clientBatchWait:      100 * time.Millisecond,
			clientMaxRetries:     3,
			serverResponseStatus: 200,
			inputEntries:         []entry{logEntries[0], logEntries[1], logEntries[2]},
			expectedReqs: []receivedReq{
				{
					tenantID: "",
					pushReq:  logproto.PushRequest{Streams: []*logproto.Stream{{Labels: "{}", Entries: []logproto.Entry{logEntries[0].Entry, logEntries[1].Entry}}}},
				},
				{
					tenantID: "",
					pushReq:  logproto.PushRequest{Streams: []*logproto.Stream{{Labels: "{}", Entries: []logproto.Entry{logEntries[2].Entry}}}},
				},
			},
			expectedMetrics: `
				# HELP promtail_sent_entries_total Number of log entries sent to the ingester.
				# TYPE promtail_sent_entries_total counter
				promtail_sent_entries_total{host="__HOST__"} 3.0
			`,
		},
		"batch log entries together until the batch wait time is reached": {
			clientBatchSize:      10,
			clientBatchWait:      100 * time.Millisecond,
			clientMaxRetries:     3,
			serverResponseStatus: 200,
			inputEntries:         []entry{logEntries[0], logEntries[1]},
			inputDelay:           110 * time.Millisecond,
			expectedReqs: []receivedReq{
				{
					tenantID: "",
					pushReq:  logproto.PushRequest{Streams: []*logproto.Stream{{Labels: "{}", Entries: []logproto.Entry{logEntries[0].Entry}}}},
				},
				{
					tenantID: "",
					pushReq:  logproto.PushRequest{Streams: []*logproto.Stream{{Labels: "{}", Entries: []logproto.Entry{logEntries[1].Entry}}}},
				},
			},
			expectedMetrics: `
				# HELP promtail_sent_entries_total Number of log entries sent to the ingester.
				# TYPE promtail_sent_entries_total counter
				promtail_sent_entries_total{host="__HOST__"} 2.0
			`,
		},
		"retry send a batch up to backoff's max retries in case the server responds with a 5xx": {
			clientBatchSize:      10,
			clientBatchWait:      10 * time.Millisecond,
			clientMaxRetries:     3,
			serverResponseStatus: 500,
			inputEntries:         []entry{logEntries[0]},
			expectedReqs: []receivedReq{
				{
					tenantID: "",
					pushReq:  logproto.PushRequest{Streams: []*logproto.Stream{{Labels: "{}", Entries: []logproto.Entry{logEntries[0].Entry}}}},
				},
				{
					tenantID: "",
					pushReq:  logproto.PushRequest{Streams: []*logproto.Stream{{Labels: "{}", Entries: []logproto.Entry{logEntries[0].Entry}}}},
				},
				{
					tenantID: "",
					pushReq:  logproto.PushRequest{Streams: []*logproto.Stream{{Labels: "{}", Entries: []logproto.Entry{logEntries[0].Entry}}}},
				},
			},
			expectedMetrics: `
				# HELP promtail_dropped_entries_total Number of log entries dropped because failed to be sent to the ingester after all retries.
				# TYPE promtail_dropped_entries_total counter
				promtail_dropped_entries_total{host="__HOST__"} 1.0
			`,
		},
		"do not retry send a batch in case the server responds with a 4xx": {
			clientBatchSize:      10,
			clientBatchWait:      10 * time.Millisecond,
			clientMaxRetries:     3,
			serverResponseStatus: 400,
			inputEntries:         []entry{logEntries[0]},
			expectedReqs: []receivedReq{
				{
					tenantID: "",
					pushReq:  logproto.PushRequest{Streams: []*logproto.Stream{{Labels: "{}", Entries: []logproto.Entry{logEntries[0].Entry}}}},
				},
			},
			expectedMetrics: `
				# HELP promtail_dropped_entries_total Number of log entries dropped because failed to be sent to the ingester after all retries.
				# TYPE promtail_dropped_entries_total counter
				promtail_dropped_entries_total{host="__HOST__"} 1.0
			`,
		},
		"batch log entries together honoring the client tenant ID": {
			clientBatchSize:      100,
			clientBatchWait:      100 * time.Millisecond,
			clientMaxRetries:     3,
			clientTenantID:       "tenant-default",
			serverResponseStatus: 200,
			inputEntries:         []entry{logEntries[0], logEntries[1]},
			expectedReqs: []receivedReq{
				{
					tenantID: "tenant-default",
					pushReq:  logproto.PushRequest{Streams: []*logproto.Stream{{Labels: "{}", Entries: []logproto.Entry{logEntries[0].Entry, logEntries[1].Entry}}}},
				},
			},
			expectedMetrics: `
				# HELP promtail_sent_entries_total Number of log entries sent to the ingester.
				# TYPE promtail_sent_entries_total counter
				promtail_sent_entries_total{host="__HOST__"} 2.0
			`,
		},
		"batch log entries together honoring the tenant ID overridden while processing the pipeline stages": {
			clientBatchSize:      100,
			clientBatchWait:      100 * time.Millisecond,
			clientMaxRetries:     3,
			clientTenantID:       "tenant-default",
			serverResponseStatus: 200,
			inputEntries:         []entry{logEntries[0], logEntries[3], logEntries[4], logEntries[5]},
			expectedReqs: []receivedReq{
				{
					tenantID: "tenant-default",
					pushReq:  logproto.PushRequest{Streams: []*logproto.Stream{{Labels: "{}", Entries: []logproto.Entry{logEntries[0].Entry}}}},
				},
				{
					tenantID: "tenant-1",
					pushReq:  logproto.PushRequest{Streams: []*logproto.Stream{{Labels: "{}", Entries: []logproto.Entry{logEntries[3].Entry, logEntries[4].Entry}}}},
				},
				{
					tenantID: "tenant-2",
					pushReq:  logproto.PushRequest{Streams: []*logproto.Stream{{Labels: "{}", Entries: []logproto.Entry{logEntries[5].Entry}}}},
				},
			},
			expectedMetrics: `
				# HELP promtail_sent_entries_total Number of log entries sent to the ingester.
				# TYPE promtail_sent_entries_total counter
				promtail_sent_entries_total{host="__HOST__"} 4.0
			`,
		},
	}

	for testName, testData := range tests {
		t.Run(testName, func(t *testing.T) {
			// Reset metrics
			sentEntries.Reset()
			droppedEntries.Reset()

			// Create a buffer channel where we do enqueue received requests
			receivedReqsChan := make(chan receivedReq, 10)

			// Start a local HTTP server
			server := httptest.NewServer(createServerHandler(receivedReqsChan, testData.serverResponseStatus))
			require.NotNil(t, server)
			defer server.Close()

			// Get the URL at which the local test server is listening to
			serverURL := flagext.URLValue{}
			err := serverURL.Set(server.URL)
			require.NoError(t, err)

			// Instance the client
			cfg := Config{
				URL:            serverURL,
				BatchWait:      testData.clientBatchWait,
				BatchSize:      testData.clientBatchSize,
				Client:         config.HTTPClientConfig{},
				BackoffConfig:  util.BackoffConfig{MinBackoff: 1 * time.Millisecond, MaxBackoff: 2 * time.Millisecond, MaxRetries: testData.clientMaxRetries},
				ExternalLabels: lokiflag.LabelSet{},
				Timeout:        1 * time.Second,
				TenantID:       testData.clientTenantID,
			}

			c, err := New(cfg, log.NewNopLogger())
			require.NoError(t, err)

			// Send all the input log entries
			for i, logEntry := range testData.inputEntries {
				err = c.Handle(logEntry.labels, logEntry.Timestamp, logEntry.Line)
				require.NoError(t, err)

				if testData.inputDelay > 0 && i < len(testData.inputEntries)-1 {
					time.Sleep(testData.inputDelay)
				}
			}

			// Wait until the expected push requests are received (with a timeout)
			deadline := time.Now().Add(1 * time.Second)
			for len(receivedReqsChan) < len(testData.expectedReqs) && time.Now().Before(deadline) {
				time.Sleep(5 * time.Millisecond)
			}

			// Stop the client: it waits until the current batch is sent
			c.Stop()
			close(receivedReqsChan)

			// Get all push requests received on the server side
			receivedReqs := make([]receivedReq, 0)
			for req := range receivedReqsChan {
				receivedReqs = append(receivedReqs, req)
			}

			// Due to implementation details (maps iteration ordering is random) we just check
			// that the expected requests are equal to the received requests, without checking
			// the exact order which is not guaranteed in case of multi-tenant
			require.ElementsMatch(t, testData.expectedReqs, receivedReqs)

			expectedMetrics := strings.Replace(testData.expectedMetrics, "__HOST__", serverURL.Host, -1)
			err = testutil.GatherAndCompare(prometheus.DefaultGatherer, strings.NewReader(expectedMetrics), "promtail_sent_entries_total", "promtail_dropped_entries_total")
			assert.NoError(t, err)
		})
	}
}

func createServerHandler(receivedReqsChan chan receivedReq, status int) http.HandlerFunc {
	return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		// Parse the request
		var pushReq logproto.PushRequest
		if _, err := util.ParseProtoReader(req.Context(), req.Body, int(req.ContentLength), math.MaxInt32, &pushReq, util.RawSnappy); err != nil {
			rw.WriteHeader(500)
			return
		}

		receivedReqsChan <- receivedReq{
			tenantID: req.Header.Get("X-Scope-OrgID"),
			pushReq:  pushReq,
		}

		rw.WriteHeader(status)
	})
}
