package queryfrontend

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logline/store"
)

func TestConfigValidateDisabled(t *testing.T) {
	cfg := Config{}

	require.NoError(t, cfg.Validate())
}

func TestConfigValidateEnabled(t *testing.T) {
	t.Run("requires store minimum date", func(t *testing.T) {
		cfg := Config{Enabled: true}

		err := cfg.Validate()
		require.ErrorContains(t, err, "min_date is required")
	})

	t.Run("validates and defaults middleware", func(t *testing.T) {
		cfg := Config{
			Enabled: true,
			Store: store.Config{
				MinDate: "2026-09-01",
			},
		}

		require.NoError(t, cfg.Validate())
		require.Equal(t, defaultNgramLength, cfg.QueryFrontend.NgramLength)
		require.Equal(t, defaultMaxHintParallel, cfg.QueryFrontend.MaxHintParallel)
		require.Equal(t, defaultHintTimeout, cfg.QueryFrontend.HintTimeout)
	})
}
