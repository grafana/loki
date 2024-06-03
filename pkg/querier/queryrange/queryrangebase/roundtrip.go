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
	"net/http"
	"time"

	"github.com/pkg/errors"

	"github.com/grafana/dskit/flagext"
)

const day = 24 * time.Hour

// PassthroughMiddleware is a noop middleware
var PassthroughMiddleware = MiddlewareFunc(func(next Handler) Handler {
	return next
})

// Config for query_range middleware chain.
type Config struct {
	AlignQueriesWithStep bool                   `yaml:"align_queries_with_step"`
	ResultsCacheConfig   ResultsCacheConfig     `yaml:"results_cache"`
	CacheResults         bool                   `yaml:"cache_results"`
	MaxRetries           int                    `yaml:"max_retries"`
	ShardedQueries       bool                   `yaml:"parallelise_shardable_queries"`
	ShardAggregations    flagext.StringSliceCSV `yaml:"shard_aggregations"`
}

// RegisterFlags adds the flags required to config this to the given FlagSet.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.IntVar(&cfg.MaxRetries, "querier.max-retries-per-request", 5, "Maximum number of retries for a single request; beyond this, the downstream error is returned.")
	f.BoolVar(&cfg.AlignQueriesWithStep, "querier.align-querier-with-step", false, "Mutate incoming queries to align their start and end with their step.")
	f.BoolVar(&cfg.CacheResults, "querier.cache-results", false, "Cache query results.")
	f.BoolVar(&cfg.ShardedQueries, "querier.parallelise-shardable-queries", true, "Perform query parallelisations based on storage sharding configuration and query ASTs. This feature is supported only by the chunks storage engine.")

	cfg.ShardAggregations = []string{}
	f.Var(&cfg.ShardAggregations, "querier.shard-aggregations",
		"A comma-separated list of LogQL vector and range aggregations that should be sharded. Possible values 'quantile_over_time', 'last_over_time', 'first_over_time'.")

	cfg.ResultsCacheConfig.RegisterFlags(f)
}

// Validate validates the config.
func (cfg *Config) Validate() error {
	if cfg.CacheResults {
		if err := cfg.ResultsCacheConfig.Validate(); err != nil {
			return errors.Wrap(err, "invalid results_cache config")
		}
	}

	if len(cfg.ShardAggregations) > 0 && !cfg.ShardedQueries {
		return errors.New("shard_aggregations requires parallelise_shardable_queries=true")
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
