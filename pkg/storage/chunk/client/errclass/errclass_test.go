package errclass

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"syscall"
	"testing"

	"github.com/aws/smithy-go"
	"github.com/minio/minio-go/v7"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/googleapi"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/aws"
)

func TestReason(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{
			name: "no error",
			err:  nil,
			want: "",
		},
		{
			name: "context canceled",
			err:  context.Canceled,
			want: Canceled,
		},
		{
			name: "context deadline exceeded",
			err:  context.DeadlineExceeded,
			want: Deadline,
		},
		{
			// Cancellation wins over any provider error it may be reported
			// alongside: we never want to blame storage for our own teardown.
			name: "cancellation alongside a provider error",
			err:  fmt.Errorf("%w: %w", context.Canceled, minio.ErrorResponse{Code: "SlowDown", StatusCode: 503}),
			want: Canceled,
		},
		{
			// Built exactly as object_client.getChunk builds it.
			name: "decode failure",
			err: fmt.Errorf("%w '%s' for tenant `%s`: %w",
				client.ErrChunkDecodeFailed, "fake/key", "1309875", chunk.ErrInvalidChecksum),
			want: Decode,
		},
		{
			name: "chunk checksum mismatch",
			err:  chunk.ErrInvalidChecksum,
			want: Decode,
		},
		{
			name: "chunkenc checksum mismatch",
			err:  chunkenc.ErrInvalidChecksum,
			want: Decode,
		},
		{
			// The dominant genuine failure seen in prod, via the thanos/minio
			// path. aws.IsStorageThrottledErr cannot see this one, which is
			// why we match the minio shape directly.
			name: "minio slowdown",
			err:  minio.ErrorResponse{Code: "SlowDown", StatusCode: 503, Message: "Please reduce your request rate."},
			want: Throttled,
		},
		{
			name: "minio unrecognised code falls back to status",
			err:  minio.ErrorResponse{Code: "SomethingNew", StatusCode: 429},
			want: Throttled,
		},
		{
			name: "minio status only throttle",
			err:  minio.ErrorResponse{Code: "", StatusCode: http.StatusTooManyRequests},
			want: Throttled,
		},
		{
			name: "minio missing object",
			err:  minio.ErrorResponse{Code: "NoSuchKey", StatusCode: 404},
			want: NotFound,
		},
		{
			name: "minio request timeout is not throttling",
			err:  minio.ErrorResponse{Code: "RequestTimeout", StatusCode: 400},
			want: Timeout,
		},
		{
			// A 500 means the store is failing rather than pushing back, and the
			// two call for opposite responses from us.
			name: "minio internal error is not throttling",
			err:  minio.ErrorResponse{Code: "InternalError", StatusCode: 500},
			want: ServerError,
		},
		{
			// ToObjectInfo builds this on a 200 OK whose headers will not parse,
			// so the code did not come off the wire and must not be trusted.
			name: "minio internal error with no status",
			err:  minio.ErrorResponse{Code: "InternalError", StatusCode: 0},
			want: Other,
		},
		{
			// minio synthesises Code from the status line when a proxy returns a
			// body it cannot decode.
			name: "minio proxy bad gateway",
			err:  minio.ErrorResponse{Code: "502 Bad Gateway", StatusCode: http.StatusBadGateway},
			want: ServerError,
		},
		{
			// Real S3 always sends the XML code, so a codeless 503 is a middlebox
			// rather than backpressure.
			name: "minio codeless service unavailable",
			err:  minio.ErrorResponse{Code: "", StatusCode: http.StatusServiceUnavailable},
			want: ServerError,
		},
		{
			// A clock fault is permanent until NTP is repaired, so it must not sit
			// beside transient faults.
			name: "minio clock skew is not a timeout",
			err:  minio.ErrorResponse{Code: "RequestTimeTooSkewed", StatusCode: 403},
			want: Other,
		},
		{
			// 501 is a permanent capability gap on Swift.
			name: "minio not implemented is permanent",
			err:  minio.ErrorResponse{Code: "NotImplemented", StatusCode: 501},
			want: Other,
		},
		{
			name: "aws sdk slowdown",
			err:  &smithy.GenericAPIError{Code: "SlowDown"},
			want: Throttled,
		},
		{
			name: "aws sdk request timeout",
			err:  &smithy.GenericAPIError{Code: "RequestTimeout"},
			want: Timeout,
		},
		{
			// No status travels on the smithy path, so the zero status guard
			// cannot apply here.
			name: "aws sdk internal error",
			err:  &smithy.GenericAPIError{Code: "InternalError"},
			want: ServerError,
		},
		{
			name: "aws sdk not implemented is permanent",
			err:  &smithy.GenericAPIError{Code: "NotImplemented"},
			want: Other,
		},
		{
			name: "gcs too many requests",
			err:  &googleapi.Error{Code: 429},
			want: Throttled,
		},
		{
			name: "gcs missing object",
			err:  &googleapi.Error{Code: 404},
			want: NotFound,
		},
		{
			name: "gcs service unavailable",
			err:  &googleapi.Error{Code: 503},
			want: ServerError,
		},
		{
			// The shape behind the continuous low-rate prod errors:
			// "read tcp ...: read: connection reset by peer".
			name: "connection reset by peer",
			err: &net.OpError{
				Op:  "read",
				Net: "tcp",
				Err: os.NewSyscallError("read", syscall.ECONNRESET),
			},
			want: ConnReset,
		},
		{
			// A truncated body is the one failure nothing in the stack retries.
			name: "truncated body read",
			err:  io.ErrUnexpectedEOF,
			want: ConnReset,
		},
		{
			// A bare EOF is how every successful body read ends, so it must not
			// inflate the bucket named for connections dying mid-stream.
			name: "clean end of stream",
			err:  io.EOF,
			want: Other,
		},
		{
			name: "clean end of stream while reading a body",
			err:  fmt.Errorf("reading body: %w", io.EOF),
			want: Other,
		},
		{
			name: "closed file",
			err:  fmt.Errorf("reading chunk body: %w", os.ErrClosed),
			want: Other,
		},
		{
			name: "server side timeout",
			err:  &net.OpError{Op: "read", Net: "tcp", Err: os.ErrDeadlineExceeded},
			want: Timeout,
		},
		{
			name: "unclassifiable",
			err:  errors.New("boom"),
			want: Other,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, Reason(tc.err))

			if tc.err == nil {
				return
			}
			// Classification must survive the wrapping that the object clients
			// apply on the way out, otherwise every reason silently degrades to
			// "other" in production.
			wrapped := errors.WithStack(errors.Wrapf(tc.err, "failed to load chunk '%s'", "fake/key"))
			require.Equal(t, tc.want, Reason(wrapped), "reason changed once wrapped")
		})
	}
}

