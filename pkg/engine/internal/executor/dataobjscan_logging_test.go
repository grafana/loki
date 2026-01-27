package executor

import (
	"os"
	"testing"

	"github.com/go-kit/log"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
)

func TestDataObjScanLoggingPipeline(t *testing.T) {
	ctx := t.Context()

	// Log to stderr so the dataobj_scan_completed line is visible when running the test.
	logger := log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr))
	logger = log.With(logger, "test", t.Name())

	inner := NewBufferedPipeline()
	node := &physical.DataObjScan{
		NodeID:     ulid.Make(),
		Location:   "test/location",
		Section:    0,
		Predicates: nil,
		Projections: nil,
	}
	tenant := "test-tenant"

	wrapped := newDataObjScanLoggingPipeline(inner, node, tenant, logger)

	_, err := wrapped.Read(ctx)
	require.ErrorIs(t, err, EOF)

	wrapped.Close()
}
