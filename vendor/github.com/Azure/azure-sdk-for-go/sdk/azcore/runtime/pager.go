// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/tracing"
)

// PagingHandler contains the required data for constructing a Pager.
type PagingHandler[T any] struct {
	// More returns a boolean indicating if there are more pages to fetch.
	// It uses the provided page to make the determination.
	More func(T) bool

	// Fetcher fetches the first and subsequent pages.
	Fetcher func(context.Context, *T) (T, error)

	// Tracer contains the Tracer from the client that's creating the Pager.
	Tracer tracing.Tracer
}

// Pager provides operations for iterating over paged responses.
// Methods on this type are not safe for concurrent use.
type Pager[T any] struct {
	current   *T
	handler   PagingHandler[T]
	tracer    tracing.Tracer
	firstPage bool
	// fetchErr is set when Fetcher returns an error, placing the Pager in a
	// terminal state so that More returns false.
	fetchErr error
}

// NewPager creates an instance of Pager using the specified PagingHandler.
// Pass a non-nil T for firstPage if the first page has already been retrieved.
func NewPager[T any](handler PagingHandler[T]) *Pager[T] {
	return &Pager[T]{
		handler:   handler,
		tracer:    handler.Tracer,
		firstPage: true,
	}
}

// More returns true if there are more pages to retrieve.
//
// If a prior call to [Pager.NextPage] returned an error while fetching a page,
// the Pager enters a terminal state and More returns false so that a for loop
// over the pager terminates instead of retrying indefinitely.
func (p *Pager[T]) More() bool {
	// a failed fetch puts the Pager in a terminal state; there are no more pages
	// to retrieve regardless of whether it was the first or a subsequent page.
	if p.fetchErr != nil {
		return false
	}
	if p.current != nil {
		return p.handler.More(*p.current)
	}
	return true
}

// NextPage advances the pager to the next page.
//
// If fetching the page returns an error, the Pager enters a terminal state:
// [Pager.More] returns false and every subsequent call to NextPage returns the
// same error without invoking the fetcher again.
func (p *Pager[T]) NextPage(ctx context.Context) (T, error) {
	if p.fetchErr != nil {
		// a prior fetch failed; the Pager is in a terminal state. return the
		// stored error rather than re-invoking the fetcher, which may not be
		// safe to retry (e.g. stateful handlers that latch into a done state).
		return *new(T), p.fetchErr
	}
	if p.current != nil {
		if p.firstPage {
			// we get here if it's an LRO-pager, we already have the first page
			p.firstPage = false
			return *p.current, nil
		} else if !p.handler.More(*p.current) {
			return *new(T), errors.New("no more pages")
		}
	} else {
		// non-LRO case, first page
		p.firstPage = false
	}

	var err error
	ctx, endSpan := StartSpan(ctx, fmt.Sprintf("%s.NextPage", shortenTypeName(reflect.TypeOf(*p).Name())), p.tracer, nil)
	defer func() { endSpan(err) }()

	resp, err := p.handler.Fetcher(ctx, p.current)
	if err != nil {
		p.fetchErr = err
		return *new(T), err
	}
	p.current = &resp
	return *p.current, nil
}

// UnmarshalJSON implements the json.Unmarshaler interface for Pager[T].
func (p *Pager[T]) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &p.current)
}

// FetcherForNextLinkOptions contains the optional values for [FetcherForNextLink].
type FetcherForNextLinkOptions struct {
	// NextReq is the func to be called when requesting subsequent pages.
	// Used for paged operations that have a custom next link operation.
	NextReq func(context.Context, string) (*policy.Request, error)

	// StatusCodes contains additional HTTP status codes indicating success.
	// The default value is http.StatusOK.
	StatusCodes []int

	// HTTPVerb specifies the HTTP verb to use when fetching the next page.
	// The default value is http.MethodGet.
	// This field is only used when NextReq is not specified.
	HTTPVerb string
}

// FetcherForNextLink is a helper containing boilerplate code to simplify creating a PagingHandler[T].Fetcher from a next link URL.
//   - ctx is the [context.Context] controlling the lifetime of the HTTP operation
//   - pl is the [Pipeline] used to dispatch the HTTP request
//   - nextLink is the URL used to fetch the next page. the empty string indicates the first page is to be requested
//   - firstReq is the func to be called when creating the request for the first page
//   - options contains any optional parameters, pass nil to accept the default values
func FetcherForNextLink(ctx context.Context, pl Pipeline, nextLink string, firstReq func(context.Context) (*policy.Request, error), options *FetcherForNextLinkOptions) (*http.Response, error) {
	var req *policy.Request
	var err error
	if options == nil {
		options = &FetcherForNextLinkOptions{}
	}
	if nextLink == "" {
		req, err = firstReq(ctx)
	} else if nextLink, err = EncodeQueryParams(nextLink); err == nil {
		if options.NextReq != nil {
			req, err = options.NextReq(ctx, nextLink)
		} else {
			verb := http.MethodGet
			if options.HTTPVerb != "" {
				verb = options.HTTPVerb
			}
			req, err = NewRequest(ctx, verb, nextLink)
		}
	}
	if err != nil {
		return nil, err
	}
	resp, err := pl.Do(req)
	if err != nil {
		return nil, err
	}
	successCodes := []int{http.StatusOK}
	successCodes = append(successCodes, options.StatusCodes...)
	if !HasStatusCode(resp, successCodes...) {
		return nil, NewResponseError(resp)
	}
	return resp, nil
}
