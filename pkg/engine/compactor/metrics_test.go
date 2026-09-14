package compactor

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func TestCoordinatorMetrics_DeleteTenant(t *testing.T) {
	m := newCoordinatorMetrics(prometheus.NewRegistry())

	// Target tenant series across every per-tenant vector.
	m.unconsolidatedBacklog.WithLabelValues("acme").Set(5)
	m.oldestBacklogLogAgeSeconds.WithLabelValues("acme").Set(1)
	m.indexesPerTenantWindow.WithLabelValues("acme").Set(2)
	m.indexesRemovedTotal.WithLabelValues("acme").Add(1)
	m.indexesAddedTotal.WithLabelValues("acme").Add(3)
	m.tasksTotal.WithLabelValues("acme").Add(4)
	m.tenantCyclesTotal.WithLabelValues("compacted", "acme").Inc()
	m.tenantLogCyclesTotal.WithLabelValues("compacted", "acme").Inc()

	// A second tenant that must survive.
	m.unconsolidatedBacklog.WithLabelValues("other").Set(7)
	m.tenantCyclesTotal.WithLabelValues("compacted", "other").Inc()

	// A non-tenant-labeled metric that must survive.
	m.cyclesTotal.WithLabelValues("compacted").Inc()

	// Baseline: every series exists before the delete. Counting series (rather
	// than reading values via ToFloat64) avoids lazily re-creating a series and
	// keeps the post-delete assertions honest.
	require.Equal(t, 2, testutil.CollectAndCount(m.unconsolidatedBacklog), "acme + other before delete")
	require.Equal(t, 1, testutil.CollectAndCount(m.oldestBacklogLogAgeSeconds))
	require.Equal(t, 1, testutil.CollectAndCount(m.indexesPerTenantWindow))
	require.Equal(t, 1, testutil.CollectAndCount(m.indexesRemovedTotal))
	require.Equal(t, 1, testutil.CollectAndCount(m.indexesAddedTotal))
	require.Equal(t, 1, testutil.CollectAndCount(m.tasksTotal))
	require.Equal(t, 2, testutil.CollectAndCount(m.tenantCyclesTotal), "acme + other before delete")
	require.Equal(t, 1, testutil.CollectAndCount(m.tenantLogCyclesTotal))
	require.Equal(t, 1, testutil.CollectAndCount(m.cyclesTotal))

	m.deleteTenant("acme")

	// Every acme-only series is gone; series shared with "other" drop only acme.
	require.Equal(t, 0, testutil.CollectAndCount(m.oldestBacklogLogAgeSeconds))
	require.Equal(t, 0, testutil.CollectAndCount(m.indexesPerTenantWindow))
	require.Equal(t, 0, testutil.CollectAndCount(m.indexesRemovedTotal))
	require.Equal(t, 0, testutil.CollectAndCount(m.indexesAddedTotal))
	require.Equal(t, 0, testutil.CollectAndCount(m.tasksTotal))
	require.Equal(t, 0, testutil.CollectAndCount(m.tenantLogCyclesTotal))

	// The other tenant and the non-tenant metric are untouched.
	require.Equal(t, 1, testutil.CollectAndCount(m.unconsolidatedBacklog), "only the surviving tenant remains")
	require.Equal(t, 7.0, testutil.ToFloat64(m.unconsolidatedBacklog.WithLabelValues("other")))
	require.Equal(t, 1, testutil.CollectAndCount(m.tenantCyclesTotal), "other tenant's cycle series survives")
	require.Equal(t, 1, testutil.CollectAndCount(m.cyclesTotal), "non-tenant-labeled metric survives")
}
