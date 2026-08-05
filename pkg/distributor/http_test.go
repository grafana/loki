package distributor

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/grafana/dskit/user"

	"github.com/grafana/loki/v3/pkg/util/constants"

	"github.com/grafana/loki/v3/pkg/loghttp/push"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/runtime"

	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"

	"github.com/grafana/loki/v3/pkg/validation"
)

func TestDistributorRingHandler(t *testing.T) {
	limits := &validation.Limits{}
	flagext.DefaultValues(limits)

	runServer := func() *httptest.Server {
		distributors, _ := prepare(t, 1, 3, limits, nil)

		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			distributors[0].ServeHTTP(w, r)
		}))
	}

	t.Run("renders ring status for global rate limiting", func(t *testing.T) {
		limits.IngestionRateStrategy = validation.GlobalIngestionRateStrategy
		svr := runServer()
		defer svr.Close()

		resp, err := svr.Client().Get(svr.URL)
		require.NoError(t, err)

		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Contains(t, string(body), "<th>Instance ID</th>")
		require.NotContains(t, string(body), "Not running with Global Rating Limit - ring not being used by the Distributor")
	})

	t.Run("doesn't return ring status for local rate limiting", func(t *testing.T) {
		limits.IngestionRateStrategy = validation.LocalIngestionRateStrategy
		svr := runServer()
		defer svr.Close()

		resp, err := svr.Client().Get(svr.URL)
		require.NoError(t, err)

		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Contains(t, string(body), "Not running with Global Rating Limit - ring not being used by the Distributor")
		require.NotContains(t, string(body), "<th>Instance ID</th>")
	})
}

func TestRequestParserWrapping(t *testing.T) {
	t.Run("it calls the parser wrapper if there is one", func(t *testing.T) {
		limits := &validation.Limits{}
		flagext.DefaultValues(limits)
		limits.RejectOldSamples = false
		distributors, _ := prepare(t, 1, 3, limits, nil)

		var called bool
		distributors[0].RequestParserWrapper = func(requestParser push.RequestParser) push.RequestParser {
			called = true
			return requestParser
		}

		ctx := user.InjectOrgID(context.Background(), "test-user")
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "fake-path", nil)
		require.NoError(t, err)

		rec := httptest.NewRecorder()
		distributors[0].pushHandler(rec, req, newFakeParser().parseRequest, push.HTTPError, constants.Loki)

		// unprocessable code because there are no streams in the request.
		require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
		require.True(t, called)
	})

	t.Run("it returns 204 when the parser wrapper filteres all log lines", func(t *testing.T) {
		limits := &validation.Limits{}
		flagext.DefaultValues(limits)
		limits.RejectOldSamples = false
		distributors, _ := prepare(t, 1, 3, limits, nil)

		var called bool
		distributors[0].RequestParserWrapper = func(requestParser push.RequestParser) push.RequestParser {
			called = true
			return requestParser
		}

		ctx := user.InjectOrgID(context.Background(), "test-user")
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "fake-path", nil)
		require.NoError(t, err)

		parser := newFakeParser()
		parser.parseErr = push.ErrAllLogsFiltered

		rec := httptest.NewRecorder()
		distributors[0].pushHandler(rec, req, parser.parseRequest, push.HTTPError, constants.Loki)

		require.True(t, called)
		require.Equal(t, http.StatusNoContent, rec.Code)
	})

	t.Run("it handles request body too large error with positive content length", func(t *testing.T) {
		limits := &validation.Limits{}
		flagext.DefaultValues(limits)
		limits.RejectOldSamples = false
		distributors, _ := prepare(t, 1, 3, limits, nil)

		ctx := user.InjectOrgID(context.Background(), "test-user")
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "fake-path", nil)
		require.NoError(t, err)

		// Set a positive content length
		req.ContentLength = 1000

		parser := newFakeParser()
		parser.parseErr = push.ErrRequestBodyTooLarge

		rec := httptest.NewRecorder()
		distributors[0].pushHandler(rec, req, parser.parseRequest, push.HTTPError, constants.Loki)

		require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	})

	t.Run("it handles request body too large error with negative content length", func(t *testing.T) {
		limits := &validation.Limits{}
		flagext.DefaultValues(limits)
		limits.RejectOldSamples = false
		distributors, _ := prepare(t, 1, 3, limits, nil)

		ctx := user.InjectOrgID(context.Background(), "test-user")
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "fake-path", nil)
		require.NoError(t, err)

		// Set a negative content length to test our guard clause
		req.ContentLength = -1

		parser := newFakeParser()
		parser.parseErr = push.ErrRequestBodyTooLarge

		rec := httptest.NewRecorder()
		distributors[0].pushHandler(rec, req, parser.parseRequest, push.HTTPError, constants.Loki)

		require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
		// The test should complete without panicking
	})
}

