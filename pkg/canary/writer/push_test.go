package writer

import (
	"encoding/base64"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/backoff"
	"github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util"
)

const (
	testTenant   = "test1"
	testUsername = "canary"
	testPassword = "secret"
)

type testConfig struct {
	responses chan response
	backoff   backoff.Config
	mock      *httptest.Server
}

type response struct {
	tenantID           string
	pushReq            logproto.PushRequest
	contentType        string
	userAgent          string
	username, password string
}

// basic testing to make sure we create the correct pusher or buffered pusher
func Test_CreatePusher(t *testing.T) {
	testCfg := newTestConfig(t)
	defer func() {
		testCfg.mock.Close()
	}()

	// batch size of 0/1 means don't batch...
	push, err := newPush(testCfg, 0)
	require.NoError(t, err)
	if _, ok := push.(*Push); !ok {
		require.Fail(t, "NewPush returned an invalid type w/logBatchSize == 0")
	}

	push, err = newPush(testCfg, 1)
	require.NoError(t, err)
	if _, ok := push.(*Push); !ok {
		require.Fail(t, "NewPush returned an invalid type w/logBatchSize == 1")
	}

	push, err = newPush(testCfg, 10)
	require.NoError(t, err)
	if _, ok := push.(*BatchedPush); !ok {
		require.Fail(t, "NewPush returned an invalid type w/logBatchSize == 10")
	}

	// batch size of -1 is nonsensical
	_, err = newPush(testCfg, -1)
	require.Error(t, err)
}

// basic test with a few diff HTTP settings
func Test_Push(t *testing.T) {
	testCfg := newTestConfig(t)
	defer func() {
		testCfg.mock.Close()
	}()

	// without TLS
	push, err := newPush(testCfg, 1)
	require.NoError(t, err)
	ts, payload := testPayload()
	push.WriteEntry(ts, payload)
	resp := <-testCfg.responses
	assertResponse(t, resp, false, labelSet("name", "loki-canary", "stream", "stdout"), ts, payload, 1)

	// with basic Auth
	push, err = newPushWithCredentials(testCfg, testUsername, testPassword, 1)
	require.NoError(t, err)
	ts, payload = testPayload()
	push.WriteEntry(ts, payload)
	resp = <-testCfg.responses
	assertResponse(t, resp, true, labelSet("name", "loki-canary", "stream", "stdout"), ts, payload, 1)

	// with custom labels
	push, err = newPushWithCredentialsAndStreamNameValue(testCfg, testUsername, testPassword, "pod", "abc", 1)
	require.NoError(t, err)
	ts, payload = testPayload()
	push.WriteEntry(ts, payload)
	resp = <-testCfg.responses
	assertResponse(t, resp, true, labelSet("name", "loki-canary", "pod", "abc"), ts, payload, 1)
}

// test batching log lines and ensure the testing resp contains exactly 10 unique entries
func Test_BatchedPush(t *testing.T) {
	testCfg := newTestConfig(t)
	defer func() {
		testCfg.mock.Close()
	}()

	// test batching 10 logs at-a-time
	push, err := newPush(testCfg, 10)
	require.NoError(t, err)

	ts, payload := testPayload()
	for range 10 {
		ts, payload = testPayload()
		push.WriteEntry(ts, payload)
	}
	resp := <-testCfg.responses
	assertResponse(t, resp, false, labelSet("name", "loki-canary", "stream", "stdout"), ts, payload, 10)
}

// test sending 25 log lines in batches of 5
// this ensures the pusher only sends 25 unique logs
func Test_LargeBatchedPush(t *testing.T) {
	testCfg := newTestConfig(t)
	defer func() {
		testCfg.mock.Close()
	}()

	// logsSent represents the lines which were sent by the BatchedPusher, when
	// we receive a log line, we increment the entry in the map by 1
	logBatches, logsSent := newLogBatch(5, 5)

	push, err := newPush(testCfg, 5)
	require.NoError(t, err)

	// monitor the logs which were sent
	go logbatchMonitor(t, testCfg, logsSent, 25)

	var wg sync.WaitGroup
	for _, logs := range logBatches {
		// routine to send logs through the pusher
		wg.Add(1)
		go func() {
			defer func() {
				wg.Done()
			}()

			for _, log := range logs {
				push.WriteEntry(log.ts, log.entry)
			}
		}()
	}

	// wait until all logs are pushed and the monitor function are done running
	wg.Wait()
}

