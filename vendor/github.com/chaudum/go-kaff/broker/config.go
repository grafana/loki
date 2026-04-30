// Package broker provides the public library API for go-kaff.
package broker

import (
	"log/slog"
	"time"
)

// Config controls how the Broker is set up.
type Config struct {
	// ListenAddr is the TCP address to bind.  Use "127.0.0.1:0" (the default)
	// to have the OS pick a free port — ideal for tests.
	ListenAddr string

	// AutoCreateTopics controls whether an unknown topic is automatically
	// created when it first appears in a Metadata request.
	// Default: true.
	AutoCreateTopics bool

	// DefaultNumPartitions is the partition count used when a topic is
	// auto-created.  Default: 1.
	DefaultNumPartitions int32

	// MaxBytesPerPartition is a soft cap on per-partition memory usage.
	// 0 means unlimited.  Default: 0.
	MaxBytesPerPartition int64

	// SessionTimeout is the default consumer group session timeout.
	// Default: 30 s.
	SessionTimeout time.Duration

	// Logger is the structured logger used by the broker.  If nil,
	// slog.Default() is used.
	Logger *slog.Logger

	// Metrics receives operational observations from the broker.
	// If nil, a no-op implementation (NoopMetrics) is used.
	Metrics Metrics
}

// TopicConfig controls how a pre-created topic is configured.
type TopicConfig struct {
	// NumPartitions is the number of partitions.  Default: 1.
	NumPartitions int32
	// RetentionBytes is a soft cap on per-partition memory.  0 = unlimited.
	RetentionBytes int64
}

// defaults fills in zero-value fields with sensible defaults.
func (c *Config) defaults() {
	if c.ListenAddr == "" {
		c.ListenAddr = "127.0.0.1:0"
	}
	if c.DefaultNumPartitions == 0 {
		c.DefaultNumPartitions = 1
	}
	if c.SessionTimeout == 0 {
		c.SessionTimeout = 30 * time.Second
	}
	// AutoCreateTopics defaults to true when the zero value of bool is false;
	// use a pointer approach to distinguish "not set" from "explicitly false".
	// For simplicity we default to true via the New() constructor instead.
}
