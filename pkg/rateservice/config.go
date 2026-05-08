package rateservice

import (
	"errors"
	"flag"
)

const (
	DefaultWindowSecs     = 300
	DefaultBucketSizeSecs = 15
)

type Config struct {
	Enabled        bool   `yaml:"enabled"`
	WindowSecs     uint64 `yaml:"window"`
	BucketSizeSecs uint64 `yaml:"bucket_size"`
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.BoolVar(&cfg.Enabled, "rate-service.enabled", false, "Enable the rates service.")
	f.Uint64Var(&cfg.WindowSecs, "rate-service.window", DefaultWindowSecs, "The time window over which samples are collected.")
	f.Uint64Var(&cfg.BucketSizeSecs, "rate-service.bucket-size", DefaultBucketSizeSecs, "The size of each bucket, smaller buckets means better estimates.")
}

func (cfg *Config) Validate() error {
	if !cfg.Enabled {
		// Do not validate if disabled.
		return nil
	}
	if cfg.WindowSecs <= 0 {
		return errors.New("window must be positive duration")
	}
	if cfg.BucketSizeSecs <= 0 {
		return errors.New("bucket size must be positive")
	}
	if cfg.WindowSecs%cfg.BucketSizeSecs != 0 {
		return errors.New("window must be a multiple of bucket size")
	}
	return nil
}