// test sending a batch of logs, and then waiting for the timeout value to
// pass.  This is a bit of a painful test given we wait for 'n' seconds for
// the timeout to expire, but it ensures we know the batched push will send
// whichever logs were received even if it stops receiving more
func Test_ForceTimeoutBatchedPush(t *testing.T) {
	testCfg := newTestConfig(t)
	defer func() {
		testCfg.mock.Close()
	}()

	// don't worry about checking each log is sent once -- we test that elsewhere
	logBatch, _ := newLogBatch(1, 5)

	push, err := newPush(testCfg, 20)
	require.NoError(t, err)

	for _, l := range logBatch[0] {
		push.WriteEntry(l.ts, l.entry)
	}

	time.Sleep(time.Second*DefaultLogBatchTimeout - 1)
	resp := <-testCfg.responses
	require.Len(t, resp.pushReq.Streams, 5)
}

// test sending batches of logs and then terminating the client.  the last sent
// logs should be sent before the client terminates.
func Test_TerminateBatchedPush(t *testing.T) {
	testCfg := newTestConfig(t)
	defer func() {
		testCfg.mock.Close()
	}()

	// don't worry about checking each is sent only once -- we test that elsewehere
	logBatches, _ := newLogBatch(1, 9)

	// large batch size to ensure no logs go out before we terminate
	push, err := newPush(testCfg, 20)
	require.NoError(t, err)

	for _, l := range logBatches[0] {
		// don't monitor the push -- the logs won't send until we stop the client
		push.WriteEntry(l.ts, l.entry)
	}

	// hacky, but sleep for 5s to ensure the logs have made it through the push channel...
	time.Sleep(time.Second * 5)
	push.Stop()

	resp := <-testCfg.responses
	require.Len(t, resp.pushReq.Streams, 9)
}

// -- Helper Functions for Testing Valid Responses -- //

// function which monitors the test config response channel, waiting for 'n'
// log responses to be returned.  Moreover, this function monitors the log
// lines received in the channel to ensure each log line is only sent once
func logbatchMonitor(t *testing.T, testCfg testConfig, logsSent map[string]int, expectedCount int) {
	t.Helper()

	var resp response
	logCount := 0
	for {
		resp = <-testCfg.responses

		for _, log := range resp.pushReq.Streams {
			for _, entry := range log.Entries {
				logsSent[entry.Line]++ // received
				logCount++             // total # expected...
			}
		}

		// once we have at least the
		if logCount >= expectedCount {
			break
		}
	}

	// make sure each of the entries in the logsSent was received once
	for log := range logsSent {
		require.Equal(t, logsSent[log], 1)
	}

	// sanity-check: the # of sent logs is 5x5
	require.Len(t, logsSent, expectedCount)
}

func assertResponse(t *testing.T, resp response, testAuth bool, labels model.LabelSet, ts time.Time, payload string, streamCount int) {
	t.Helper()

	// assert metadata
	assert.Equal(t, testTenant, resp.tenantID)

	var expUser, expPass string

	if testAuth {
		expUser = testUsername
		expPass = testPassword
	}

	assert.Equal(t, expUser, resp.username)
	assert.Equal(t, expPass, resp.password)
	assert.Equal(t, defaultContentType, resp.contentType)
	assert.Equal(t, defaultUserAgent, resp.userAgent)

	// assert stream count and labels
	lastStream := resp.pushReq.Streams[len(resp.pushReq.Streams)-1]

	require.Len(t, resp.pushReq.Streams, streamCount)
	assert.Equal(t, labels.String(), lastStream.Labels)
	assert.Equal(t, uint64(labels.Fingerprint()), lastStream.Hash)

	// assert log entry
	require.Len(t, resp.pushReq.Streams[0].Entries, 1)
	assert.Equal(t, payload, lastStream.Entries[0].Line)
	assert.Equal(t, ts, lastStream.Entries[0].Timestamp)
}

// -- Utility Functions -- //

