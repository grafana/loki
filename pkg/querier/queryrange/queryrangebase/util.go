package queryrangebase

import (
	"context"

	"github.com/grafana/loki/v3/pkg/storage/chunk/cache/resultscache"
)

// RequestResponse contains a request response and the respective request that was used.
type RequestResponse struct {
	Request  Request
	Response Response
}

// DoRequests executes a list of requests in parallel.
func DoRequests(ctx context.Context, downstream Handler, reqs []Request, parallelism int) ([]RequestResponse, error) {
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

type queryMergerAsCacheResponseMerger struct {
	Merger
}

func (m *queryMergerAsCacheResponseMerger) MergeResponse(responses ...resultscache.Response) (resultscache.Response, error) {
	cacheResponses := make([]Response, 0, len(responses))
	for _, r := range responses {
		cacheResponses = append(cacheResponses, r.(Response))
	}
	response, err := m.Merger.MergeResponse(cacheResponses...)
	if err != nil {
		return nil, err
	}
	return response.(resultscache.Response), nil
}

func FromQueryResponseMergerToCacheResponseMerger(m Merger) resultscache.ResponseMerger {
	return &queryMergerAsCacheResponseMerger{m}
}
