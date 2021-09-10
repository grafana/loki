package querytee

import (
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"

	util_log "github.com/cortexproject/cortex/pkg/util/log"
)

type ResponsesComparator interface {
	Compare(expected, actual []byte) error
}

type ProxyEndpoint struct {
	backends   []*ProxyBackend
	metrics    *ProxyMetrics
	logger     log.Logger
	comparator ResponsesComparator

	// Whether for this endpoint there's a preferred backend configured.
	hasPreferredBackend bool

	// The route name used to track metrics.
	routeName string
}

func NewProxyEndpoint(backends []*ProxyBackend, routeName string, metrics *ProxyMetrics, logger log.Logger, comparator ResponsesComparator) *ProxyEndpoint {
	hasPreferredBackend := false
	for _, backend := range backends {
		if backend.preferred {
			hasPreferredBackend = true
			break
		}
	}

	return &ProxyEndpoint{
		backends:            backends,
		routeName:           routeName,
		metrics:             metrics,
		logger:              logger,
		comparator:          comparator,
		hasPreferredBackend: hasPreferredBackend,
	}
}

func (p *ProxyEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	level.Debug(p.logger).Log("msg", "Received request", "path", r.URL.Path, "query", r.URL.RawQuery)

	// Send the same request to all backends.
	resCh := make(chan *backendResponse, len(p.backends))
	go p.executeBackendRequests(r, resCh)

	// Wait for the first response that's feasible to be sent back to the client.
	downstreamRes := p.waitBackendResponseForDownstream(resCh)

	if downstreamRes.err != nil {
		http.Error(w, downstreamRes.err.Error(), http.StatusInternalServerError)
	} else {
		w.WriteHeader(downstreamRes.status)
		if _, err := w.Write(downstreamRes.body); err != nil {
			level.Warn(p.logger).Log("msg", "Unable to write response", "err", err)
		}
	}

	p.metrics.responsesTotal.WithLabelValues(downstreamRes.backend.name, r.Method, p.routeName).Inc()
}

func (p *ProxyEndpoint) executeBackendRequests(r *http.Request, resCh chan *backendResponse) {
	responses := make([]*backendResponse, 0, len(p.backends))

	var (
		wg  = sync.WaitGroup{}
		mtx = sync.Mutex{}
	)
	wg.Add(len(p.backends))

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
			}

			// Log with a level based on the backend response.
			lvl := level.Debug
			if !res.succeeded() {
				lvl = level.Warn
			}

			lvl(p.logger).Log("msg", "Backend response", "path", r.URL.Path, "query", r.URL.RawQuery, "backend", b.name, "status", status, "elapsed", elapsed)
			p.metrics.requestDuration.WithLabelValues(res.backend.name, r.Method, p.routeName, strconv.Itoa(res.statusCode())).Observe(elapsed.Seconds())

			// Keep track of the response if required.
			if p.comparator != nil {
				mtx.Lock()
				responses = append(responses, res)
				mtx.Unlock()
			}

			resCh <- res
		}()
	}

	// Wait until all backend requests completed.
	wg.Wait()
	close(resCh)

	// Compare responses.
	if p.comparator != nil {
		expectedResponse := responses[0]
		actualResponse := responses[1]
		if responses[1].backend.preferred {
			expectedResponse, actualResponse = actualResponse, expectedResponse
		}

		result := comparisonSuccess
		err := p.compareResponses(expectedResponse, actualResponse)
		if err != nil {
			level.Error(util_log.Logger).Log("msg", "response comparison failed", "route-name", p.routeName,
				"query", r.URL.RawQuery, "err", err)
			result = comparisonFailed
		}

		p.metrics.responsesComparedTotal.WithLabelValues(p.routeName, result).Inc()
	}
}

func (p *ProxyEndpoint) waitBackendResponseForDownstream(resCh chan *backendResponse) *backendResponse {
	var (
		responses                 = make([]*backendResponse, 0, len(p.backends))
		preferredResponseReceived = false
	)

	for res := range resCh {
		// If the response is successful we can immediately return it if:
		// - There's no preferred backend configured
		// - Or this response is from the preferred backend
		// - Or the preferred backend response has already been received and wasn't successful
		if res.succeeded() && (!p.hasPreferredBackend || res.backend.preferred || preferredResponseReceived) {
			return res
		}

		// If we received a non successful response from the preferred backend, then we can
		// return the first successful response received so far (if any).
		if res.backend.preferred && !res.succeeded() {
			preferredResponseReceived = true

			for _, prevRes := range responses {
				if prevRes.succeeded() {
					return prevRes
				}
			}
		}

		// Otherwise we keep track of it for later.
		responses = append(responses, res)
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
		return fmt.Errorf("skipped comparison of response because we got status code %d from secondary backend's response", actualResponse.status)
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
