package correctness

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestConfigValidate_Defaults(t *testing.T) {
	cfg := Config{
		LokiQueryEndpoints:   []string{"http://localhost:3100", "http://localhost:3100/"},
		QueryIngestersWithin: 3 * time.Hour, // required; normally inherited from Loki config
	}

	require.NoError(t, cfg.Validate())
	require.Equal(t, []string{"http://localhost:3100"}, cfg.LokiQueryEndpoints)
	require.Equal(t, DefaultQueryInterval, cfg.QueryInterval)
	require.Equal(t, 3*time.Hour, cfg.QueryIngestersWithin)
	require.Equal(t, DefaultQueryRangeMin, cfg.QueryRangeMin)
	require.Equal(t, DefaultQueryRangeMax, cfg.QueryRangeMax)
	require.Equal(t, DefaultMaxLookback, cfg.MaxLookback)
	require.Equal(t, DefaultRequestTimeout, cfg.RequestTimeout)
	require.Equal(t, DefaultCycleTimeout, cfg.CycleTimeout)
	require.Equal(t, DefaultErrorBackoffMin, cfg.ErrorBackoffMin)
	require.Equal(t, DefaultErrorBackoffMax, cfg.ErrorBackoffMax)
	require.Equal(t, DefaultLogQueryLimit, cfg.LogQueryLimit)
	require.Equal(t, DefaultNgramLength, cfg.NgramLength)
}

func TestConfigValidate_Errors(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		errPart string
	}{
		{
			name:    "missing query_ingesters_within",
			cfg:     Config{LokiQueryEndpoints: []string{"http://localhost:3100"}},
			errPart: "query_ingesters_within must be set",
		},
		{
			name:    "missing endpoints",
			cfg:     Config{},
			errPart: "loki_query_endpoints is required",
		},
		{
			name: "bad endpoint scheme",
			cfg: Config{
				LokiQueryEndpoints:   []string{"ftp://localhost:3100"},
				QueryIngestersWithin: 3 * time.Hour,
			},
			errPart: "scheme must be http or https",
		},
		{
			name: "sub-second interval",
			cfg: Config{
				LokiQueryEndpoints:   []string{"http://localhost:3100"},
				QueryIngestersWithin: 3 * time.Hour,
				QueryInterval:        500 * time.Millisecond,
			},
			errPart: "query_interval must be at least 1s",
		},
		{
			name: "max range less than min",
			cfg: Config{
				LokiQueryEndpoints:   []string{"http://localhost:3100"},
				QueryIngestersWithin: 3 * time.Hour,
				QueryRangeMin:        10 * time.Minute,
				QueryRangeMax:        5 * time.Minute,
			},
			errPart: "query_range_max must be >= query_range_min",
		},
		{
			name: "invalid ngram length",
			cfg: Config{
				LokiQueryEndpoints:   []string{"http://localhost:3100"},
				QueryIngestersWithin: 3 * time.Hour,
				NgramLength:          9,
			},
			errPart: "ngram_length must be between 1 and 8",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.errPart)
		})
	}
}

func TestParseEndpointCSV(t *testing.T) {
	result := ParseEndpointCSV(" http://a:3100/ ,https://b:3100,http://a:3100 ")
	require.Equal(t, []string{"http://a:3100", "https://b:3100"}, result)
}
