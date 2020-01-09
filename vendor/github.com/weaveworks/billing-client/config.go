package billing

import (
	"flag"
	"time"
)

// Config is the config for a billing client
type Config struct {
	MaxBufferedEvents int
	RetryDelay        time.Duration
	IngesterHostPort  string
}

// RegisterFlags register the billing client flags with the main flag set
func (c *Config) RegisterFlags(f *flag.FlagSet) {
	f.IntVar(&c.MaxBufferedEvents, "billing.max-buffered-events", 1024, "Maximum number of billing events to buffer in memory")
	f.DurationVar(&c.RetryDelay, "billing.retry-delay", 500*time.Millisecond, "How often to retry sending events to the billing ingester.")
	f.StringVar(&c.IngesterHostPort, "billing.ingester", "localhost:24225", "points to the billing ingester sidecar (should be on localhost)")
}
