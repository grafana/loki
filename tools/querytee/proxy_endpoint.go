package querytee

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
)

type ResponsesComparator interface {
	Compare(expected, actual []byte, queryEvaluationTime time.Time) (*ComparisonSummary, error)
}

type ComparisonSummary struct {
	skipped        bool
	missingMetrics int
}

type ProxyEndpoint struct {
	backends   []*ProxyBackend
	metrics    *ProxyMetrics
	logger     log.Logger
	comparator ResponsesComparator

	instrumentCompares bool

	// Whether for this endpoint there's a preferred backend configured.
	hasPreferredBackend bool

	// The route name used to track metrics.
	routeName string
}

func NewProxyEndpoint(backends []*ProxyBackend, routeName string, metrics *ProxyMetrics, logger log.Logger, comparator ResponsesComparator, instrumentCompares bool) *ProxyEndpoint {
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
		instrumentCompares:  instrumentCompares,
	}
}

func (p *ProxyEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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

	p.metrics.responsesTotal.WithLabelValues(downstreamRes.backend.name, r.Method, p.routeName, detectIssuer(r)).Inc()
}

func (p *ProxyEndpoint) executeBackendRequests(r *http.Request, resCh chan *backendResponse) {
	var (
		wg                  = sync.WaitGroup{}
		err                 error
		body                []byte
		expectedResponseIdx int
		responses           = make([]*backendResponse, len(p.backends))
		query               = r.URL.RawQuery
		issuer              = detectIssuer(r)
	)

	if r.Body != nil {
		body, err = io.ReadAll(r.Body)
		if err != nil {
			level.Warn(p.logger).Log("msg", "Unable to read request body", "err", err)
			return
		}
		if err := r.Body.Close(); err != nil {
			level.Warn(p.logger).Log("msg", "Unable to close request body", "err", err)
		}

		r.Body = io.NopCloser(bytes.NewReader(body))
		if err := r.ParseForm(); err != nil {
			level.Warn(p.logger).Log("msg", "Unable to parse form", "err", err)
		}
		query = r.Form.Encode()
	}

	level.Debug(p.logger).Log("msg", "Received request", "path", r.URL.Path, "query", query)

	wg.Add(len(p.backends))
	for i, b := range p.backends {
		go func() {
			defer wg.Done()
			var (
				bodyReader io.ReadCloser
				start      = time.Now()
				lvl        = level.Debug
			)
			if len(body) > 0 {
				bodyReader = io.NopCloser(bytes.NewReader(body))
			}

			if b.filter != nil && !b.filter.Match([]byte(r.URL.String())) {
				lvl(p.logger).Log("msg", "Skipping non-preferred backend", "path", r.URL.Path, "query", query, "backend", b.name)
				return
			}

			status, body, err := b.ForwardRequest(r, bodyReader)
			elapsed := time.Since(start)

			res := &backendResponse{
				backend: b,
				status:  status,
				body:    body,
				err:     err,
			}

			// Log with a level based on the backend response.
			if !res.succeeded() {
				lvl = level.Warn
			}

			lvl(p.logger).Log("msg", "Backend response", "path", r.URL.Path, "query", query, "backend", b.name, "status", status, "elapsed", elapsed)
			p.metrics.requestDuration.WithLabelValues(
				res.backend.name,
				r.Method,
				p.routeName,
				strconv.Itoa(res.statusCode()),
				issuer,
			).Observe(elapsed.Seconds())

			// Keep track of the response if required.
			if p.comparator != nil {
				if b.preferred {
					expectedResponseIdx = i
				}
				responses[i] = res
			}

			resCh <- res
		}()
	}

	// Wait until all backend requests completed.
	wg.Wait()
	close(resCh)

	// Compare responses.
	if p.comparator != nil {
		expectedResponse := responses[expectedResponseIdx]
		for i := range responses {
			if i == expectedResponseIdx {
				continue
			}
			actualResponse := responses[i]

			result := comparisonSuccess
			summary, err := p.compareResponses(expectedResponse, actualResponse, time.Now().UTC())
			if err != nil {
				level.Error(p.logger).Log("msg", "response comparison failed",
					"backend-name", p.backends[i].name,
					"route-name", p.routeName,
					"query", r.URL.RawQuery, "err", err)
				result = comparisonFailed
			} else if summary != nil && summary.skipped {
				result = comparisonSkipped
			}

			if p.instrumentCompares && summary != nil {
				p.metrics.missingMetrics.WithLabelValues(p.backends[i].name, p.routeName, result, issuer).Observe(float64(summary.missingMetrics))
			}
			p.metrics.responsesComparedTotal.WithLabelValues(p.backends[i].name, p.routeName, result, issuer).Inc()
		}
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

func (p *ProxyEndpoint) compareResponses(expectedResponse, actualResponse *backendResponse, queryEvalTime time.Time) (*ComparisonSummary, error) {
	if expectedResponse.err != nil {
		return &ComparisonSummary{skipped: true}, nil
	}

	if actualResponse.err != nil {
		return nil, fmt.Errorf("skipped comparison of response because the request to the secondary backend failed: %w", actualResponse.err)
	}

	// compare response body only if we get a 200
	if expectedResponse.status != 200 {
		return &ComparisonSummary{skipped: true}, nil
	}

	if actualResponse.status != 200 {
		return nil, fmt.Errorf("skipped comparison of response because we got status code %d from secondary backend's response", actualResponse.status)
	}

	if expectedResponse.status != actualResponse.status {
		return nil, fmt.Errorf("expected status code %d but got %d", expectedResponse.status, actualResponse.status)
	}

	return p.comparator.Compare(expectedResponse.body, actualResponse.body, queryEvalTime)
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

func detectIssuer(r *http.Request) string {
	if strings.HasPrefix(r.Header.Get("User-Agent"), "loki-canary") {
		return canaryIssuer
	}
	return unknownIssuer
}
