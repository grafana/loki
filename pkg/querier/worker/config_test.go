package worker

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestConfigValidate_ShutdownQueryStatsPush(t *testing.T) {
	t.Run("accepts empty pushgateway URL", func(t *testing.T) {
		cfg := Config{}
		require.NoError(t, cfg.Validate())
	})

	t.Run("rejects invalid pushgateway URL", func(t *testing.T) {
		cfg := Config{ShutdownQueryStatsPushGatewayURL: "://bad-url"}
		err := cfg.Validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid querier.shutdown-query-stats-pushgateway-url")
	})

	t.Run("requires job name when URL is configured", func(t *testing.T) {
		cfg := Config{
			ShutdownQueryStatsPushGatewayURL: "http://pushgateway:9091",
			ShutdownQueryStatsPushJobName:    "",
			ShutdownQueryStatsPushTimeout:    time.Second,
		}

		err := cfg.Validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "querier.shutdown-query-stats-push-job-name must be set")
	})

	t.Run("requires positive timeout when URL is configured", func(t *testing.T) {
		cfg := Config{
			ShutdownQueryStatsPushGatewayURL: "http://pushgateway:9091",
			ShutdownQueryStatsPushJobName:    "loki-querier-shutdown",
			ShutdownQueryStatsPushTimeout:    0,
		}

		err := cfg.Validate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "querier.shutdown-query-stats-push-timeout must be greater than 0")
	})
}
