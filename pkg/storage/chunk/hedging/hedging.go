package hedging

import (
	"flag"
	"net/http"
	"time"

	"github.com/cristalhq/hedgedhttp"
)

// Config is the configuration for hedging requests.
type Config struct {
	// At is the duration after which a second request will be issued.
	At time.Duration `yaml:"at"`
	// UpTo is the maximum number of requests that will be issued.
	UpTo int `yaml:"up_to"`
}

// RegisterFlags registers flags.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("", f)
}

// RegisterFlagsWithPrefix registers flags with prefix.
func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.IntVar(&cfg.UpTo, prefix+"hedge-requests-up-to", 2, "The maximun of hedge requests allowed.")
	f.DurationVar(&cfg.At, prefix+"hedge-requests-at", 0, "If set to a non-zero value a second request will be issued at the provided duration. Default is 0 (disabled)")
}

func (cfg *Config) Client(client *http.Client) *http.Client {
	if cfg.At == 0 {
		return client
	}
	return hedgedhttp.NewClient(cfg.At, cfg.UpTo, client)
}

func (cfg *Config) RoundTripper(next http.RoundTripper) http.RoundTripper {
	if cfg.At == 0 {
		return next
	}
	return hedgedhttp.NewRoundTripper(cfg.At, cfg.UpTo, next)
}
