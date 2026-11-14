package queryfrontend

import (
	"flag"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMaxConjunctionsDefaultsAndOptIn(t *testing.T) {
	var cfg Config
	require.NoError(t, cfg.Validate())
	require.Equal(t, 1, cfg.MaxConjunctions)

	for _, tc := range []struct {
		name string
		args []string
		want int
	}{
		{"omitted", nil, 1},
		{"zero", []string{"-logline-query.max-conjunctions=0"}, 1},
		{"explicit opt-in", []string{"-logline-query.max-conjunctions=4"}, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var cfg Config
			fs := flag.NewFlagSet("test", flag.ContinueOnError)
			cfg.RegisterFlagsWithPrefix("logline-query", fs)
			require.NoError(t, fs.Parse(tc.args))
			require.NoError(t, cfg.Validate())
			require.Equal(t, tc.want, cfg.MaxConjunctions)
		})
	}
}

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
