// Package errclass classifies chunk fetch errors into a small, bounded set of
// reasons. The values are used as Prometheus label values, so the set must stay
// small and stable.
//
// The classification lives here rather than in the object store clients. Those
// clients import pkg/storage/chunk/client, so provider-aware classification
// inside them would close an import cycle.
//
// retry/standard.go in aws-sdk-go-v2 is the upstream authority for the retryable
// code and status sets, and is worth reading before changing either mapping.
package errclass

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"syscall"

	"github.com/aws/smithy-go"
	"github.com/minio/minio-go/v7"
	"google.golang.org/api/googleapi"
	amnet "k8s.io/apimachinery/pkg/util/net"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/aws"
)

// Reason values. These are used as Prometheus label values, so the set must
// stay small and stable.
const (
	// Canceled and Deadline are client-side and almost always benign: a query
	// finished early, or the caller's context expired.
	Canceled = "canceled"
	Deadline = "deadline"
	// Decode means the bytes were retrieved but could not be turned back into a
	// chunk. Either corruption or a version mismatch.
	Decode = "decode"
	// Throttled means the object store asked us to slow down. Only an explicit
	// wire code sets it, because this is the value a rollout decision reads.
	Throttled = "throttled"
	// ServerError is a 5xx that is not an explicit throttle. The store is failing
	// rather than pushing back, and the two call for opposite responses from us.
	ServerError = "server_error"
	// NotFound means the object is absent, e.g. deleted by retention while the
	// index still references it.
	NotFound = "not_found"
	// Timeout is a server-side timeout, as distinct from Deadline.
	Timeout = "timeout"
	// ConnReset covers connections closed mid-stream. Notably this is what a
	// failed body read looks like, which is currently never retried.
	ConnReset = "conn_reset"
	// Other is a real error we could not classify.
	Other = "other"
	// Unknown is used when a chunk was lost but no error was observed, which
	// indicates a bug in our own accounting rather than a storage problem.
	Unknown = "unknown"
)

// Reasons returns every reason value. Callers use this to pre-initialise label
// combinations so that dashboards and alerts do not have to handle missing
// series.
func Reasons() []string {
	return []string{Canceled, Deadline, Decode, Throttled, ServerError, NotFound, Timeout, ConnReset, Other, Unknown}
}

// Reason classifies err into one of the reason constants.
//
// Every check uses errors.Is or errors.As so that classification survives the
// errors.Wrapf and errors.WithStack wrapping applied by the object clients.
// Order matters: the more specific checks come first, because a single error can
// satisfy several of them.
func Reason(err error) string {
	// A nil error is not classifiable. Callers that count a loss must map nil to
	// their own Unknown, so a mistaken nil can never look like a real reason.
	if err == nil {
		return ""
	}

	// Context errors first: a canceled request can surface as any of the
	// provider error shapes below, and we never want to blame storage for it.
	if errors.Is(err, context.Canceled) {
		return Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return Deadline
	}

	if errors.Is(err, client.ErrChunkDecodeFailed) ||
		errors.Is(err, chunk.ErrInvalidChecksum) ||
		errors.Is(err, chunkenc.ErrInvalidChecksum) {
		return Decode
	}

	if reason, ok := reasonFromProvider(err); ok {
		return reason
	}

	// Connection reset must be checked before the generic timeout check: a
	// stream that dies mid-read is a distinct, and currently unretried, failure
	// mode that we specifically want to be able to count. A bare io.EOF is
	// deliberately absent, since it is how every successful body read ends.
	if amnet.IsConnectionReset(err) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, io.ErrUnexpectedEOF) {
		return ConnReset
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return Timeout
	}

	return Other
}

// reasonFromProvider maps provider specific error shapes onto reasons. It
// returns false if the error is not a recognised provider error, or is one but
// carries a code we have no reason for.
func reasonFromProvider(err error) (string, bool) {
	// S3 via the thanos objstore path, which is the default
	// (-store.use-thanos-objstore defaults to true). This is minio-go, whose
	// ErrorResponse does NOT implement smithy.APIError, so the smithy branch
	// below cannot see it. Missing this is why aws.IsRetryableErr currently
	// fails to recognise SlowDown on the live read path.
	var minioErr minio.ErrorResponse
	if errors.As(err, &minioErr) {
		// A minio error with no status never came off the wire. ToObjectInfo builds
		// one on a 200 OK with an unparseable header, so its code must not be trusted.
		if minioErr.StatusCode == 0 {
			return Other, true
		}
		if reason, ok := reasonFromCode(minioErr.Code); ok {
			return reason, true
		}
		if reason, ok := reasonFromStatus(minioErr.StatusCode); ok {
			return reason, true
		}
	}

	// S3 via the legacy aws-sdk-go-v2 path.
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		if reason, ok := reasonFromCode(apiErr.ErrorCode()); ok {
			return reason, true
		}
	}

	// GCS. Note we deliberately use errors.As rather than the bare type
	// assertion in gcp.IsStorageThrottledErr, which cannot see through any
	// wrapping.
	var gcsErr *googleapi.Error
	if errors.As(err, &gcsErr) {
		if reason, ok := reasonFromStatus(gcsErr.Code); ok {
			return reason, true
		}
	}

	// TODO: Azure and Swift throttling codes are not recognised and fall
	// through to Other. That only matters for GEL/on-prem deployments.
	return "", false
}

// reasonFromCode maps the string error codes shared by S3 and S3 compatible
// stores. The codes come from the aws client so that a rename cannot silently
// split the two lists. The mapping stays finer here than the retry predicate,
// which can merge what this must keep apart.
func reasonFromCode(code string) (string, bool) {
	switch code {
	case aws.ErrCodeSlowDown, aws.ErrCodeTooManyRequests,
		aws.ErrCodeTooManyRequestsException, aws.ErrCodeServiceUnavailable:
		return Throttled, true
	case aws.ErrCodeInternalError:
		return ServerError, true
	case aws.ErrCodeNotImplemented:
		// A permanent capability gap on Swift. It must be claimed here so the 5xx
		// arm below cannot report it as a transient server fault.
		return Other, true
	case "NoSuchKey", "NoSuchBucket", "NotFound":
		return NotFound, true
	case aws.ErrCodeRequestTimeout:
		return Timeout, true
	}
	return "", false
}

// reasonFromStatus maps an HTTP status onto a reason. The open-ended 5xx arm is
// what catches a proxy failure, whose body minio cannot decode into a code.
func reasonFromStatus(status int) (string, bool) {
	switch status {
	case http.StatusTooManyRequests:
		return Throttled, true
	case http.StatusNotFound:
		return NotFound, true
	case http.StatusRequestTimeout, http.StatusGatewayTimeout:
		return Timeout, true
	}
	if status >= http.StatusInternalServerError {
		return ServerError, true
	}
	return "", false
}
