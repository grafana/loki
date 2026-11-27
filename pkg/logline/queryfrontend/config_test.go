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

func TestMaxHintRangesPerRequest(t *testing.T) {
	for _, tc := range []struct {
		name    string
		args    []string
		want    int
		wantErr bool
	}{
		{"omitted", nil, 100_000, false},
		{"explicit", []string{"-logline-query.max-hint-ranges-per-request=500"}, 500, false},
		{"zero disables", []string{"-logline-query.max-hint-ranges-per-request=0"}, 0, false},
		{"negative", []string{"-logline-query.max-hint-ranges-per-request=-1"}, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var cfg Config
			fs := flag.NewFlagSet("test", flag.ContinueOnError)
			cfg.RegisterFlagsWithPrefix("logline-query", fs)
			require.NoError(t, fs.Parse(tc.args))
			if tc.wantErr {
				require.Error(t, cfg.Validate())
				return
			}
			require.NoError(t, cfg.Validate())
			require.Equal(t, tc.want, cfg.MaxHintRangesPerRequest)
		})
	}
}

func TestMetadataCacheMaxSize(t *testing.T) {
	for _, tc := range []struct {
		name      string
		args      []string
		wantBytes uint64
		wantErr   bool
	}{
		{"omitted", nil, 512 << 20, false},
		{"explicit", []string{"-logline-query.metadata-cache-max-size-mb=64"}, 64 << 20, false},
		{"zero disables", []string{"-logline-query.metadata-cache-max-size-mb=0"}, 0, false},
		{"negative", []string{"-logline-query.metadata-cache-max-size-mb=-1"}, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var cfg Config
			fs := flag.NewFlagSet("test", flag.ContinueOnError)
			cfg.RegisterFlagsWithPrefix("logline-query", fs)
			require.NoError(t, fs.Parse(tc.args))
			if tc.wantErr {
				require.Error(t, cfg.Validate())
				return
			}
			require.NoError(t, cfg.Validate())
			require.Equal(t, tc.wantBytes, cfg.MetadataCacheMaxBytes())
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
