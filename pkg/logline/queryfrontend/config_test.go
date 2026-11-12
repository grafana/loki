package queryfrontend

import (
	"flag"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeprecatedShardPlanningFlagsStillParse(t *testing.T) {
	register := func() *flag.FlagSet {
		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		cfg := Config{}
		cfg.RegisterFlagsWithPrefix("logline-query", fs)
		return fs
	}

	t.Run("explicit values", func(t *testing.T) {
		fs := register()
		require.NoError(t, fs.Parse([]string{
			"-logline-query.shard-planning.enabled=false",
			"-logline-query.shard-planning.min-time-reduction-ratio=0.5",
		}))
	})

	t.Run("bare boolean flag", func(t *testing.T) {
		fs := register()
		require.NoError(t, fs.Parse([]string{"-logline-query.shard-planning.enabled"}))
	})
}
