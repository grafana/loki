package congestion

import (
	"io"
	"net/http"

	"github.com/go-kit/log"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/hedging"
)

// Controller handles congestion by:
// - determining if calls to object storage can be retried
// - defining and enforcing a back-pressure mechanism
// - centralising retries & hedging
type Controller interface {
	client.ObjectClient

	// Wrap wraps a given object store client and handles congestion against its backend service
	Wrap(client client.ObjectClient) client.ObjectClient

	withLogger(log.Logger) Controller
	withRetrier(Retrier) Controller
	withHedger(Hedger) Controller
	withMetrics(*Metrics) Controller

	getRetrier() Retrier
	getHedger() Hedger
	getMetrics() *Metrics
}

type DoRequestFunc func(attempt int) (io.ReadCloser, int64, error)
type IsRetryableErrFunc func(err error) bool

// Retrier orchestrates requests & subsequent retries (if configured).
// NOTE: this only supports ObjectClient.GetObject calls right now.
type Retrier interface {
	// Do executes a given function which is expected to be a GetObject call, and its return signature matches that.
	// Any failed requests will be retried.
	//
	// count is the current request count; any positive number indicates retries, 0 indicates first attempt.
	Do(fn DoRequestFunc, isRetryable IsRetryableErrFunc, onSuccess func(), onError func()) (io.ReadCloser, int64, error)

	withLogger(log.Logger) Retrier
}

// Hedger orchestrates request "hedging", which is the process of sending a new request when the old request is
// taking too long, and returning the response that is received first
type Hedger interface {
	// HTTPClient returns an HTTP client which is responsible for handling both the initial and all hedged requests.
	// It is recommended that retries are not hedged.
	// Bear in mind this function can be called several times, and should return the same client each time.
	HTTPClient(cfg hedging.Config) (*http.Client, error)

	withLogger(log.Logger) Hedger
}
