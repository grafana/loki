package aggregation

import (
	"encoding/base64"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/backoff"
	"github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

const (
	testTenant   = "test1"
	testUsername = "user"
	testPassword = "secret"
	LogEntry     = "%s %s\n"
)

func Test_Push(t *testing.T) {
	lbls := labels.New(labels.Label{Name: "test", Value: "test"})
	structuredMetadata := []logproto.LabelAdapter{
		{Name: constants.LevelLabel, Value: "info"},
	}

	// create dummy loki server
	responses := make(chan response, 1) // buffered not to block the response handler
	backoff := backoff.Config{
		MinBackoff: 300 * time.Millisecond,
		MaxBackoff: 1 * time.Minute,
		MaxRetries: 1,
	}

	t.Run("sends log entry to loki server without TLS", func(t *testing.T) {
		// mock loki server
		mock := httptest.NewServer(createServerHandler(responses))
		require.NotNil(t, mock)
		defer mock.Close()

		// without TLS
		push, err := NewPush(
			mock.Listener.Addr().String(),
			"test1",
			2*time.Second,
			1*time.Second,
			config.DefaultHTTPClientConfig,
			"", "",
			false,
			&backoff,
			log.NewNopLogger(),
			NewMetrics(nil),
		)
		require.NoError(t, err)
		ts, payload := testPayload()
		push.WriteEntry(ts, payload, lbls, structuredMetadata)
		resp := <-responses
		assertResponse(t, resp, false, labelSet("test", "test"), ts, payload)
	})

	t.Run("sends log entry to loki server with basic auth", func(t *testing.T) {
		// mock loki server
		mock := httptest.NewServer(createServerHandler(responses))
		require.NotNil(t, mock)
		defer mock.Close()

		// with basic Auth
		push, err := NewPush(
			mock.Listener.Addr().String(),
			"test1",
			2*time.Second,
			1*time.Second,
			config.DefaultHTTPClientConfig,
			"user", "secret",
			false,
			&backoff,
			log.NewNopLogger(), NewMetrics(nil),
		)
		require.NoError(t, err)
		ts, payload := testPayload()
		push.WriteEntry(ts, payload, lbls, structuredMetadata)
		resp := <-responses
		assertResponse(t, resp, true, labelSet("test", "test"), ts, payload)
	})

	t.Run("batches push requests", func(t *testing.T) {
		// mock loki server
		responses := make(chan response, 10)
		mock := httptest.NewServer(createServerHandler(responses))
		require.NotNil(t, mock)
		defer mock.Close()

		client, err := config.NewClientFromConfig(
			config.DefaultHTTPClientConfig,
			"pattern-ingester-push-test",
			config.WithHTTP2Disabled(),
		)
		require.NoError(t, err)
		client.Timeout = 2 * time.Second

		u := url.URL{
			Scheme: "http",
			Host:   mock.Listener.Addr().String(),
			Path:   pushEndpoint,
		}

		p := &Push{
			lokiURL:     u.String(),
			tenantID:    "test1",
			httpClient:  client,
			userAgent:   defaultUserAgent,
			contentType: defaultContentType,
			username:    "user",
			password:    "secret",
			logger:      log.NewNopLogger(),
			quit:        make(chan struct{}),
			backoff:     &backoff,
			entries:     entries{},
			metrics:     NewMetrics(nil),
		}

		lbls1 := labels.New(labels.Label{Name: "test", Value: "test"})
		lbls2 := labels.New(
			labels.Label{Name: "test", Value: "test"},
			labels.Label{Name: "test2", Value: "test2"},
		)

		now := time.Now().Truncate(time.Second).UTC()
		then := now.Add(-1 * time.Minute)
		wayBack := now.Add(-5 * time.Minute)

		p.WriteEntry(
			wayBack,
			AggregatedMetricEntry(model.TimeFromUnix(wayBack.Unix()), 1, 1, lbls1),
			lbls1,
			structuredMetadata,
		)
		p.WriteEntry(
			then,
			AggregatedMetricEntry(model.TimeFromUnix(then.Unix()), 2, 2, lbls1),
			lbls1,
			structuredMetadata,
		)
		p.WriteEntry(
			now,
			AggregatedMetricEntry(model.TimeFromUnix(now.Unix()), 3, 3, lbls1),
			lbls1,
			structuredMetadata,
		)
		p.WriteEntry(
			wayBack,
			AggregatedMetricEntry(model.TimeFromUnix(wayBack.Unix()), 1, 1, lbls2),
			lbls2,
			structuredMetadata,
		)
		p.WriteEntry(
			then,
			AggregatedMetricEntry(model.TimeFromUnix(then.Unix()), 2, 2, lbls2),
			lbls2,
			structuredMetadata,
		)
		p.WriteEntry(
			now,
			AggregatedMetricEntry(model.TimeFromUnix(now.Unix()), 3, 3, lbls2),
			lbls2,
			structuredMetadata,
		)

		p.running.Add(1)
		go p.run(time.Nanosecond)

		select {
		case resp := <-responses:
			p.Stop()
			req := resp.pushReq
			assert.Len(t, req.Streams, 2)

			var stream1, stream2 logproto.Stream
			for _, stream := range req.Streams {
				if stream.Labels == lbls1.String() {
					stream1 = stream
				}

				if stream.Labels == lbls2.String() {
					stream2 = stream
				}
			}

			require.Len(t, stream1.Entries, 3)
			require.Len(t, stream2.Entries, 3)

			require.Equal(t, stream1.Entries[0].Timestamp, wayBack)
			require.Equal(t, stream1.Entries[1].Timestamp, then)
			require.Equal(t, stream1.Entries[2].Timestamp, now)

			require.Equal(
				t,
				AggregatedMetricEntry(model.TimeFromUnix(wayBack.Unix()), 1, 1, lbls1),
				stream1.Entries[0].Line,
			)
			require.Equal(
				t,
				AggregatedMetricEntry(model.TimeFromUnix(then.Unix()), 2, 2, lbls1),
				stream1.Entries[1].Line,
			)
			require.Equal(
				t,
				AggregatedMetricEntry(model.TimeFromUnix(now.Unix()), 3, 3, lbls1),
				stream1.Entries[2].Line,
			)

			require.Equal(t, stream2.Entries[0].Timestamp, wayBack)
			require.Equal(t, stream2.Entries[1].Timestamp, then)
			require.Equal(t, stream2.Entries[2].Timestamp, now)

			require.Equal(
				t,
				AggregatedMetricEntry(model.TimeFromUnix(wayBack.Unix()), 1, 1, lbls2),
				stream2.Entries[0].Line,
			)
			require.Equal(
				t,
				AggregatedMetricEntry(model.TimeFromUnix(then.Unix()), 2, 2, lbls2),
				stream2.Entries[1].Line,
			)
			require.Equal(
				t,
				AggregatedMetricEntry(model.TimeFromUnix(now.Unix()), 3, 3, lbls2),
				stream2.Entries[2].Line,
			)

			// sanity check that bytes are logged in humanized form without whitespaces
			assert.Contains(t, stream1.Entries[0].Line, "bytes=1B")

		case <-time.After(5 * time.Second):
			t.Fatal("timeout")
		}
	})
}

// Test helpers

func assertResponse(t *testing.T, resp response, testAuth bool, ls labels.Labels, ts time.Time, payload string) {
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
	assert.Equal(t, ls.String(), resp.pushReq.Streams[0].Labels)
	assert.Equal(t, labels.StableHash(ls), resp.pushReq.Streams[0].Hash)

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

func labelSet(keyVals ...string) labels.Labels {
	if len(keyVals)%2 != 0 {
		panic("not matching key-value pairs")
	}

	lbls := labels.NewBuilder(labels.EmptyLabels())

	for i := 0; i < len(keyVals)-1; i += 2 {
		lbls.Set(keyVals[i], keyVals[i+1])
	}

	return lbls.Labels()
}

func testPayload() (time.Time, string) {
	ts := time.Now().UTC()
	payload := fmt.Sprintf(LogEntry, fmt.Sprint(ts.UnixNano()), "pppppp")

	return ts, payload
}

func Test_PushRetryStatusCodes(t *testing.T) {
	tests := []struct {
		name string
		// statuses are the status codes the server responds with to consecutive
		// push requests. Using more than one allows a case to fail and then
		// succeed, such as a 429 followed by a 200. Once the statuses run out,
		// the last one is used for all further requests.
		statuses []int
		// expectedRequests is the total number of push requests the server
		// should receive, including any retries.
		expectedRequests int
	}{
		{name: "429 is retried", statuses: []int{http.StatusTooManyRequests, http.StatusOK}, expectedRequests: 2},
		{name: "500 is retried", statuses: []int{http.StatusInternalServerError, http.StatusOK}, expectedRequests: 2},
		{name: "503 is retried", statuses: []int{http.StatusServiceUnavailable, http.StatusOK}, expectedRequests: 2},
		{name: "400 is not retried", statuses: []int{http.StatusBadRequest}, expectedRequests: 1},
		{name: "401 is not retried", statuses: []int{http.StatusUnauthorized}, expectedRequests: 1},
		{name: "403 is not retried", statuses: []int{http.StatusForbidden}, expectedRequests: 1},
		{name: "404 is not retried", statuses: []int{http.StatusNotFound}, expectedRequests: 1},
		{name: "413 is not retried", statuses: []int{http.StatusRequestEntityTooLarge}, expectedRequests: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var (
				mtx      sync.Mutex
				requests int
			)
			server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
				mtx.Lock()
				n := requests
				requests++
				mtx.Unlock()
				rw.WriteHeader(tc.statuses[min(n, len(tc.statuses)-1)])
			}))
			defer server.Close()

			numRequests := func() int {
				mtx.Lock()
				defer mtx.Unlock()
				return requests
			}

			push, err := NewPush(
				server.Listener.Addr().String(),
				"test1",
				2*time.Second,
				50*time.Millisecond,
				config.DefaultHTTPClientConfig,
				"", "",
				false,
				&backoff.Config{MinBackoff: 10 * time.Millisecond, MaxBackoff: 20 * time.Millisecond, MaxRetries: 5},
				log.NewNopLogger(),
				NewMetrics(nil),
			)
			require.NoError(t, err)
			defer push.Stop()

			ts, line := testPayload()
			push.WriteEntry(ts, line, labels.New(labels.Label{Name: "foo", Value: "bar"}), nil)

			require.Eventually(t, func() bool {
				return numRequests() >= tc.expectedRequests
			}, 5*time.Second, 10*time.Millisecond)
			// Give any further retries time to happen.
			require.Never(t, func() bool {
				return numRequests() > tc.expectedRequests
			}, 500*time.Millisecond, 10*time.Millisecond)
		})
	}
}
