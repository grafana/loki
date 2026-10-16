package logsobj

import (
	"testing"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/scratch"
)

func newTestBuilderFactory(t *testing.T, reg prometheus.Registerer, overrides TenantOverrides) *BuilderFactory {
	t.Helper()
	metrics := NewBuilderMetrics()
	require.NoError(t, metrics.Register(reg))
	bf, err := NewBuilderFactory(testBuilderConfig, scratch.NewMemory(), metrics, log.NewNopLogger(), overrides)
	require.NoError(t, err)
	return bf
}

func TestBuilderFactory_NewSorterBuilder(t *testing.T) {
	bf := newTestBuilderFactory(t, prometheus.NewRegistry(), nil)

	b, err := bf.NewSorterBuilder()
	require.NoError(t, err)
	require.NotNil(t, b)
}

func TestBuilderFactory_NewBuilder(t *testing.T) {
	bf := newTestBuilderFactory(t, prometheus.NewRegistry(), nil)

	b, err := bf.NewBuilder()
	require.NoError(t, err)
	require.NotNil(t, b)
}

func TestBuilderFactory_RegistersMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	newTestBuilderFactory(t, reg, nil)

	// Should be able to gather registered metrics.
	n, err := testutil.GatherAndCount(reg)
	require.NoError(t, err)
	require.Greater(t, n, 0)
}

func TestBuilderFactory_SetsOverridesOnBuilders(t *testing.T) {
	overrides := tenantOverrides{"tenant": {"label"}}
	bf := newTestBuilderFactory(t, prometheus.NewRegistry(), overrides)

	b, err := bf.NewBuilder()
	require.NoError(t, err)
	require.Equal(t, overrides, b.overrides)

	b, err = bf.NewSorterBuilder()
	require.NoError(t, err)
	require.Equal(t, overrides, b.overrides)
}
