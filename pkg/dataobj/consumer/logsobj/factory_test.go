package logsobj

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/scratch"
)

func TestBuilderFactory(t *testing.T) {
	reg := prometheus.NewRegistry()
	metrics := NewBuilderMetrics()
	require.NoError(t, metrics.Register(reg))
	bf, err := NewBuilderFactory(testBuilderConfig, scratch.NewMemory(), metrics)
	require.NoError(t, err)

	// can create builder without metrics
	b, err := bf.NewSorterBuilder()
	require.NoError(t, err)
	require.NotNil(t, b)

	// can create builder with metrics
	b, err = bf.NewBuilder()
	require.NoError(t, err)
	require.NotNil(t, b)

	// Should be able to gather registered metrics.
	n, err := testutil.GatherAndCount(reg)
	require.NoError(t, err)
	require.Greater(t, n, 0)
}
