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

func Test_Push(t *testing.T) {
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
	defer mock.Close()

	// without TLS
	push, err := NewPush(mock.Listener.Addr().String(), "test1", 2*time.Second, config.DefaultHTTPClientConfig, "name", "loki-canary", "stream", "stdout", false, nil, "", "", "", "", "", &backoff, log.NewNopLogger())
	require.NoError(t, err)
	ts, payload := testPayload()
	push.WriteEntry(ts, payload)
	resp := <-responses
	assertResponse(t, resp, false, labelSet("name", "loki-canary", "stream", "stdout"), ts, payload)

	// with basic Auth
	push, err = NewPush(mock.Listener.Addr().String(), "test1", 2*time.Second, config.DefaultHTTPClientConfig, "name", "loki-canary", "stream", "stdout", false, nil, "", "", "", testUsername, testPassword, &backoff, log.NewNopLogger())
	require.NoError(t, err)
	ts, payload = testPayload()
	push.WriteEntry(ts, payload)
	resp = <-responses
	assertResponse(t, resp, true, labelSet("name", "loki-canary", "stream", "stdout"), ts, payload)

	// with custom labels
	push, err = NewPush(mock.Listener.Addr().String(), "test1", 2*time.Second, config.DefaultHTTPClientConfig, "name", "loki-canary", "pod", "abc", false, nil, "", "", "", testUsername, testPassword, &backoff, log.NewNopLogger())
	require.NoError(t, err)
	ts, payload = testPayload()
	push.WriteEntry(ts, payload)
	resp = <-responses
	assertResponse(t, resp, true, labelSet("name", "loki-canary", "pod", "abc"), ts, payload)
}

// Test helpers

func assertResponse(t *testing.T, resp response, testAuth bool, labels model.LabelSet, ts time.Time, payload string) {
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

	// assert stream labels
	require.Len(t, resp.pushReq.Streams, 1)
	assert.Equal(t, labels.String(), resp.pushReq.Streams[0].Labels)
	assert.Equal(t, uint64(labels.Fingerprint()), resp.pushReq.Streams[0].Hash)

	// assert log entry
	require.Len(t, resp.pushReq.Streams, 1)
	require.Len(t, resp.pushReq.Streams[0].Entries, 1)
	assert.Equal(t, payload, resp.pushReq.Streams[0].Entries[0].Line)
	assert.Equal(t, ts, resp.pushReq.Streams[0].Entries[0].Timestamp)
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

		var (
			username, password string
		)

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
