package frontend

import (
	"errors"
	"flag"
	"fmt"
	"time"

	"github.com/grafana/dskit/ring"

	limits_client "github.com/grafana/loki/v3/pkg/limits/client"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

// Config contains the config for an ingest-limits-frontend.
type Config struct {
	ClientConfig                   limits_client.Config  `yaml:"client_config"`
	LifecyclerConfig               ring.LifecyclerConfig `yaml:"lifecycler,omitempty"`
	NumPartitions                  int                   `yaml:"num_partitions"`
	AssignedPartitionsCacheEnabled bool                  `yaml:"assigned_partitions_cache_enabled"`
	AssignedPartitionsCacheTTL     time.Duration         `yaml:"assigned_partitions_cache_ttl"`
	AcceptedStreamsCacheEnabled    bool                  `yaml:"accepted_streams_cache_enabled"`
	AcceptedStreamsCacheTTL        time.Duration         `yaml:"accepted_streams_cache_ttl"`
	AcceptedStreamsCacheTTLJitter  time.Duration         `yaml:"accepted_streams_cache_ttl_jitter"`
	AcceptedStreamsCacheSize       int                   `yaml:"accepted_streams_cache_size"`
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.ClientConfig.RegisterFlagsWithPrefix("ingest-limits-frontend", f)
	cfg.LifecyclerConfig.RegisterFlagsWithPrefix("ingest-limits-frontend.", f, util_log.Logger)
	f.IntVar(
		&cfg.NumPartitions,
		"ingest-limits-frontend.num-partitions",
		64,
		"The number of partitions to use for the ring.",
	)
	f.BoolVar(
		&cfg.AssignedPartitionsCacheEnabled,
		"ingest-limits-frontend.assigned-partitions-cache-enabled",
		true,
		"Enable the assigned partitions cache.",
	)
	f.DurationVar(
		&cfg.AssignedPartitionsCacheTTL,
		"ingest-limits-frontend.assigned-partitions-cache-ttl",
		time.Minute,
		"The TTL for the assigned partitions cache.",
	)
	f.BoolVar(
		&cfg.AcceptedStreamsCacheEnabled,
		"ingest-limits-frontend.accepted-streams-cache-enabled",
		true,
		"Enable the accepted streams cache.",
	)
	f.DurationVar(
		&cfg.AcceptedStreamsCacheTTL,
		"ingest-limits-frontend.accepted-streams-cache-ttl",
		time.Minute,
		"The TTL for the accepted streams cache.",
	)
	f.DurationVar(
		&cfg.AcceptedStreamsCacheTTLJitter,
		"ingest-limits-frontend.accepted-streams-cache-ttl-jitter",
		15*time.Second,
		"The jitter to add to the accepted streams cache.",
	)
	f.IntVar(
		&cfg.AcceptedStreamsCacheSize,
		"ingest-limits-frontend.accepted-streams-cache-size",
		1000000,
		"The maximum number of streams that can be stored in the cache without false positives.",
	)
}

func (cfg *Config) Validate() error {
	if err := cfg.ClientConfig.GRPCClientConfig.Validate(); err != nil {
		return fmt.Errorf("invalid gRPC client config: %w", err)
	}
	if cfg.AssignedPartitionsCacheEnabled {
		if cfg.AssignedPartitionsCacheTTL <= 0 {
			return errors.New("assigned partitions cache TTL must be a positive number, or the cache must be disabled")
		}
	}
	if cfg.AcceptedStreamsCacheEnabled {
		if cfg.AcceptedStreamsCacheTTL <= 0 {
			return errors.New("accepted streams cache TTL must be a positive number, or the cache must be disabled")
		}
		if cfg.AcceptedStreamsCacheTTLJitter <= 0 {
			return errors.New("accepted streams cache TTL jitter must be a positive number, or the cache must be disabled")
		}
		if cfg.AcceptedStreamsCacheSize <= 0 {
			return errors.New("accepted streams cache size must be a positive number, or the cache must be disabled")
		}
	}
	return nil
}
