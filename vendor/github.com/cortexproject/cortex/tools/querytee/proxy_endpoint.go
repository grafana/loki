package querytee

import (
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/cortexproject/cortex/pkg/util"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
)

type ResponsesComparator interface {
	Compare(expected, actual []byte) error
}

type ProxyEndpoint struct {
	backends   []*ProxyBackend
	metrics    *ProxyMetrics
	logger     log.Logger
	comparator ResponsesComparator

	// The route name used to track metrics.
	routeName string
}

func NewProxyEndpoint(backends []*ProxyBackend, routeName string, metrics *ProxyMetrics, logger log.Logger, comparator ResponsesComparator) *ProxyEndpoint {
	return &ProxyEndpoint{
		backends:   backends,
		routeName:  routeName,
		metrics:    metrics,
		logger:     logger,
		comparator: comparator,
	}
}

func (p *ProxyEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	level.Debug(p.logger).Log("msg", "Received request", "path", r.URL.Path, "query", r.URL.RawQuery)

	// Send the same request to all backends.
	wg := sync.WaitGroup{}
	wg.Add(len(p.backends))
	resCh := make(chan *backendResponse, len(p.backends))

	for _, b := range p.backends {
		b := b

		go func() {
			defer wg.Done()

			start := time.Now()
			status, body, err := b.ForwardRequest(r)
			elapsed := time.Since(start)

			res := &backendResponse{
				backend: b,
				status:  status,
				body:    body,
				err:     err,
				elapsed: elapsed,
			}
			resCh <- res

			// Log with a level based on the backend response.
			lvl := level.Debug
			if !res.succeeded() {
				lvl = level.Warn
			}

			lvl(p.logger).Log("msg", "Backend response", "path", r.URL.Path, "query", r.URL.RawQuery, "backend", b.name, "status", status, "elapsed", elapsed)
		}()
	}

	// Wait until all backend requests completed.
	wg.Wait()
	close(resCh)

	// Collect all responses and track metrics for each of them.
	responses := make([]*backendResponse, 0, len(p.backends))
	for res := range resCh {
		responses = append(responses, res)

		p.metrics.durationMetric.WithLabelValues(res.backend.name, r.Method, p.routeName, strconv.Itoa(res.statusCode())).Observe(res.elapsed.Seconds())
	}

	// Select the response to send back to the client.
	downstreamRes := p.pickResponseForDownstream(responses)
	if downstreamRes.err != nil {
		http.Error(w, downstreamRes.err.Error(), http.StatusInternalServerError)
	} else {
		w.WriteHeader(downstreamRes.status)
		if _, err := w.Write(downstreamRes.body); err != nil {
			level.Warn(p.logger).Log("msg", "Unable to write response", "err", err)
		}
	}

	if p.comparator != nil {
		go func() {
			expectedResponse := responses[0]
			actualResponse := responses[1]
			if responses[1].backend.preferred {
				expectedResponse, actualResponse = actualResponse, expectedResponse
			}

			result := resultSuccess
			err := p.compareResponses(expectedResponse, actualResponse)
			if err != nil {
				level.Error(util.Logger).Log("msg", "response comparison failed", "route-name", p.routeName,
					"query", r.URL.RawQuery, "err", err)
				result = resultFailed
			}

			p.metrics.responsesComparedTotal.WithLabelValues(p.routeName, result).Inc()
		}()
	}
}

func (p *ProxyEndpoint) pickResponseForDownstream(responses []*backendResponse) *backendResponse {
	// Look for a successful response from the preferred backend.
	for _, res := range responses {
		if res.backend.preferred && res.succeeded() {
			return res
		}
	}

	// Look for any other successful response.
	for _, res := range responses {
		if res.succeeded() {
			return res
		}
	}

	// No successful response, so let's pick the first one.
	return responses[0]
}

func (p *ProxyEndpoint) compareResponses(expectedResponse, actualResponse *backendResponse) error {
	// compare response body only if we get a 200
	if expectedResponse.status != 200 {
		return fmt.Errorf("skipped comparison of response because we got status code %d from preferred backend's response", expectedResponse.status)
	}

	if actualResponse.status != 200 {
		return fmt.Errorf("skipped comparison of response because we got status code %d from secondary backend's response", expectedResponse.status)
	}

	if expectedResponse.status != actualResponse.status {
		return fmt.Errorf("expected status code %d but got %d", expectedResponse.status, actualResponse.status)
	}

	return p.comparator.Compare(expectedResponse.body, actualResponse.body)
}

type backendResponse struct {
	backend *ProxyBackend
	status  int
	body    []byte
	err     error
	elapsed time.Duration
}

func (r *backendResponse) succeeded() bool {
	if r.err != nil {
		return false
	}

	// We consider the response successful if it's a 2xx or 4xx (but not 429).
	return (r.status >= 200 && r.status < 300) || (r.status >= 400 && r.status < 500 && r.status != 429)
}

func (r *backendResponse) statusCode() int {
	if r.err != nil || r.status <= 0 {
		return 500
	}

	return r.status
}
