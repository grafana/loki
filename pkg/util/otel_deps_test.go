package util_test

import (
	"testing"

	"github.com/grafana/dskit/tracing"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/stretchr/testify/require"
)

func TestOtelVersions(t *testing.T) {
	t.Setenv("JAEGER_AGENT_HOST", "localhost:1234")
	closer, err := tracing.NewOTelOrJaegerFromEnv("test-service", util_log.Logger)
	require.NoError(t, err)
	err = closer.Close()
	require.NoError(t, err)
}
