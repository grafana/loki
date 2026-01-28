package deletion

import (
	"context"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
)

// DataobjDeletionSweeper rewrites dataobjects to remove tombstoned sections (sweep phase of deletion).
// It runs as a separate goroutine in the compactor, independent of tombstone creation (mark phase).
type DataobjDeletionSweeper struct {
	cfg            DataObjDeletionConfig
	metastore      metastore.Metastore
	objStoreClient client.ObjectClient
	logger         log.Logger
	metrics        *dataObjSweepMetrics
}

type dataObjSweepMetrics struct {
	objectsSwept    prometheus.Counter
	sectionsRemoved prometheus.Counter
	bytesReclaimed  prometheus.Counter
	sweepErrors     prometheus.Counter
}

func newDataObjSweepMetrics(r prometheus.Registerer) *dataObjSweepMetrics {
	m := &dataObjSweepMetrics{
		objectsSwept: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "loki",
			Name:      "compactor_dataobj_objects_swept_total",
			Help:      "Total number of dataobjects swept (rewritten to remove deleted sections)",
		}),
		sectionsRemoved: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "loki",
			Name:      "compactor_dataobj_sections_removed_total",
			Help:      "Total number of sections removed during sweep",
		}),
		bytesReclaimed: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "loki",
			Name:      "compactor_dataobj_bytes_reclaimed_total",
			Help:      "Total bytes reclaimed by sweeping dataobjects",
		}),
		sweepErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "loki",
			Name:      "compactor_dataobj_sweep_errors_total",
			Help:      "Total number of errors during dataobj sweep",
		}),
	}

	if r != nil {
		r.MustRegister(m.objectsSwept, m.sectionsRemoved, m.bytesReclaimed, m.sweepErrors)
	}

	return m
}

func NewDataobjDeletionSweeper(
	cfg DataObjDeletionConfig,
	metastore metastore.Metastore,
	objStoreClient client.ObjectClient,
	logger log.Logger,
	r prometheus.Registerer,
) *DataobjDeletionSweeper {
	return &DataobjDeletionSweeper{
		cfg:            cfg,
		metastore:      metastore,
		objStoreClient: objStoreClient,
		logger:         log.With(logger, "component", "dataobj-deletion-sweeper"),
		metrics:        newDataObjSweepMetrics(r),
	}
}

// Start begins sweeping dataobjects on a periodic interval.
// This method blocks until the context is cancelled.
func (s *DataobjDeletionSweeper) Start(ctx context.Context) {
	level.Info(s.logger).Log("msg", "starting dataobj deletion sweeper", "interval", s.cfg.SweepInterval)

	ticker := time.NewTicker(s.cfg.SweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			level.Info(s.logger).Log("msg", "dataobj deletion sweeper stopped")
			return
		case <-ticker.C:
			if err := s.sweep(ctx); err != nil {
				level.Error(s.logger).Log("msg", "failed to sweep dataobjects", "err", err)
				s.metrics.sweepErrors.Inc()
			}
		}
	}
}

// sweep performs a single sweep run.
func (s *DataobjDeletionSweeper) sweep(_ context.Context) error {
	level.Debug(s.logger).Log("msg", "starting sweep run")

	// TODO: Implement actual sweep logic
	// 1. List tombstones
	// 2. Group by object
	// 3. Rewrite objects without tombstoned sections
	// 4. Update TOC and indexes
	// 5. Cleanup old objects and tombstones

	level.Debug(s.logger).Log("msg", "sweep run complete")
	return nil
}
