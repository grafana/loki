package indexshipper

import (
	"flag"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPostingsCacheFlags(t *testing.T) {
	var cfg Config
	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	cfg.RegisterFlags(flags)

	require.Equal(t, time.Hour, cfg.PostingsCache.DefaultValidity)
	require.NoError(t, flags.Parse([]string{
		"-shipper.postings-cache.default-validity=2m",
		"-shipper.postings-cache.background.write-back-concurrency=3",
	}))
	require.Equal(t, 2*time.Minute, cfg.PostingsCache.DefaultValidity)
	require.Equal(t, 3, cfg.PostingsCache.Background.WriteBackGoroutines)
}
