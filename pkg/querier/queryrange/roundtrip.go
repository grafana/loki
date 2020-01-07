package queryrange

import (
	"flag"
	"net/http"
	"strings"

	"github.com/cortexproject/cortex/pkg/querier/frontend"
	"github.com/cortexproject/cortex/pkg/querier/queryrange"
	"github.com/go-kit/kit/log"
	"github.com/grafana/loki/pkg/logql"
)

// Config is the configuration for the queryrange tripperware
type Config struct {
	queryrange.Config `yaml:",inline"`
}

// RegisterFlags adds the flags required to configure this flag set.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.Config.RegisterFlags(f)
}

// Stopper gracefully shutdown resources created
type Stopper interface {
	Stop() error
}

// NewTripperware returns a Tripperware configured with middlewares to align, split and cache requests.
func NewTripperware(cfg Config, log log.Logger, limits queryrange.Limits) (frontend.Tripperware, Stopper, error) {
	metricsTripperware, cache, err := queryrange.NewTripperware(cfg.Config, log, limits, lokiCodec, queryrange.PrometheusResponseExtractor)
	if err != nil {
		return nil, nil, err
	}
	logFilterTripperware, err := NewLogFilterTripperware(cfg, log, limits, lokiCodec)
	if err != nil {
		return nil, nil, err
	}
	return frontend.Tripperware(func(next http.RoundTripper) http.RoundTripper {
		metricRT := metricsTripperware(next)
		logFilterRT := logFilterTripperware(next)
		return frontend.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
			if !strings.HasSuffix(req.URL.Path, "/query_range") && !strings.HasSuffix(req.URL.Path, "/prom/query") {
				return next.RoundTrip(req)
			}
			params := req.URL.Query()
			query := params.Get("query")
			expr, err := logql.ParseExpr(query)
			if err != nil {
				return nil, err
			}
			if _, ok := expr.(logql.SampleExpr); ok {
				return metricRT.RoundTrip(req)
			}
			if logSelector, ok := expr.(logql.LogSelectorExpr); ok {
				filter, err := logSelector.Filter()
				if err != nil {
					return nil, err
				}
				if filter != nil {
					return logFilterRT.RoundTrip(req)
				}
			}
			return next.RoundTrip(req)
		})
	}), cache, nil
}

// NewLogFilterTripperware creates a new frontend tripperware responsible for handling log requests with regex.
func NewLogFilterTripperware(
	cfg Config,
	log log.Logger,
	limits queryrange.Limits,
	codec queryrange.Codec,
) (frontend.Tripperware, error) {
	queryRangeMiddleware := []queryrange.Middleware{queryrange.LimitsMiddleware(limits)}
	if cfg.SplitQueriesByInterval != 0 {
		queryRangeMiddleware = append(queryRangeMiddleware, queryrange.InstrumentMiddleware("split_by_interval"), SplitByIntervalMiddleware(cfg.SplitQueriesByInterval, limits, codec))
	}
	if cfg.MaxRetries > 0 {
		queryRangeMiddleware = append(queryRangeMiddleware, queryrange.InstrumentMiddleware("retry"), queryrange.NewRetryMiddleware(log, cfg.MaxRetries))
	}
	return frontend.Tripperware(func(next http.RoundTripper) http.RoundTripper {
		if len(queryRangeMiddleware) > 0 {
			return queryrange.NewRoundTripper(next, codec, queryRangeMiddleware...)
		}
		return next
	}), nil
}
