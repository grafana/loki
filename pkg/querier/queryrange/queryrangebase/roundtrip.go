// Copyright 2016 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// Mostly lifted from prometheus/web/api/v1/api.go.

package queryrangebase

import (
	"context"
	"flag"
	"io"
	"net/http"
	"time"

	"github.com/grafana/dskit/flagext"
	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/user"
)

const day = 24 * time.Hour

// PassthroughMiddleware is a noop middleware
var PassthroughMiddleware = MiddlewareFunc(func(next Handler) Handler {
	return next
})

// Config for query_range middleware chain.
type Config struct {
	// Deprecated: SplitQueriesByInterval will be removed in the next major release
	SplitQueriesByInterval time.Duration `yaml:"split_queries_by_interval" doc:"deprecated|description=Use -querier.split-queries-by-interval instead. CLI flag: -querier.split-queries-by-day. Split queries by day and execute in parallel."`

	AlignQueriesWithStep bool               `yaml:"align_queries_with_step"`
	ResultsCacheConfig   ResultsCacheConfig `yaml:"results_cache"`
	CacheResults         bool               `yaml:"cache_results"`
	MaxRetries           int                `yaml:"max_retries"`
	ShardedQueries       bool               `yaml:"parallelise_shardable_queries"`
	// List of headers which query_range middleware chain would forward to downstream querier.
	ForwardHeaders flagext.StringSlice `yaml:"forward_headers_list"`
}

// RegisterFlags adds the flags required to config this to the given FlagSet.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.IntVar(&cfg.MaxRetries, "querier.max-retries-per-request", 5, "Maximum number of retries for a single request; beyond this, the downstream error is returned.")
	f.BoolVar(&cfg.AlignQueriesWithStep, "querier.align-querier-with-step", false, "Mutate incoming queries to align their start and end with their step.")
	f.BoolVar(&cfg.CacheResults, "querier.cache-results", false, "Cache query results.")
	f.BoolVar(&cfg.ShardedQueries, "querier.parallelise-shardable-queries", true, "Perform query parallelisations based on storage sharding configuration and query ASTs. This feature is supported only by the chunks storage engine.")
	f.Var(&cfg.ForwardHeaders, "frontend.forward-headers-list", "List of headers forwarded by the query Frontend to downstream querier.")
	cfg.ResultsCacheConfig.RegisterFlags(f)
}

// Validate validates the config.
func (cfg *Config) Validate() error {
	if cfg.SplitQueriesByInterval != 0 {
		return errors.New("the yaml flag `split_queries_by_interval` must now be set in the `limits_config` section instead of the `query_range` config section")
	}
	if cfg.CacheResults {
		if err := cfg.ResultsCacheConfig.Validate(); err != nil {
			return errors.Wrap(err, "invalid results_cache config")
		}
	}
	return nil
}

// HandlerFunc is like http.HandlerFunc, but for Handler.
type HandlerFunc func(context.Context, Request) (Response, error)

// Do implements Handler.
func (q HandlerFunc) Do(ctx context.Context, req Request) (Response, error) {
	return q(ctx, req)
}

// Handler is like http.Handle, but specifically for Prometheus query_range calls.
type Handler interface {
	Do(context.Context, Request) (Response, error)
}

// MiddlewareFunc is like http.HandlerFunc, but for Middleware.
type MiddlewareFunc func(Handler) Handler

// Wrap implements Middleware.
func (q MiddlewareFunc) Wrap(h Handler) Handler {
	return q(h)
}

// Middleware is a higher order Handler.
type Middleware interface {
	Wrap(Handler) Handler
}

// MergeMiddlewares produces a middleware that applies multiple middleware in turn;
// ie Merge(f,g,h).Wrap(handler) == f.Wrap(g.Wrap(h.Wrap(handler)))
func MergeMiddlewares(middleware ...Middleware) Middleware {
	return MiddlewareFunc(func(next Handler) Handler {
		for i := len(middleware) - 1; i >= 0; i-- {
			next = middleware[i].Wrap(next)
		}
		return next
	})
}

// Tripperware is a signature for all http client-side middleware.
type Tripperware func(http.RoundTripper) http.RoundTripper

// RoundTripFunc is to http.RoundTripper what http.HandlerFunc is to http.Handler.
type RoundTripFunc func(*http.Request) (*http.Response, error)

// RoundTrip implements http.RoundTripper.
func (f RoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

type roundTripper struct {
	roundTripperHandler
	handler Handler
	headers []string
}

// NewRoundTripper merges a set of middlewares into an handler, then inject it into the `next` roundtripper
// using the codec to translate requests and responses.
func NewRoundTripper(next http.RoundTripper, codec Codec, headers []string, middlewares ...Middleware) http.RoundTripper {
	transport := roundTripper{
		roundTripperHandler: roundTripperHandler{
			next:  next,
			codec: codec,
		},
		headers: headers,
	}
	transport.handler = MergeMiddlewares(middlewares...).Wrap(&transport)
	return transport
}

func (q roundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	// include the headers specified in the roundTripper during decoding the request.
	request, err := q.codec.DecodeRequest(r.Context(), r, q.headers)
	if err != nil {
		return nil, err
	}

	if span := opentracing.SpanFromContext(r.Context()); span != nil {
		request.LogToSpan(span)
	}

	response, err := q.handler.Do(r.Context(), request)
	if err != nil {
		return nil, err
	}

	return q.codec.EncodeResponse(r.Context(), response)
}

type roundTripperHandler struct {
	next  http.RoundTripper
	codec Codec
}

// NewRoundTripperHandler returns a handler that translates Loki requests into http requests
// and passes down these to the next RoundTripper.
func NewRoundTripperHandler(next http.RoundTripper, codec Codec) Handler {
	return roundTripperHandler{
		next:  next,
		codec: codec,
	}
}

// Do implements Handler.
func (q roundTripperHandler) Do(ctx context.Context, r Request) (Response, error) {
	request, err := q.codec.EncodeRequest(ctx, r)
	if err != nil {
		return nil, err
	}

	if err := user.InjectOrgIDIntoHTTPRequest(ctx, request); err != nil {
		return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
	}

	response, err := q.next.RoundTrip(request)
	if err != nil {
		return nil, err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1024)) //nolint:errcheck
		response.Body.Close()
	}()

	return q.codec.DecodeResponse(ctx, response, r)
}