func TestPushHandlerMaxRecvMsgSize(t *testing.T) {
	const line = "the quick brown fox jumps over the lazy dog"

	limits := &validation.Limits{}
	flagext.DefaultValues(limits)
	limits.RejectOldSamples = false
	distributors, _ := prepare(t, 1, 3, limits, nil)
	distributors[0].cfg.MaxRecvMsgSize = 10

	t.Run("protobuf returns 413", func(t *testing.T) {
		body, err := proto.Marshal(&logproto.PushRequest{
			Streams: []logproto.Stream{
				{
					Labels:  `{foo="bar"}`,
					Entries: []logproto.Entry{{Timestamp: time.Now(), Line: line}},
				},
			},
		})
		require.NoError(t, err)
		require.Greater(t, len(body), distributors[0].cfg.MaxRecvMsgSize)

		req := httptest.NewRequest(http.MethodPost, "/loki/api/v1/push", bytes.NewReader(body))
		ctx := user.InjectOrgID(t.Context(), "test")
		req = req.WithContext(ctx)
		req.Header.Set("Content-Type", "application/x-protobuf")

		// The metric is a global counter shared across tests, so measure the
		// delta produced by this request rather than an absolute value.
		discardedBytes := validation.DiscardedBytes.WithLabelValues(validation.RequestBodyTooLarge, "test", "", "", constants.Loki)
		before := testutil.ToFloat64(discardedBytes)

		rec := httptest.NewRecorder()
		distributors[0].pushHandler(rec, req, push.ParseLokiRequest, push.HTTPError, constants.Loki)

		require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
		require.Equal(t, float64(req.ContentLength), testutil.ToFloat64(discardedBytes)-before)
	})

	t.Run("snappy compressed protobuf returns 413", func(t *testing.T) {
		protoBytes, err := proto.Marshal(&logproto.PushRequest{
			Streams: []logproto.Stream{
				{
					Labels:  `{foo="bar"}`,
					Entries: []logproto.Entry{{Timestamp: time.Now(), Line: line}},
				},
			},
		})
		require.NoError(t, err)
		body := snappy.Encode(nil, protoBytes)
		require.Greater(t, len(body), distributors[0].cfg.MaxRecvMsgSize)

		req := httptest.NewRequest(http.MethodPost, "/loki/api/v1/push", bytes.NewReader(body))
		ctx := user.InjectOrgID(t.Context(), "test")
		req = req.WithContext(ctx)
		req.Header.Set("Content-Type", "application/x-protobuf")

		// The metric is a global counter shared across tests, so measure the
		// delta produced by this request rather than an absolute value.
		discardedBytes := validation.DiscardedBytes.WithLabelValues(validation.RequestBodyTooLarge, "test", "", "", constants.Loki)
		before := testutil.ToFloat64(discardedBytes)

		rec := httptest.NewRecorder()
		distributors[0].pushHandler(rec, req, push.ParseLokiRequest, push.HTTPError, constants.Loki)

		require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
		require.Equal(t, float64(req.ContentLength), testutil.ToFloat64(discardedBytes)-before)
	})

	t.Run("Loki JSON returns 413", func(t *testing.T) {
		t.Skip() // Returns HTTP 400

		// NOTE: this currently returns 400, not 413: an oversized JSON body is
		// truncated by the max-recv-msg-size LimitReader and fails to decode
		// before any size check maps to ErrRequestBodyTooLarge. We assert 413
		// here as the desired behavior.
		body := []byte(`{"streams":[{"stream":{"foo":"bar"},"values":[["1234567890000000000","` + line + `"]]}]}`)
		require.Greater(t, len(body), distributors[0].cfg.MaxRecvMsgSize)

		req := httptest.NewRequest(http.MethodPost, "/loki/api/v1/push", bytes.NewReader(body))
		ctx := user.InjectOrgID(t.Context(), "test")
		req = req.WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")

		// The metric is a global counter shared across tests, so measure the
		// delta produced by this request rather than an absolute value.
		discardedBytes := validation.DiscardedBytes.WithLabelValues(validation.RequestBodyTooLarge, "test", "", "", constants.Loki)
		before := testutil.ToFloat64(discardedBytes)

		rec := httptest.NewRecorder()
		distributors[0].pushHandler(rec, req, push.ParseLokiRequest, push.HTTPError, constants.Loki)

		require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
		require.Equal(t, float64(req.ContentLength), testutil.ToFloat64(discardedBytes)-before)
	})

	t.Run("OTLP JSON returns 413", func(t *testing.T) {
		otlpLogs := plog.NewLogs()
		rl := otlpLogs.ResourceLogs().AppendEmpty()
		rl.Resource().Attributes().PutStr("service.name", "test-service")
		lr := rl.ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
		lr.Body().SetStr(line)
		lr.SetTimestamp(pcommon.Timestamp(time.Now().UnixNano()))
		body, err := plogotlp.NewExportRequestFromLogs(otlpLogs).MarshalJSON()
		require.NoError(t, err)
		require.Greater(t, len(body), distributors[0].cfg.MaxRecvMsgSize)

		req := httptest.NewRequest(http.MethodPost, "/otlp/v1/logs", bytes.NewReader(body))
		ctx := user.InjectOrgID(t.Context(), "test")
		req = req.WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")

		// The metric is a global counter shared across tests, so measure the
		// delta produced by this request rather than an absolute value.
		discardedBytes := validation.DiscardedBytes.WithLabelValues(validation.RequestBodyTooLarge, "test", "", "", constants.OTLP)
		before := testutil.ToFloat64(discardedBytes)

		rec := httptest.NewRecorder()
		distributors[0].pushHandler(rec, req, push.ParseOTLPRequest, push.OTLPError, constants.OTLP)

		require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
		require.Equal(t, float64(req.ContentLength), testutil.ToFloat64(discardedBytes)-before)
	})
}

