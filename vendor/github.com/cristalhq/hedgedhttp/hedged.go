package hedgedhttp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

const infiniteTimeout = 30 * 24 * time.Hour // domain specific infinite

// NewClient returns a new http.Client which implements hedged requests pattern.
// Given Client starts a new request after a timeout from previous request.
// Starts no more than upto requests.
func NewClient(timeout time.Duration, upto int, client *http.Client) (*http.Client, error) {
	newClient, _, err := NewClientAndStats(timeout, upto, client)
	if err != nil {
		return nil, err
	}
	return newClient, nil
}

// NewClientAndStats returns a new http.Client which implements hedged requests pattern
// And Stats object that can be queried to obtain client's metrics.
// Given Client starts a new request after a timeout from previous request.
// Starts no more than upto requests.
func NewClientAndStats(timeout time.Duration, upto int, client *http.Client) (*http.Client, *Stats, error) {
	if client == nil {
		client = &http.Client{
			Timeout: 5 * time.Second,
		}
	}

	newTransport, metrics, err := NewRoundTripperAndStats(timeout, upto, client.Transport)
	if err != nil {
		return nil, nil, err
	}

	client.Transport = newTransport

	return client, metrics, nil
}

// NewRoundTripper returns a new http.RoundTripper which implements hedged requests pattern.
// Given RoundTripper starts a new request after a timeout from previous request.
// Starts no more than upto requests.
func NewRoundTripper(timeout time.Duration, upto int, rt http.RoundTripper) (http.RoundTripper, error) {
	newRT, _, err := NewRoundTripperAndStats(timeout, upto, rt)
	if err != nil {
		return nil, err
	}
	return newRT, nil
}

// NewRoundTripperAndStats returns a new http.RoundTripper which implements hedged requests pattern
// And Stats object that can be queried to obtain client's metrics.
// Given RoundTripper starts a new request after a timeout from previous request.
// Starts no more than upto requests.
func NewRoundTripperAndStats(timeout time.Duration, upto int, rt http.RoundTripper) (http.RoundTripper, *Stats, error) {
	switch {
	case timeout < 0:
		return nil, nil, errors.New("hedgedhttp: timeout cannot be negative")
	case upto < 1:
		return nil, nil, errors.New("hedgedhttp: upto must be greater than 0")
	}

	if rt == nil {
		rt = http.DefaultTransport
	}

	if timeout == 0 {
		timeout = time.Nanosecond // smallest possible timeout if not set
	}

	hedged := &hedgedTransport{
		rt:      rt,
		timeout: timeout,
		upto:    upto,
		metrics: &Stats{},
	}
	return hedged, hedged.metrics, nil
}

type hedgedTransport struct {
	rt      http.RoundTripper
	timeout time.Duration
	upto    int
	metrics *Stats
}

func (ht *hedgedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	mainCtx := req.Context()

	timeout := ht.timeout
	errOverall := &MultiError{}
	resultCh := make(chan indexedResp, ht.upto)
	errorCh := make(chan error, ht.upto)

	ht.metrics.requestedRoundTripsInc()

	resultIdx := -1
	cancels := make([]func(), ht.upto)

	defer runInPool(func() {
		for i, cancel := range cancels {
			if i != resultIdx && cancel != nil {
				ht.metrics.canceledSubRequestsInc()
				cancel()
			}
		}
	})

	for sent := 0; len(errOverall.Errors) < ht.upto; sent++ {
		if sent < ht.upto {
			idx := sent
			subReq, cancel := reqWithCtx(req, mainCtx, idx != 0)
			cancels[idx] = cancel

			runInPool(func() {
				ht.metrics.actualRoundTripsInc()
				resp, err := ht.rt.RoundTrip(subReq)
				if err != nil {
					ht.metrics.failedRoundTripsInc()
					errorCh <- err
				} else {
					resultCh <- indexedResp{idx, resp}
				}
			})
		}

		// all request sent - effectively disabling timeout between requests
		if sent == ht.upto {
			timeout = infiniteTimeout
		}
		resp, err := waitResult(mainCtx, resultCh, errorCh, timeout)

		switch {
		case resp.Resp != nil:
			resultIdx = resp.Index
			return resp.Resp, nil
		case mainCtx.Err() != nil:
			ht.metrics.canceledByUserRoundTripsInc()
			return nil, mainCtx.Err()
		case err != nil:
			errOverall.Errors = append(errOverall.Errors, err)
		}
	}

	// all request have returned errors
	return nil, errOverall
}

