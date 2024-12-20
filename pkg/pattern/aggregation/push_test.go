package aggregation

import (
	"encoding/base64"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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
)

const (
	testTenant   = "test1"
	testUsername = "user"
	testPassword = "secret"
	LogEntry     = "%s %s\n"
)

func Test_Push(t *testing.T) {
	lbls := labels.New(labels.Label{Name: "test", Value: "test"})

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
		push.WriteEntry(ts, payload, lbls)
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
		push.WriteEntry(ts, payload, lbls)
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
			AggregatedMetricEntry(model.TimeFromUnix(wayBack.Unix()), 1, 1, "test_service", lbls1),
			lbls1,
		)
		p.WriteEntry(
			then,
			AggregatedMetricEntry(model.TimeFromUnix(then.Unix()), 2, 2, "test_service", lbls1),
			lbls1,
		)
		p.WriteEntry(
			now,
			AggregatedMetricEntry(model.TimeFromUnix(now.Unix()), 3, 3, "test_service", lbls1),
			lbls1,
		)

		p.WriteEntry(
			wayBack,
			AggregatedMetricEntry(model.TimeFromUnix(wayBack.Unix()), 1, 1, "test2_service", lbls2),
			lbls2,
		)
		p.WriteEntry(
			then,
			AggregatedMetricEntry(model.TimeFromUnix(then.Unix()), 2, 2, "test2_service", lbls2),
			lbls2,
		)
		p.WriteEntry(
			now,
			AggregatedMetricEntry(model.TimeFromUnix(now.Unix()), 3, 3, "test2_service", lbls2),
			lbls2,
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
				AggregatedMetricEntry(model.TimeFromUnix(wayBack.Unix()), 1, 1, "test_service", lbls1),
				stream1.Entries[0].Line,
			)
			require.Equal(
				t,
				AggregatedMetricEntry(model.TimeFromUnix(then.Unix()), 2, 2, "test_service", lbls1),
				stream1.Entries[1].Line,
			)
			require.Equal(
				t,
				AggregatedMetricEntry(model.TimeFromUnix(now.Unix()), 3, 3, "test_service", lbls1),
				stream1.Entries[2].Line,
			)

			require.Equal(t, stream2.Entries[0].Timestamp, wayBack)
			require.Equal(t, stream2.Entries[1].Timestamp, then)
			require.Equal(t, stream2.Entries[2].Timestamp, now)

			require.Equal(
				t,
				AggregatedMetricEntry(model.TimeFromUnix(wayBack.Unix()), 1, 1, "test2_service", lbls2),
				stream2.Entries[0].Line,
			)
			require.Equal(
				t,
				AggregatedMetricEntry(model.TimeFromUnix(then.Unix()), 2, 2, "test2_service", lbls2),
				stream2.Entries[1].Line,
			)
			require.Equal(
				t,
				AggregatedMetricEntry(model.TimeFromUnix(now.Unix()), 3, 3, "test2_service", lbls2),
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

func assertResponse(t *testing.T, resp response, testAuth bool, labels labels.Labels, ts time.Time, payload string) {
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
	assert.Equal(t, labels.Hash(), resp.pushReq.Streams[0].Hash)

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

	lbls := labels.Labels{}

	for i := 0; i < len(keyVals)-1; i += 2 {
		lbls = append(lbls, labels.Label{Name: keyVals[i], Value: keyVals[i+1]})
	}

	return lbls
}

func testPayload() (time.Time, string) {
	ts := time.Now().UTC()
	payload := fmt.Sprintf(LogEntry, fmt.Sprint(ts.UnixNano()), "pppppp")

	return ts, payload
}
