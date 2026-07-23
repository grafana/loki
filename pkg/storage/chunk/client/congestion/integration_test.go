package congestion_test

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/storage/bucket"
	"github.com/grafana/loki/v3/pkg/storage/bucket/s3"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/aws"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/congestion"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/hedging"
)

// These tests exercise the full congestion-control read path end to end:
//
//	AIMD controller -> LimitedRetrier -> thanos-objstore ObjectClientAdapter -> minio-go -> (fake) S3
//
// against a fake S3 endpoint that returns "503 SlowDown". They verify that S3
// throttling returned by the minio-backed thanos client is recognised as
// retryable, so the controller retries (and its inner AIMD back-off engages)
// rather than surfacing the throttle immediately as a failed chunk download.
//
// Regression coverage: previously the retryability check only understood the
// AWS SDK's smithy.APIError, so minio.ErrorResponse throttles were treated as
// non-retryable — no retries, no back-off, and every throttle became a failure.
//
// This lives in the external test package (congestion_test) on purpose: it
// imports the higher-level bucket package to build the real thanos/minio
// client, and an external test package avoids any import-cycle coupling to the
// core congestion package. Assertions are black-box (observed request count and
// returned result); the AIMD counter internals are covered by controller_test.go.

const testChunkKey = "fake/chunk/key"

// writeS3Error writes an S3-style XML error body that minio-go parses into a
// minio.ErrorResponse with the given Code and StatusCode.
func writeS3Error(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w,
		`<?xml version="1.0" encoding="UTF-8"?><Error><Code>%s</Code><Message>%s</Message><Resource>/test-bucket/%s</Resource><RequestId>test</RequestId></Error>`,
		code, message, testChunkKey)
}

func isChunkGet(r *http.Request) bool {
	return r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, testChunkKey)
}

// writeS3Object writes a well-formed S3 GetObject 200 response. minio-go parses
// the response headers into ObjectInfo and errors if required headers (ETag,
// Last-Modified) are missing, so we set them like a real S3 server would.
func writeS3Object(w http.ResponseWriter, body []byte) {
	sum := md5.Sum(body)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.Header().Set("ETag", fmt.Sprintf("%q", hex.EncodeToString(sum[:])))
	w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
	w.Header().Set("Accept-Ranges", "bytes")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// newCongestionControlledS3 builds the real congestion-controlled S3 read path
// pointed at the given fake-S3 handler.
func newCongestionControlledS3(t *testing.T, handler http.HandlerFunc) congestion.Controller {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	// disableRetries=true mirrors how the storage factory wires the client when
	// congestion control is enabled: the inner (minio) client must not retry, so
	// the AIMD controller is solely in charge of retries/back-off. This also keeps
	// the observed request count == the controller's attempt count.
	adapter, err := bucket.NewObjectClient(context.Background(), bucket.S3, bucket.ConfigWithNamedStores{
		Config: bucket.Config{
			S3: s3.Config{
				Endpoint:        srv.Listener.Addr().String(),
				Region:          "test",
				BucketName:      "test-bucket",
				AccessKeyID:     "test",
				SecretAccessKey: flagext.SecretWithValue("test"),
				Insecure:        true,
			},
		},
	}, "test", hedging.Config{}, true, log.NewNopLogger())
	require.NoError(t, err)

	cfg := congestion.Config{
		Enabled: true,
		Controller: congestion.ControllerConfig{
			Strategy: "aimd",
			// Start high enough that the rate limiter never blocks for the handful
			// of requests in these tests; we're exercising retry + back-off, not the
			// throughput limiter.
			AIMD: congestion.AIMD{Start: 2000, UpperBound: 10000, BackoffFactor: 0.2},
		},
		Retry: congestion.RetrierConfig{Strategy: "limited", Limit: 2},
	}

	metrics := congestion.NewMetrics(t.Name(), cfg)
	t.Cleanup(metrics.Unregister)

	ctrl := congestion.NewController(cfg, log.NewNopLogger(), metrics)
	ctrl.Wrap(adapter)

	return ctrl
}

// TestCongestionControl_S3Throttling_RetriesThenExceeds verifies that when S3
// throttles every request, the controller retries up to the configured limit
// and finally returns RetriesExceeded. Reaching RetriesExceeded (rather than the
// raw minio error on the first attempt) is only possible if the SlowDown is
// classified retryable; the observed request count proves the retries hit S3.
func TestCongestionControl_S3Throttling_RetriesThenExceeds(t *testing.T) {
	var serverGets atomic.Int64
	ctrl := newCongestionControlledS3(t, func(w http.ResponseWriter, r *http.Request) {
		if isChunkGet(r) {
			serverGets.Inc()
			writeS3Error(w, http.StatusServiceUnavailable, aws.ErrCodeSlowDown, "Please reduce your request rate.")
			return
		}
		// Any other minio bookkeeping request (bucket location, etc.).
		w.WriteHeader(http.StatusOK)
	})

	_, _, err := ctrl.GetObject(context.Background(), testChunkKey)
	require.Error(t, err)
	require.ErrorIs(t, err, congestion.RetriesExceeded)

	// limit=2 => 1 initial attempt + 2 retries = 3 attempts, each a GET to S3.
	require.EqualValues(t, 3, serverGets.Load(), "expected 1 initial request + 2 retries to reach S3")
}

// TestCongestionControl_S3Throttling_RecoversAfterRetry verifies that a
// transient throttle is absorbed: after two SlowDown responses the third attempt
// succeeds and GetObject returns the object body with no error.
func TestCongestionControl_S3Throttling_RecoversAfterRetry(t *testing.T) {
	const body = "hello-chunk-bytes"
	var serverGets atomic.Int64
	ctrl := newCongestionControlledS3(t, func(w http.ResponseWriter, r *http.Request) {
		if isChunkGet(r) {
			if serverGets.Inc() <= 2 {
				writeS3Error(w, http.StatusServiceUnavailable, aws.ErrCodeSlowDown, "Please reduce your request rate.")
				return
			}
			writeS3Object(w, []byte(body))
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	rc, _, err := ctrl.GetObject(context.Background(), testChunkKey)
	require.NoError(t, err, "request should succeed after retrying past the throttles")
	t.Cleanup(func() { _ = rc.Close() })

	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.Equal(t, body, string(got))

	// 2 throttled attempts + 1 successful attempt.
	require.EqualValues(t, 3, serverGets.Load())
}