func createServerHandler(responses chan response) http.HandlerFunc {
	return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		// Parse the request
		var pushReq logproto.PushRequest
		if err := util.ParseProtoReader(req.Context(), req.Body, int(req.ContentLength), math.MaxInt32, &pushReq, util.RawSnappy); err != nil {
			rw.WriteHeader(500)
			return
		}

		var username, password string

		basicAuth := req.Header.Get("Authorization")
		if basicAuth != "" {
			encoded := strings.TrimPrefix(basicAuth, "Basic ") // now we have just encoded `username:password`
			decoded, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				rw.WriteHeader(500)
				return
			}
			toks := strings.FieldsFunc(string(decoded), func(r rune) bool {
				return r == ':'
			})
			username, password = toks[0], toks[1]
		}

		responses <- response{
			tenantID:    req.Header.Get("X-Scope-OrgID"),
			contentType: req.Header.Get("Content-Type"),
			userAgent:   req.Header.Get("User-Agent"),
			username:    username,
			password:    password,
			pushReq:     pushReq,
		}

		rw.WriteHeader(http.StatusOK)
	})
}

func labelSet(keyVals ...string) model.LabelSet {
	if len(keyVals)%2 != 0 {
		panic("not matching key-value pairs")
	}

	lbs := model.LabelSet{}

	i := 0
	j := i + 1
	for i < len(keyVals)-1 {
		lbs[model.LabelName(keyVals[i])] = model.LabelValue(keyVals[i+1])
		i += 2
		j += 2
	}

	return lbs
}

// creates an nested list of `entry` structs which are sent via the
// `EntryWriter` to loki.  Returns the 2-dimensional entry/log array
// as well as a map of each log line initialized to 0.  This map can
// be used to ensure each log was sent once and only once
func newLogBatch(batchCount int, batchSize int) ([][]entry, map[string]int) {
	logBatches := [][]entry{}
	logsSent := map[string]int{}

	for i := range batchCount {
		var logs []entry
		for j := range batchSize {
			logs = append(logs, entry{
				ts:    time.Now().Add(1 * time.Second),
				entry: fmt.Sprintf("test log %d %d", i, j),
			})
			logsSent[fmt.Sprintf("test log %d %d", i, j)] = 0 // sent, not yet received
		}
		logBatches = append(logBatches, logs)
	}

	return logBatches, logsSent
}

func testPayload() (time.Time, string) {
	ts := time.Now().UTC()
	payload := fmt.Sprintf(LogEntry, fmt.Sprint(ts.UnixNano()), "pppppp")

	return ts, payload
}

// create a new `testConfig` struct with mock objects for
// testing the ability to push logs
func newTestConfig(t *testing.T) testConfig {
	t.Helper()

	// create dummy loki server
	responses := make(chan response, 1) // buffered not to block the response handler
	backoff := backoff.Config{
		MinBackoff: 300 * time.Millisecond,
		MaxBackoff: 5 * time.Minute,
		MaxRetries: 10,
	}

	// mock loki server
	mock := httptest.NewServer(createServerHandler(responses))
	require.NotNil(t, mock)

	return testConfig{
		responses: responses,
		backoff:   backoff,
		mock:      mock,
	}
}

// create a new `EventWriter` with standard everything...
func newPush(testCfg testConfig, logBatchSize int) (EntryWriter, error) {
	return newPushWithCredentials(testCfg, "", "", logBatchSize)
}

// create a new `EventWriter` with credentials
func newPushWithCredentials(testCfg testConfig, username, password string, logBatchSize int) (EntryWriter, error) {
	return newPushWithCredentialsAndStreamNameValue(testCfg, username, password, "stream", "stdout", logBatchSize)
}

// create a new `EventWriter` with custom credentials and labels
func newPushWithCredentialsAndStreamNameValue(testCfg testConfig, username, password, streamName, streamValue string, logBatchSize int) (EntryWriter, error) {
	return NewPush(
		testCfg.mock.Listener.Addr().String(),
		"test1",
		2*time.Second,
		config.DefaultHTTPClientConfig,
		"name",
		"loki-canary",
		streamName,
		streamValue,
		false,
		nil,
		"",
		"",
		"",
		username,
		password,
		&testCfg.backoff,
		logBatchSize,
		log.NewNopLogger(),
	)
}
