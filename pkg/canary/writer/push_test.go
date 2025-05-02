package writer

import (
	"encoding/base64"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
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

// basic testing to make sure we create the correct pusher or buffered pusher
func Test_CreatePusher(t *testing.T) {
	testCfg := newTestConfig(t)
	defer func() {
		testCfg.mock.Close()
	}()

	// -1 should not return a pusher
	_, err := newPush(testCfg, -1)
	require.Error(t, err)

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

	push, err = newPush(testCfg, 20)
	require.NoError(t, err)

	if _, ok := push.(*BatchedPush); !ok {
		require.Fail(t, "NewPush returned an invalid type w/logBatchSize == 20")
	}
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

// test batching log lines and ensure the testing resp contains 10 entries
func Test_BasicPushWithBatching(t *testing.T) {
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

// test batching log lines in groups of 5
// we need to ensure the responses are in groups of 5, even
// as we iteratively add more and more logs...
func Test_LongRunningPushWithBatching(t *testing.T) {
}

func Test_PushWithBatchingTerminate(t *testing.T) {
	testCfg := newTestConfig(t)
	defer func() {
		testCfg.mock.Close()
	}()

	_, err := newPush(testCfg, 5)
	require.NoError(t, err)

	// go func() {
	// 	resp := <-testCfg.responses
	// 	assertResponse(t, resp, false, labelSet("name", "loki-canary", "stream", "stdout"), ts, payload, 10)
	// }()
}

// Test helpers

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

type response struct {
	tenantID           string
	pushReq            logproto.PushRequest
	contentType        string
	userAgent          string
	username, password string
}

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
