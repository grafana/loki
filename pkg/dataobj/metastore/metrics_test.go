package metastore

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

// Both statuses must exist before the first write, otherwise rate() and
// increase() report nothing for the jump from absent to 1.
func TestTableOfContentsMetrics_WriteStatusesInitialized(t *testing.T) {
	reg := prometheus.NewRegistry()
	require.NoError(t, newTableOfContentsMetrics().register(reg))

	require.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(`
	# HELP loki_dataobj_consumer_metastore_writes_total Total number of metastore writes
	# TYPE loki_dataobj_consumer_metastore_writes_total counter
	loki_dataobj_consumer_metastore_writes_total{status="failure"} 0
	loki_dataobj_consumer_metastore_writes_total{status="success"} 0
	`), "loki_dataobj_consumer_metastore_writes_total"))
}
