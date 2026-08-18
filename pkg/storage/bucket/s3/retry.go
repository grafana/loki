package s3

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/minio/minio-go/v7"
	amnet "k8s.io/apimachinery/pkg/util/net"
)

const (
	errCodeRequestTimeout           = "RequestTimeout"
	errCodeTooManyRequests          = "TooManyRequests"
	errCodeTooManyRequestsException = "TooManyRequestsException"
	errCodeInternalError            = "InternalError"
	errCodeNotImplemented           = "NotImplemented"
	errCodeServiceUnavailable       = "ServiceUnavailable"
	errCodeSlowDown                 = "SlowDown"
)

// IsRetryableErr reports whether err is a transient error returned by the
// MinIO-backed Thanos S3 client.
func IsRetryableErr(err error) bool {
	return isStorageTimeoutErr(err) || isStorageThrottledErr(err)
}

func isStorageTimeoutErr(err error) bool {
	if isContextErr(err) {
		// Go 1.23 changed the type of the error returned by the HTTP client when
		// a timeout occurs while waiting for response headers.
		return strings.Contains(err.Error(), "Client.Timeout")
	}

	// These errors usually indicate a client configuration problem.
	if errors.Is(err, net.ErrClosed) || amnet.IsConnectionRefused(err) {
		return false
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	return errors.Is(err, io.EOF) || amnet.IsConnectionReset(err)
}

func isContextErr(err error) bool {
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}

func isStorageThrottledErr(err error) bool {
	var response minio.ErrorResponse
	if !errors.As(err, &response) {
		return false
	}

	return isRetryableS3ErrorCode(response.Code) ||
		response.StatusCode == http.StatusTooManyRequests ||
		response.StatusCode >= http.StatusInternalServerError
}

func isRetryableS3ErrorCode(code string) bool {
	switch code {
	case errCodeRequestTimeout,
		errCodeTooManyRequests,
		errCodeTooManyRequestsException,
		errCodeInternalError,
		errCodeNotImplemented,
		errCodeServiceUnavailable,
		errCodeSlowDown:
		return true
	default:
		return false
	}
}
