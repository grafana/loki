package deletion

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func TestNewDeleteRequestClientMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	drm1 := newDeleteRequestClientMetrics(reg)
	require.NotNil(t, drm1)

	drm2 := newDeleteRequestClientMetrics(reg)
	require.NotNil(t, drm2)

	drm1.deleteRequestsLookupsTotal.Inc()
	drm2.deleteRequestsLookupsTotal.Inc()
}
