package metastore

import (
	"context"
	"errors"
	"fmt"
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
	# HELP loki_metastore_toc_writes_total Total number of metastore writes
	# TYPE loki_metastore_toc_writes_total counter
	loki_metastore_toc_writes_total{status="failure"} 0
	loki_metastore_toc_writes_total{status="success"} 0
	`), "loki_metastore_toc_writes_total"))
}

func TestObjectMetastoreMetrics_GetIndexesResultsInitialized(t *testing.T) {
	metrics := NewObjectMetastoreMetrics(nil)

	require.Equal(t, 4, testutil.CollectAndCount(metrics.getIndexesTotalDuration))
}

func TestGetIndexesResult(t *testing.T) {
	t.Run("returns success when there is no error", func(t *testing.T) {
		require.Equal(t, resultSuccess, getIndexesResult(nil))
	})

	t.Run("returns canceled when the error wraps context.Canceled", func(t *testing.T) {
		require.Equal(t, resultCanceled, getIndexesResult(fmt.Errorf("list: %w", context.Canceled)))
	})

	t.Run("returns deadline_exceeded when the error wraps context.DeadlineExceeded", func(t *testing.T) {
		require.Equal(t, resultDeadlineExceeded, getIndexesResult(fmt.Errorf("list: %w", context.DeadlineExceeded)))
	})

	t.Run("returns error for any other error", func(t *testing.T) {
		require.Equal(t, resultError, getIndexesResult(errors.New("boom")))
	})
}
