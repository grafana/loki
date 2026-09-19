package gcs

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"

	"google.golang.org/api/googleapi"
	amnet "k8s.io/apimachinery/pkg/util/net"
)

// IsRetryableErr reports whether err is a transient error returned by the
// Thanos GCS client.
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

	if errors.Is(err, io.EOF) || amnet.IsConnectionReset(err) {
		return true
	}

	var response *googleapi.Error
	if !errors.As(err, &response) {
		return false
	}

	return response.Code == http.StatusRequestTimeout || response.Code == http.StatusGatewayTimeout
}

func isContextErr(err error) bool {
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}

func isStorageThrottledErr(err error) bool {
	var response *googleapi.Error
	if !errors.As(err, &response) {
		return false
	}

	return response.Code == http.StatusTooManyRequests || response.Code/100 == 5
}
