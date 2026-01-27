package executor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
)

// dataObjScanSourcesID returns a deterministic, short ID from the scan sources
// (tenant, location, section, streamIDs) so that the same logical source produces
// the same ID.
func dataObjScanSourcesID(tenant, location string, section int, streamIDs []int64) string {
	h := sha256.New()
	_, _ = h.Write([]byte(tenant))
	_, _ = h.Write([]byte("\n"))
	_, _ = h.Write([]byte(location))
	_, _ = h.Write([]byte("\n"))
	_, _ = h.Write([]byte(fmt.Sprintf("%d", section)))
	_, _ = h.Write([]byte("\n"))
	for _, id := range streamIDs {
		_, _ = h.Write([]byte(fmt.Sprintf("%d", id)))
		_, _ = h.Write([]byte("\n"))
	}
	sum := h.Sum(nil)
	return hex.EncodeToString(sum)[:16]
}

// dataObjScanPredProjID returns a deterministic, short ID from predicates and
// projections so that the same filter/column set produces the same ID.
func dataObjScanPredProjID(preds []physical.Expression, projs []physical.ColumnExpression) string {
	h := sha256.New()
	for _, p := range preds {
		_, _ = h.Write([]byte(p.String()))
		_, _ = h.Write([]byte("\n"))
	}
	for _, p := range projs {
		_, _ = h.Write([]byte(p.String()))
		_, _ = h.Write([]byte("\n"))
	}
	sum := h.Sum(nil)
	return hex.EncodeToString(sum)[:16]
}

// dataObjScanLoggingPipeline wraps a DataObjScan pipeline to log a content-based
// scan ID and duration when the scan completes (EOF) or when the pipeline is closed.
type dataObjScanLoggingPipeline struct {
	inner      Pipeline
	sourcesID  string // from tenant, location, section, streamIDs
	predProjID string // from predicates, projections
	logger     log.Logger
	mu         sync.Mutex
	start      time.Time
	logged     bool
}

// newDataObjScanLoggingPipeline wraps inner with logging. It computes
// dataobj_scan_sources_id from node/tenant (sources and sections) and
// dataobj_scan_pred_proj_id from predicates/projections.
func newDataObjScanLoggingPipeline(inner Pipeline, node *physical.DataObjScan, tenant string, logger log.Logger) *dataObjScanLoggingPipeline {
	sourcesID := dataObjScanSourcesID(tenant, string(node.Location), node.Section, node.StreamIDs)
	predProjID := dataObjScanPredProjID(node.Predicates, node.Projections)
	return &dataObjScanLoggingPipeline{
		inner:      inner,
		sourcesID:  sourcesID,
		predProjID: predProjID,
		logger:     logger,
	}
}

// Read implements Pipeline.
func (p *dataObjScanLoggingPipeline) Read(ctx context.Context) (arrow.RecordBatch, error) {
	p.mu.Lock()
	if p.start.IsZero() {
		p.start = time.Now()
	}
	p.mu.Unlock()

	rec, err := p.inner.Read(ctx)

	p.mu.Lock()
	if !p.logged && errors.Is(err, EOF) {
		p.logged = true
		duration := time.Since(p.start).Seconds()
		_ = level.Info(p.logger).Log(
			"msg", "dataobj_scan_completed",
			"dataobj_scan_only_sources_id", p.sourcesID,
			"dataobj_scan_id", p.predProjID,
			"dataobj_scan_duration_seconds", duration,
			"experiment", "scan-cache",
		)
	}
	p.mu.Unlock()

	return rec, err
}

// Close implements Pipeline.
func (p *dataObjScanLoggingPipeline) Close() {
	p.mu.Lock()
	if !p.logged && !p.start.IsZero() {
		p.logged = true
		duration := time.Since(p.start).Seconds()
		_ = level.Info(p.logger).Log(
			"msg", "dataobj_scan_completed",
			"dataobj_scan_only_sources_id", p.sourcesID,
			"dataobj_scan_id", p.predProjID,
			"dataobj_scan_duration_seconds", duration,
			"experiment", "scan-cache",
		)
	}
	p.mu.Unlock()

	p.inner.Close()
}
