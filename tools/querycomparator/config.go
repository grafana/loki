package main

import (
	"fmt"
	"os"
	"time"

	"github.com/go-kit/log"
)

// Config holds common configuration for all commands
type Config struct {
	Bucket string
	OrgID  string
	Start  string
	End    string
	Query  string
	Limit  int
}

// ParsedConfig holds parsed time values
type ParsedConfig struct {
	Config
	StartTime time.Time
	EndTime   time.Time
}

// parseTimeConfig parses start and end time strings into time.Time values
func parseTimeConfig(cfg *Config) (*ParsedConfig, error) {
	start, err := time.Parse(time.RFC3339, cfg.Start)
	if err != nil {
		return nil, fmt.Errorf("parsing start time: %w", err)
	}
	end, err := time.Parse(time.RFC3339, cfg.End)
	if err != nil {
		return nil, fmt.Errorf("parsing end time: %w", err)
	}
	if start.After(end) {
		return nil, fmt.Errorf("start time must be before end time")
	}
	return &ParsedConfig{
		Config:    *cfg,
		StartTime: start,
		EndTime:   end,
	}, nil
}

// Global variables for bucket and org ID (used by storage functions)
var (
	storageBucket      string
	orgID              string
	indexStoragePrefix string
	logger             log.Logger
)

func init() {
	logger = log.NewLogfmtLogger(os.Stderr)
	logger = log.With(logger, "ts", log.DefaultTimestampUTC)
	logger = log.With(logger, "caller", log.DefaultCaller)
}