func waitResult(ctx context.Context, resultCh <-chan indexedResp, errorCh <-chan error, timeout time.Duration) (indexedResp, error) {
	// try to read result first before blocking on all other channels
	select {
	case res := <-resultCh:
		return res, nil
	default:
		timer := getTimer(timeout)
		defer returnTimer(timer)

		select {
		case res := <-resultCh:
			return res, nil

		case reqErr := <-errorCh:
			return indexedResp{}, reqErr

		case <-ctx.Done():
			return indexedResp{}, ctx.Err()

		case <-timer.C:
			return indexedResp{}, nil // it's not a request timeout, it's timeout BETWEEN consecutive requests
		}
	}
}

type indexedResp struct {
	Index int
	Resp  *http.Response
}

func reqWithCtx(r *http.Request, ctx context.Context, isHedged bool) (*http.Request, context.CancelFunc) {
	ctx, cancel := context.WithCancel(ctx)
	if isHedged {
		ctx = context.WithValue(ctx, hedgedRequest{}, struct{}{})
	}
	req := r.WithContext(ctx)
	return req, cancel
}

type hedgedRequest struct{}

// IsHedgedRequest reports when a request is hedged.
func IsHedgedRequest(r *http.Request) bool {
	val := r.Context().Value(hedgedRequest{})
	return val != nil
}

var taskQueue = make(chan func())

func runInPool(task func()) {
	select {
	case taskQueue <- task:
		// submitted, everything is ok

	default:
		go func() {
			// do the given task
			task()

			const cleanupDuration = 10 * time.Second
			cleanupTicker := time.NewTicker(cleanupDuration)
			defer cleanupTicker.Stop()

			for {
				select {
				case t := <-taskQueue:
					t()
					cleanupTicker.Reset(cleanupDuration)
				case <-cleanupTicker.C:
					return
				}
			}
		}()
	}
}

// MultiError is an error type to track multiple errors. This is used to
// accumulate errors in cases and return them as a single "error".
// Inspired by https://github.com/hashicorp/go-multierror
type MultiError struct {
	Errors        []error
	ErrorFormatFn ErrorFormatFunc
}

func (e *MultiError) Error() string {
	fn := e.ErrorFormatFn
	if fn == nil {
		fn = listFormatFunc
	}
	return fn(e.Errors)
}

func (e *MultiError) String() string {
	return fmt.Sprintf("*%#v", e.Errors)
}

// ErrorOrNil returns an error if there are some.
func (e *MultiError) ErrorOrNil() error {
	switch {
	case e == nil || len(e.Errors) == 0:
		return nil
	default:
		return e
	}
}

// ErrorFormatFunc is called by MultiError to return the list of errors as a string.
type ErrorFormatFunc func([]error) string

func listFormatFunc(es []error) string {
	if len(es) == 1 {
		return fmt.Sprintf("1 error occurred:\n\t* %s\n\n", es[0])
	}

	points := make([]string, len(es))
	for i, err := range es {
		points[i] = fmt.Sprintf("* %s", err)
	}

	return fmt.Sprintf("%d errors occurred:\n\t%s\n\n", len(es), strings.Join(points, "\n\t"))
}

var timerPool = sync.Pool{New: func() interface{} {
	return time.NewTimer(time.Second)
}}

func getTimer(duration time.Duration) *time.Timer {
	timer := timerPool.Get().(*time.Timer)
	timer.Reset(duration)
	return timer
}

func returnTimer(timer *time.Timer) {
	timer.Stop()
	select {
	case <-timer.C:
	default:
	}
	timerPool.Put(timer)
}