func TestPushHandlerMaxDecompressedSize(t *testing.T) {
	const line = "the quick brown fox jumps over the lazy dog"

	limits := &validation.Limits{}
	flagext.DefaultValues(limits)
	limits.RejectOldSamples = false
	distributors, _ := prepare(t, 1, 3, limits, nil)
	distributors[0].cfg.MaxDecompressedSize = 10

	withGzip := func(t *testing.T, b []byte) []byte {
		t.Helper()
		buf := bytes.Buffer{}
		w := gzip.NewWriter(&buf)
		_, err := w.Write(b)
		require.NoError(t, err)
		require.NoError(t, w.Close())
		return buf.Bytes()
	}

	t.Run("snappy compressed protobuf returns 413", func(t *testing.T) {
		t.Skip() // Returns HTTP 400
		protoBytes, err := proto.Marshal(&logproto.PushRequest{
			Streams: []logproto.Stream{
				{
					Labels:  `{foo="bar"}`,
					Entries: []logproto.Entry{{Timestamp: time.Now(), Line: line}},
				},
			},
		})
		require.NoError(t, err)
		body := snappy.Encode(nil, protoBytes)
		require.Greater(t, int64(len(protoBytes)), distributors[0].cfg.MaxDecompressedSize)

		req := httptest.NewRequest(http.MethodPost, "/loki/api/v1/push", bytes.NewReader(body))
		ctx := user.InjectOrgID(t.Context(), "test")
		req = req.WithContext(ctx)
		req.Header.Set("Content-Type", "application/x-protobuf")
		req.Header.Set("Content-Encoding", "snappy")

		// The metric is a global counter shared across tests, so measure the
		// delta produced by this request rather than an absolute value.
		discardedBytes := validation.DiscardedBytes.WithLabelValues(validation.RequestBodyTooLarge, "test", "", "", constants.Loki)
		before := testutil.ToFloat64(discardedBytes)

		rec := httptest.NewRecorder()
		distributors[0].pushHandler(rec, req, push.ParseLokiRequest, push.HTTPError, constants.Loki)

		require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
		require.Equal(t, float64(req.ContentLength), testutil.ToFloat64(discardedBytes)-before)
	})

	t.Run("gzip compressed Loki JSON returns 413", func(t *testing.T) {
		t.Skip() // Returns HTTP 400
		lokiJSON := []byte(`{"streams":[{"stream":{"foo":"bar"},"values":[["1234567890000000000","` + line + `"]]}]}`)
		body := withGzip(t, lokiJSON)
		require.Greater(t, int64(len(lokiJSON)), distributors[0].cfg.MaxDecompressedSize)

		req := httptest.NewRequest(http.MethodPost, "/loki/api/v1/push", bytes.NewReader(body))
		ctx := user.InjectOrgID(t.Context(), "test")
		req = req.WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Content-Encoding", "gzip")

		// The metric is a global counter shared across tests, so measure the
		// delta produced by this request rather than an absolute value.
		discardedBytes := validation.DiscardedBytes.WithLabelValues(validation.RequestBodyTooLarge, "test", "", "", constants.Loki)
		before := testutil.ToFloat64(discardedBytes)

		rec := httptest.NewRecorder()
		distributors[0].pushHandler(rec, req, push.ParseLokiRequest, push.HTTPError, constants.Loki)

		require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
		require.Equal(t, float64(req.ContentLength), testutil.ToFloat64(discardedBytes)-before)
	})

	t.Run("gzip compressed OTLP JSON returns 413", func(t *testing.T) {
		t.Skip() // Returns HTTP 400
		otlpLogs := plog.NewLogs()
		rl := otlpLogs.ResourceLogs().AppendEmpty()
		rl.Resource().Attributes().PutStr("service.name", "test-service")
		lr := rl.ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
		lr.Body().SetStr(line)
		lr.SetTimestamp(pcommon.Timestamp(time.Now().UnixNano()))
		otlpJSON, err := plogotlp.NewExportRequestFromLogs(otlpLogs).MarshalJSON()
		require.NoError(t, err)
		body := withGzip(t, otlpJSON)
		require.Greater(t, int64(len(otlpJSON)), distributors[0].cfg.MaxDecompressedSize)

		req := httptest.NewRequest(http.MethodPost, "/otlp/v1/logs", bytes.NewReader(body))
		ctx := user.InjectOrgID(t.Context(), "test")
		req = req.WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Content-Encoding", "gzip")

		// The metric is a global counter shared across tests, so measure the
		// delta produced by this request rather than an absolute value.
		discardedBytes := validation.DiscardedBytes.WithLabelValues(validation.RequestBodyTooLarge, "test", "", "", constants.OTLP)
		before := testutil.ToFloat64(discardedBytes)

		rec := httptest.NewRecorder()
		distributors[0].pushHandler(rec, req, push.ParseOTLPRequest, push.OTLPError, constants.OTLP)

		require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
		require.Equal(t, float64(req.ContentLength), testutil.ToFloat64(discardedBytes)-before)
	})
}

