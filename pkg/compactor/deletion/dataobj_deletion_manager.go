package deletion

import (
	"context"
	"fmt"
	"time"

	"github.com/grafana/dskit/user"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/compactor/deletion/deletionproto"
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
	DeletionProcessInterval time.Duration `yaml:"deletion_process_interval"`
	SweepInterval           time.Duration `yaml:"sweep_interval"`
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

	requests, err := m.deleteRequestStore.GetUnprocessedShards(ctx)
	if err != nil {
		return fmt.Errorf("failed to get unprocessed delete requests from deletes store: %w", err)
	}

	if len(requests) == 0 {
		level.Debug(m.logger).Log("msg", "no unprocessed delete requests found")
		return nil
	}

	level.Info(m.logger).Log("msg", "processing dataobj delete requests", "count", len(requests))

	for _, req := range requests {
		if err := m.processDeleteRequest(ctx, req); err != nil {
			level.Error(m.logger).Log("msg", "failed to process delete request", "request_id", req.RequestID,
				"user_id", req.UserID, "err", err)
			// Continue processing other requests even if one fails
			// todo: check how to handle failures here
			continue
		}
		if err := m.deleteRequestStore.MarkShardAsProcessed(ctx, req); err != nil {
			level.Error(m.logger).Log("msg", "failed to mark delete request as processed",
				"request_id", req.RequestID, "user_id", req.UserID, "err", err)
			// Continue - tombstones were created, so this will be retried
		} else {
			m.metrics.processedRequests.Inc()
		}
	}

	return nil
}

// processDeleteRequest handles a single delete request by finding matching sections and creating tombstones.
func (m *DataobjDeletionManager) processDeleteRequest(ctx context.Context, req deletionproto.DeleteRequest) error {
	tenantCtx := user.InjectOrgID(ctx, req.UserID)
	logSelector, err := parseDeletionQuery(req.Query)
	if err != nil {
		return fmt.Errorf("failed to parse delete query: %w", err)
	}

	matchers := logSelector.Matchers()
	if len(matchers) == 0 {
		return fmt.Errorf("delete query must have at least one matcher")
	}

	level.Debug(m.logger).Log(
		"msg", "processing delete request",
		"request_id", req.RequestID,
		"user_id", req.UserID,
		"query", req.Query,
		"start", req.StartTime.Time(),
		"end", req.EndTime.Time(),
	)

	sectionsResp, err := m.metastore.Sections(tenantCtx, metastore.SectionsRequest{
		Start:    req.StartTime.Time(),
		End:      req.EndTime.Time(),
		Matchers: matchers,
	})
	if err != nil {
		return fmt.Errorf("failed to query metastore for sections: %w", err)
	}

	if len(sectionsResp.Sections) == 0 {
		level.Info(m.logger).Log(
			"msg", "no matching sections found for delete request",
			"request_id", req.RequestID,
			"user_id", req.UserID,
		)
		return nil
	}

	level.Info(m.logger).Log(
		"msg", "found sections to delete",
		"request_id", req.RequestID,
		"user_id", req.UserID,
		"count", len(sectionsResp.Sections),
	)

	sectionsByObject := make(map[string][]uint32)
	for _, section := range sectionsResp.Sections {
		sectionsByObject[section.ObjectPath] = append(
			sectionsByObject[section.ObjectPath],
			uint32(section.SectionIdx),
		)
	}

	createdAt := model.Now()
	for objectPath, sectionIndices := range sectionsByObject {
		tombstone := &deletionproto.DataObjectTombstone{
			ObjectPath:            objectPath,
			DeletedSectionIndices: sectionIndices,
			DeleteRequestID:       req.RequestID,
			CreatedAt:             createdAt,
			TenantID:              req.UserID,
		}

		if err := WriteTombstone(ctx, m.objStoreClient, tombstone); err != nil {
			return fmt.Errorf("failed to write tombstone for object %s: %w", objectPath, err)
		}

		m.metrics.tombstonesCreated.Inc()
		level.Info(m.logger).Log(
			"msg", "created tombstone",
			"request_id", req.RequestID,
			"user_id", req.UserID,
			"object", objectPath,
			"sections", fmt.Sprintf("%v", sectionIndices),
		)
	}

	level.Info(m.logger).Log(
		"msg", "completed delete request processing",
		"request_id", req.RequestID,
		"user_id", req.UserID,
		"tombstones_created", len(sectionsByObject),
		"total_sections", len(sectionsResp.Sections),
	)

	return nil
}
