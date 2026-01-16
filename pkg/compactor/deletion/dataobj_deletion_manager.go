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

// DataobjDeletionManager processes delete requests for dataobjects by creating tombstone markers.
// It runs as a separate goroutine in the compactor, independent of traditional deletion processing.
type DataobjDeletionManager struct {
	cfg                DataObjDeletionConfig
	metastore          metastore.Metastore
	objStoreClient     client.ObjectClient
	deleteRequestStore DeleteRequestsStore
	logger             log.Logger
	metrics            *dataObjDeletionMetrics
}

type DataObjDeletionConfig struct {
	DeletionProcessInterval time.Duration
}

type dataObjDeletionMetrics struct {
	processedRequests prometheus.Counter
	tombstonesCreated prometheus.Counter
	processErrors     prometheus.Counter
}

func newDataObjDeletionMetrics(r prometheus.Registerer) *dataObjDeletionMetrics {
	m := &dataObjDeletionMetrics{
		processedRequests: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "loki",
			Name:      "compactor_dataobj_delete_requests_processed_total",
			Help:      "Total number of dataobj delete requests processed",
		}),
		tombstonesCreated: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "loki",
			Name:      "compactor_dataobj_tombstones_created_total",
			Help:      "Total number of dataobj tombstones created",
		}),
		processErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "loki",
			Name:      "compactor_dataobj_deletion_process_errors_total",
			Help:      "Total number of errors while processing dataobj deletions",
		}),
	}

	if r != nil {
		r.MustRegister(m.processedRequests, m.tombstonesCreated, m.processErrors)
	}

	return m
}

func NewDataobjDeletionManager(
	cfg DataObjDeletionConfig,
	metastore metastore.Metastore,
	objStoreClient client.ObjectClient,
	deleteRequestStore DeleteRequestsStore,
	logger log.Logger,
	r prometheus.Registerer,
) *DataobjDeletionManager {
	return &DataobjDeletionManager{
		cfg:                cfg,
		metastore:          metastore,
		objStoreClient:     objStoreClient,
		deleteRequestStore: deleteRequestStore,
		logger:             log.With(logger, "component", "dataobj-deletion-manager"),
		metrics:            newDataObjDeletionMetrics(r),
	}
}

// Start begins processing delete requests for dataobjects on a periodic interval.
// This method blocks until the context is cancelled.
func (m *DataobjDeletionManager) Start(ctx context.Context) {
	level.Info(m.logger).Log("msg", "starting dataobj deletion manager", "interval", m.cfg.DeletionProcessInterval)

	ticker := time.NewTicker(m.cfg.DeletionProcessInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			level.Info(m.logger).Log("msg", "dataobj deletion manager stopped")
			return
		case <-ticker.C:
			if err := m.processDeleteRequests(ctx); err != nil {
				level.Error(m.logger).Log("msg", "failed to process dataobj delete requests", "err", err)
				m.metrics.processErrors.Inc()
			}
		}
	}
}

// processDeleteRequests loads pending delete requests and creates tombstones for matching sections.
func (m *DataobjDeletionManager) processDeleteRequests(ctx context.Context) error {
	level.Debug(m.logger).Log("msg", "processing dataobj delete requests")
	// TODO --
	// 1. Load pending delete requests from deleteRequestStore
	// 2. For each delete request:
	//    a. Parse the query to extract matchers and time range
	//    b. Query metastore.Sections() to find matching sections
	//    c. Create tombstones via WriteTombstone() for each matching section
	//    d. Mark the delete request as processed
	// 3. Log metrics and completion

	return nil
}
