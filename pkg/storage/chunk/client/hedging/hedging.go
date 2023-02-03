package hedging

import (
	"errors"
	"flag"
	"net/http"
	"sync"
	"time"

	"github.com/cristalhq/hedgedhttp"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/time/rate"
)

var (
	ErrTooManyHedgeRequests       = errors.New("too many hedge requests")
	totalHedgeRequests            prometheus.Counter
	totalRateLimitedHedgeRequests prometheus.Counter
	once                          sync.Once
)

func init() {
	initMetrics()
}

func initMetrics() {
	once = sync.Once{}
	totalHedgeRequests = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "hedged_requests_total",
		Help: "The total number of hedged requests.",
	})

	totalRateLimitedHedgeRequests = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "hedged_requests_rate_limited_total",
		Help: "The total number of hedged requests rejected via rate limiting.",
	})
}

// Config is the configuration for hedging requests.
type Config struct {
	// At is the duration after which a second request will be issued.
	At time.Duration `yaml:"at"`
	// UpTo is the maximum number of requests that will be issued.
	UpTo int `yaml:"up_to"`
	// The maximun of hedge requests allowed per second.
	MaxPerSecond int `yaml:"max_per_second"`
}

// RegisterFlags registers flags.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("", f)
}

// RegisterFlagsWithPrefix registers flags with prefix.
func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.IntVar(&cfg.UpTo, prefix+"hedge-requests-up-to", 2, "The maximum of hedge requests allowed.")
	f.DurationVar(&cfg.At, prefix+"hedge-requests-at", 0, "If set to a non-zero value a second request will be issued at the provided duration. Default is 0 (disabled)")
	f.IntVar(&cfg.MaxPerSecond, prefix+"hedge-max-per-second", 5, "The maximum of hedge requests allowed per seconds.")
}

// Client returns a hedged http client.
// The client transport will be mutated to use the hedged roundtripper.
func (cfg *Config) Client(client *http.Client) (*http.Client, error) {
	return cfg.ClientWithRegisterer(client, prometheus.DefaultRegisterer)
}

// ClientWithRegisterer returns a hedged http client with instrumentation registered to the provided registerer.
// The client transport will be mutated to use the hedged roundtripper.
func (cfg *Config) ClientWithRegisterer(client *http.Client, reg prometheus.Registerer) (*http.Client, error) {
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}
	if client == nil {
		client = http.DefaultClient
	}
	if cfg.At == 0 {
		return client, nil
	}
	var err error
	client.Transport, err = cfg.RoundTripperWithRegisterer(client.Transport, reg)
	if err != nil {
		return nil, err
	}
	return client, nil
}

// RoundTripperWithRegisterer returns a hedged roundtripper with instrumentation registered to the provided registerer.
func (cfg *Config) RoundTripperWithRegisterer(next http.RoundTripper, reg prometheus.Registerer) (http.RoundTripper, error) {
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}
	if next == nil {
		next = http.DefaultTransport
	}
	if cfg.At == 0 {
		return next, nil
	}
	// register metrics
	once.Do(func() {
		reg.MustRegister(totalHedgeRequests)
		reg.MustRegister(totalRateLimitedHedgeRequests)
	})
	return hedgedhttp.NewRoundTripper(
		cfg.At,
		cfg.UpTo,
		newLimitedHedgingRoundTripper(cfg.MaxPerSecond, next),
	)
}

// RoundTripper returns a hedged roundtripper.
func (cfg *Config) RoundTripper(next http.RoundTripper) (http.RoundTripper, error) {
	return cfg.RoundTripperWithRegisterer(next, prometheus.DefaultRegisterer)
}

type limitedHedgingRoundTripper struct {
	next    http.RoundTripper
	limiter *rate.Limiter
}

func newLimitedHedgingRoundTripper(max int, next http.RoundTripper) *limitedHedgingRoundTripper {
	return &limitedHedgingRoundTripper{
		next:    next,
		limiter: rate.NewLimiter(rate.Limit(max), max),
	}
}

func (rt *limitedHedgingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if hedgedhttp.IsHedgedRequest(req) {
		if !rt.limiter.Allow() {
			totalRateLimitedHedgeRequests.Inc()
			return nil, ErrTooManyHedgeRequests
		}
		totalHedgeRequests.Inc()
	}
	return rt.next.RoundTrip(req)
}