func TestReasonsIsComplete(t *testing.T) {
	seen := make(map[string]struct{}, len(Reasons()))
	for _, r := range Reasons() {
		require.NotContains(t, seen, r, "duplicate reason")
		seen[r] = struct{}{}
	}
	require.Len(t, seen, 10)
}

// TestReasonAgreesWithRetryPredicate pins the boundary this package shares with
// the retry predicate in the aws client. The two answer different questions, so
// the codes are shared but the classification is not. The contract is that
// Reason partitions the retryable set without moving its edge.
//
// A row with divergesBecause set inverts the assertion. Those rows will start
// failing when the aws predicate is revised, and that is the intended signal:
// the list is a reviewed changelog of deliberate divergence.
func TestReasonAgreesWithRetryPredicate(t *testing.T) {
	retryable := map[string]struct{}{
		Throttled:   {},
		ServerError: {},
		Timeout:     {},
		ConnReset:   {},
	}

	for _, tc := range []struct {
		name            string
		err             error
		divergesBecause string
	}{
		{
			name: "minio slowdown",
			err:  minio.ErrorResponse{Code: "SlowDown", StatusCode: http.StatusServiceUnavailable},
		},
		{
			name: "aws sdk too many requests",
			err:  &smithy.GenericAPIError{Code: "TooManyRequestsException"},
		},
		{
			name: "minio service unavailable",
			err:  minio.ErrorResponse{Code: "ServiceUnavailable", StatusCode: http.StatusServiceUnavailable},
		},
		{
			name: "minio status only throttle",
			err:  minio.ErrorResponse{Code: "", StatusCode: http.StatusTooManyRequests},
		},
		{
			name: "minio internal error",
			err:  minio.ErrorResponse{Code: "InternalError", StatusCode: http.StatusInternalServerError},
		},
		{
			name: "minio codeless internal server error",
			err:  minio.ErrorResponse{Code: "", StatusCode: http.StatusInternalServerError},
		},
		{
			name: "minio codeless bad gateway",
			err:  minio.ErrorResponse{Code: "", StatusCode: http.StatusBadGateway},
		},
		{
			name: "minio codeless service unavailable",
			err:  minio.ErrorResponse{Code: "", StatusCode: http.StatusServiceUnavailable},
		},
		{
			name: "minio request timeout",
			err:  minio.ErrorResponse{Code: "RequestTimeout", StatusCode: http.StatusBadRequest},
		},
		{
			name: "minio missing object",
			err:  minio.ErrorResponse{Code: "NoSuchKey", StatusCode: http.StatusNotFound},
		},
		{
			name: "minio access denied",
			err:  minio.ErrorResponse{Code: "AccessDenied", StatusCode: http.StatusForbidden},
		},
		{
			name: "minio clock skew",
			err:  minio.ErrorResponse{Code: "RequestTimeTooSkewed", StatusCode: http.StatusForbidden},
		},
		{
			name: "context canceled",
			err:  context.Canceled,
		},
		{
			name: "closed network connection",
			err:  net.ErrClosed,
		},
		{
			name: "connection refused",
			err: &net.OpError{
				Op:  "dial",
				Net: "tcp",
				Err: os.NewSyscallError("connect", syscall.ECONNREFUSED),
			},
		},
		{
			name: "connection reset by peer",
			err: &net.OpError{
				Op:  "read",
				Net: "tcp",
				Err: os.NewSyscallError("read", syscall.ECONNRESET),
			},
		},
		{
			name: "transport deadline exceeded",
			err:  &net.OpError{Op: "read", Net: "tcp", Err: os.ErrDeadlineExceeded},
		},
		{
			name: "chunk checksum mismatch",
			err:  chunk.ErrInvalidChecksum,
		},
		{
			name: "minio internal error with no status",
			err:  minio.ErrorResponse{Code: "InternalError", StatusCode: 0},
			divergesBecause: "ToObjectInfo builds this on a 200 OK with an unparseable header " +
				"(minio-go/utils.go:287), so the retry predicate will retry it forever",
		},
		{
			name: "minio not implemented",
			err:  minio.ErrorResponse{Code: "NotImplemented", StatusCode: http.StatusNotImplemented},
			divergesBecause: "501 is a permanent Swift capability error (s3_storage_client.go:530-539, " +
				"grafana/loki#21791) and is absent from aws-sdk-go-v2/aws/retry/standard.go:53-84",
		},
		{
			name: "clean end of stream",
			err:  io.EOF,
			divergesBecause: "IsStorageTimeoutErr treats a bare EOF as a server close, and it is also " +
				"the normal terminator of buf.ReadFrom in object_client.go",
		},
		{
			name: "truncated body read",
			err:  io.ErrUnexpectedEOF,
			divergesBecause: "a truncated body from object_client.go:195-198 is retried by nothing today, " +
				"and making it visible is the point",
		},
		{
			name: "minio request timeout status",
			err:  minio.ErrorResponse{Code: "", StatusCode: http.StatusRequestTimeout},
			divergesBecause: "the minio branch of the retry predicate covers 429 and 5xx only, while " +
				"minio's own retry list covers 408",
		},
		{
			name: "http client timeout awaiting headers",
			err:  fmt.Errorf("Client.Timeout exceeded while awaiting headers: %w", context.DeadlineExceeded),
			divergesBecause: "IsStorageTimeoutErr string-matches Client.Timeout, and this package will " +
				"not classify on message text",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reason := Reason(tc.err)
			_, treatedAsRetryable := retryable[reason]

			if tc.divergesBecause != "" {
				require.NotEqual(t, aws.IsRetryableErr(tc.err), treatedAsRetryable,
					"expected a known divergence, because %s", tc.divergesBecause)
				return
			}
			require.Equal(t, aws.IsRetryableErr(tc.err), treatedAsRetryable,
				"reason %q sits on the wrong side of the retryable boundary", reason)
		})
	}
}
