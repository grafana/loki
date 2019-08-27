package client

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/flagext"
	"github.com/grafana/loki/pkg/logproto"
	lokiflag "github.com/grafana/loki/pkg/util/flagext"
	"github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
)

func TestClient_Handle(t *testing.T) {
	logEntries := []entry{
		{labels: model.LabelSet{}, Entry: logproto.Entry{Timestamp: time.Unix(1, 0).UTC(), Line: "line1"}},
		{labels: model.LabelSet{}, Entry: logproto.Entry{Timestamp: time.Unix(2, 0).UTC(), Line: "line2"}},
		{labels: model.LabelSet{}, Entry: logproto.Entry{Timestamp: time.Unix(3, 0).UTC(), Line: "line3"}},
	}

	tests := map[string]struct {
		clientBatchSize      int
		clientBatchWait      time.Duration
		clientMaxRetries     int
		serverResponseStatus int
		inputEntries         []entry
		inputDelay           time.Duration
		expectedBatches      [][]*logproto.Stream
	}{
		"batch log entries together until the batch size is reached": {
			clientBatchSize:      10,
			clientBatchWait:      100 * time.Millisecond,
			clientMaxRetries:     3,
			serverResponseStatus: 200,
			inputEntries:         []entry{logEntries[0], logEntries[1], logEntries[2]},
			expectedBatches: [][]*logproto.Stream{
				{
					{Labels: "{}", Entries: []logproto.Entry{logEntries[0].Entry, logEntries[1].Entry}},
				},
				{
					{Labels: "{}", Entries: []logproto.Entry{logEntries[2].Entry}},
				},
			},
		},
		"batch log entries together until the batch wait time is reached": {
			clientBatchSize:      10,
			clientBatchWait:      100 * time.Millisecond,
			clientMaxRetries:     3,
			serverResponseStatus: 200,
			inputEntries:         []entry{logEntries[0], logEntries[1]},
			inputDelay:           110 * time.Millisecond,
			expectedBatches: [][]*logproto.Stream{
				{
					{Labels: "{}", Entries: []logproto.Entry{logEntries[0].Entry}},
				},
				{
					{Labels: "{}", Entries: []logproto.Entry{logEntries[1].Entry}},
				},
			},
		},
		"retry send a batch up to backoff's max retries in case the server responds with a 5xx": {
			clientBatchSize:      10,
			clientBatchWait:      10 * time.Millisecond,
			clientMaxRetries:     3,
			serverResponseStatus: 500,
			inputEntries:         []entry{logEntries[0]},
			expectedBatches: [][]*logproto.Stream{
				{
					{Labels: "{}", Entries: []logproto.Entry{logEntries[0].Entry}},
				},
				{
					{Labels: "{}", Entries: []logproto.Entry{logEntries[0].Entry}},
				},
				{
					{Labels: "{}", Entries: []logproto.Entry{logEntries[0].Entry}},
				},
			},
		},
		"do not retry send a batch in case the server responds with a 4xx": {
			clientBatchSize:      10,
			clientBatchWait:      10 * time.Millisecond,
			clientMaxRetries:     3,
			serverResponseStatus: 400,
			inputEntries:         []entry{logEntries[0]},
			expectedBatches: [][]*logproto.Stream{
				{
					{Labels: "{}", Entries: []logproto.Entry{logEntries[0].Entry}},
				},
			},
		},
	}

	for testName, testData := range tests {
		t.Run(testName, func(t *testing.T) {
			// Create a buffer channel where we do enqueue received requests
			receivedReqsChan := make(chan logproto.PushRequest, 10)

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
			for len(receivedReqsChan) < len(testData.expectedBatches) && time.Now().Before(deadline) {
				time.Sleep(5 * time.Millisecond)
			}

			// Stop the client: it waits until the current batch is sent
			c.Stop()
			close(receivedReqsChan)

			// Get all push requests received on the server side
			receivedReqs := make([]logproto.PushRequest, 0)
			for req := range receivedReqsChan {
				receivedReqs = append(receivedReqs, req)
			}

			require.Equal(t, len(testData.expectedBatches), len(receivedReqs))
			for i, batch := range receivedReqs {
				assert.Equal(t, testData.expectedBatches[i], batch.Streams)
			}
		})
	}
}

func createServerHandler(receivedReqsChan chan logproto.PushRequest, status int) http.HandlerFunc {
	return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		// Parse the request
		var pushReq logproto.PushRequest
		if _, err := util.ParseProtoReader(req.Context(), req.Body, &pushReq, util.RawSnappy); err != nil {
			rw.WriteHeader(500)
			return
		}

		receivedReqsChan <- pushReq
		rw.WriteHeader(status)
	})
}