func TestPushHandlerCircuitBreaker(t *testing.T) {
	isAnyErr := func(err error) bool { return err != nil }

	// Returns a distributor and a request for the tenant "test-user".
	setup := func(t *testing.T) (*Distributor, *http.Request) {
		limits := &validation.Limits{}
		flagext.DefaultValues(limits)
		distributors, _ := prepare(t, 1, 3, limits, nil)

		ctx := user.InjectOrgID(context.Background(), "test-user")
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "fake-path", nil)
		require.NoError(t, err)
		return distributors[0], req
	}

	// Opens a circuit breaker by failing a request.
	open := func(t *testing.T, b *trialCircuitBreaker) {
		ok, doneFunc := b.Allow()
		require.True(t, ok)
		doneFunc(errors.New("some error occurred"))
		require.Equal(t, circuitBreakerOpen, b.state)
	}

	t.Run("returns 429 when the tenant circuit breaker is open", func(t *testing.T) {
		d, req := setup(t)
		d.tenantCircuitBreaker = newTenantCircuitBreaker(func() *trialCircuitBreaker {
			return newTrialCircuitBreaker(time.Second, 1, 1, isAnyErr)
		})
		open(t, d.tenantCircuitBreaker.state("test-user").circuitBreaker)

		rec := httptest.NewRecorder()
		d.pushHandler(rec, req, newFakeParser().parseRequest, push.HTTPError, constants.Loki)

		require.Equal(t, http.StatusTooManyRequests, rec.Code)
		require.Contains(t, rec.Body.String(), "tenant circuit breaker open, request denied")
	})

	t.Run("returns 503 when the global circuit breaker is open", func(t *testing.T) {
		d, req := setup(t)
		circuitBreaker := newTrialCircuitBreaker(time.Second, 1, 1, isAnyErr)
		open(t, circuitBreaker)
		d.circuitBreaker = circuitBreaker

		rec := httptest.NewRecorder()
		d.pushHandler(rec, req, newFakeParser().parseRequest, push.HTTPError, constants.Loki)

		require.Equal(t, http.StatusServiceUnavailable, rec.Code)
		require.Contains(t, rec.Body.String(), "circuit breaker open, request denied")
	})

	t.Run("the tenant circuit breaker takes precedence", func(t *testing.T) {
		d, req := setup(t)
		d.tenantCircuitBreaker = newTenantCircuitBreaker(func() *trialCircuitBreaker {
			return newTrialCircuitBreaker(time.Second, 1, 1, isAnyErr)
		})
		open(t, d.tenantCircuitBreaker.state("test-user").circuitBreaker)

		globalCircuitBreaker := newTrialCircuitBreaker(time.Second, 1, 1, isAnyErr)
		d.circuitBreaker = globalCircuitBreaker
		// Put the global circuit breaker in the half-open state, where a spurious
		// success would close it early.
		open(t, globalCircuitBreaker)
		globalCircuitBreaker.halfOpen()
		stateBefore, _, _ := globalCircuitBreaker.snapshot()

		rec := httptest.NewRecorder()
		d.pushHandler(rec, req, newFakeParser().parseRequest, push.HTTPError, constants.Loki)

		require.Equal(t, http.StatusTooManyRequests, rec.Code)
		// The request must be denied before the global circuit breaker is asked, so
		// it must neither consume a trial request nor record a result.
		stateAfter, _, _ := globalCircuitBreaker.snapshot()
		require.Equal(t, stateBefore, stateAfter)
		require.Zero(t, globalCircuitBreaker.trials)
		require.Zero(t, globalCircuitBreaker.successes)
	})
}

type fakeParser struct {
	parseErr error
}

func newFakeParser() *fakeParser {
	return &fakeParser{}
}

func (p *fakeParser) parseRequest(
	_ string,
	_ *http.Request,
	_ push.Limits,
	_ *runtime.TenantConfigs,
	_ int,
	_ int64,
	_ push.UsageTracker,
	_ push.StreamResolver,
	_ log.Logger,
) (*logproto.PushRequest, *push.Stats, error) {
	return &logproto.PushRequest{}, &push.Stats{}, p.parseErr
}
