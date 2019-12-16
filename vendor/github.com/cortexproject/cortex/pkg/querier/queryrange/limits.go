package queryrange

import (
	"context"
	"net/http"
	"time"

	"github.com/cortexproject/cortex/pkg/util/validation"
	"github.com/prometheus/prometheus/pkg/timestamp"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/user"
)

// Limits allows us to specify per-tenant runtime limits on the behavior of
// the query handling code.
type Limits interface {
	MaxQueryLength(string) time.Duration
	MaxQueryParallelism(string) int
}

type limits struct {
	Limits
	next Handler
}

// LimitsMiddleware creates a new Middleware that invalidates large queries based on Limits interface.
func LimitsMiddleware(l Limits) Middleware {
	return MiddlewareFunc(func(next Handler) Handler {
		return limits{
			next:   next,
			Limits: l,
		}
	})
}

func (l limits) Do(ctx context.Context, r Request) (Response, error) {
	userid, err := user.ExtractOrgID(ctx)
	if err != nil {
		return nil, err
	}

	maxQueryLen := l.MaxQueryLength(userid)
	queryLen := timestamp.Time(r.GetEnd()).Sub(timestamp.Time(r.GetStart()))
	if maxQueryLen != 0 && queryLen > maxQueryLen {
		return nil, httpgrpc.Errorf(http.StatusBadRequest, validation.ErrQueryTooLong, queryLen, maxQueryLen)
	}
	return l.next.Do(ctx, r)
}

// RequestResponse contains a request response and the respective request that was used.
type RequestResponse struct {
	Request  Request
	Response Response
}

// DoRequests executes a list of requests in parallel. The limits parameters is used to limit parallelism per single request.
func DoRequests(ctx context.Context, downstream Handler, reqs []Request, limits Limits) ([]RequestResponse, error) {
	userid, err := user.ExtractOrgID(ctx)
	if err != nil {
		return nil, err
	}

	// If one of the requests fail, we want to be able to cancel the rest of them.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Feed all requests to a bounded intermediate channel to limit parallelism.
	intermediate := make(chan Request)
	go func() {
		for _, req := range reqs {
			intermediate <- req
		}
		close(intermediate)
	}()

	respChan, errChan := make(chan RequestResponse), make(chan error)
	parallelism := limits.MaxQueryParallelism(userid)
	if parallelism > len(reqs) {
		parallelism = len(reqs)
	}
	for i := 0; i < parallelism; i++ {
		go func() {
			for req := range intermediate {
				resp, err := downstream.Do(ctx, req)
				if err != nil {
					errChan <- err
				} else {
					respChan <- RequestResponse{req, resp}
				}
			}
		}()
	}

	resps := make([]RequestResponse, 0, len(reqs))
	var firstErr error
	for range reqs {
		select {
		case resp := <-respChan:
			resps = append(resps, resp)
		case err := <-errChan:
			if firstErr == nil {
				cancel()
				firstErr = err
			}
		}
	}

	return resps, firstErr
}
