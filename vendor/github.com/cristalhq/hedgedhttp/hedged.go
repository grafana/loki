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

// Client represents a hedged HTTP client.
type Client struct {
	rt    http.RoundTripper
	stats *Stats
}

// Config for the [Client].
type Config struct {
	// Transport of the [Client].
	// Default is nil which results in [net/http.DefaultTransport].
	Transport http.RoundTripper

	// Upto says how much requests to make.
	// Default is zero which means no hedged requests will be made.
	Upto int

	// Delay before 2 consequitive hedged requests.
	Delay time.Duration

	// Next returns the upto and delay for each HTTP that will be hedged.
	// Default is nil which results in (Upto, Delay) result.
	Next NextFn
}

// NextFn represents a function that is called for each HTTP request for retrieving hedging options.
type NextFn func() (upto int, delay time.Duration)

// New returns a new Client for the given config.
func New(cfg Config) (*Client, error) {
	switch {
	case cfg.Delay < 0:
		return nil, errors.New("hedgedhttp: timeout cannot be negative")
	case cfg.Upto < 0:
		return nil, errors.New("hedgedhttp: upto cannot be negative")
	}
	if cfg.Transport == nil {
		cfg.Transport = http.DefaultTransport
	}

	rt, stats, err := NewRoundTripperAndStats(cfg.Delay, cfg.Upto, cfg.Transport)
	if err != nil {
		return nil, err
	}

	// TODO(cristaloleg): this should be removed after internals cleanup.
	rt2, ok := rt.(*hedgedTransport)
	if !ok {
		panic(fmt.Sprintf("want *hedgedTransport got %T", rt))
	}
	rt2.next = cfg.Next

	c := &Client{
		rt:    rt2,
		stats: stats,
	}
	return c, nil
}

// Stats returns statistics for the given client, see [Stats] methods.
func (c *Client) Stats() *Stats {
	return c.stats
}

// Do does the same as [RoundTrip], this method is presented to align with [net/http.Client].
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	return c.rt.RoundTrip(req)
}

// RoundTrip implements [net/http.RoundTripper] interface.
func (c *Client) RoundTrip(req *http.Request) (*http.Response, error) {
	return c.rt.RoundTrip(req)
}

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
	case upto < 0:
		return nil, nil, errors.New("hedgedhttp: upto cannot be negative")
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
	next    NextFn
	metrics *Stats
}

func (ht *hedgedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	mainCtx := req.Context()

	upto, timeout := ht.upto, ht.timeout
	if ht.next != nil {
		upto, timeout = ht.next()
	}

	// no hedged requests, just a regular one.
	if upto <= 0 {
		return ht.rt.RoundTrip(req)
	}
	// rollback to default timeout.
	if timeout < 0 {
		timeout = ht.timeout
	}

	errOverall := &MultiError{}
	resultCh := make(chan indexedResp, upto)
	errorCh := make(chan error, upto)

	ht.metrics.requestedRoundTripsInc()

	resultIdx := -1
	cancels := make([]func(), upto)

	defer runInPool(func() {
		for i, cancel := range cancels {
			if i != resultIdx && cancel != nil {
				ht.metrics.canceledSubRequestsInc()
				cancel()
			}
		}
	})

	for sent := 0; len(errOverall.Errors) < upto; sent++ {
		if sent < upto {
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
		if sent == upto {
			timeout = infiniteTimeout
		}
		resp, err := waitResult(mainCtx, resultCh, errorCh, timeout)

		switch {
		case resp.Resp != nil:
			resultIdx = resp.Index
			if resultIdx == 0 {
				ht.metrics.originalRequestWinsInc()
			} else {
				ht.metrics.hedgedRequestWinsInc()
			}
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
