package logsobj

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/scratch"
)

func TestBuilderFactory(t *testing.T) {
	bf := NewBuilderFactory(testBuilderConfig, scratch.NewMemory())
	// Can create a builder without registering metrics.
	b, err := bf.NewBuilder(nil)
	require.NoError(t, err)
	require.NotNil(t, b)
	// Can also create a builder with metrics.
	reg := prometheus.NewRegistry()
	b, err = bf.NewBuilder(reg)
	require.NoError(t, err)
	require.NotNil(t, b)
	// Should be able to gather registered metrics.
	n, err := testutil.GatherAndCount(reg)
	require.NoError(t, err)
	require.Greater(t, n, 0)
}
