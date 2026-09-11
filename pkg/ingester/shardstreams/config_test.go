package shardstreams

import (
	"flag"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestConfig_RegisterFlagsWithPrefix_Defaults(t *testing.T) {
	var cfg Config
	fs := flag.NewFlagSet("test", flag.PanicOnError)
	cfg.RegisterFlagsWithPrefix("ingester.time-sharding", fs)

	require.False(t, cfg.Enabled)
	require.Equal(t, 40*time.Minute, cfg.IgnoreRecent)
	require.Equal(t, 16, cfg.MaxOpenBuckets)
}
