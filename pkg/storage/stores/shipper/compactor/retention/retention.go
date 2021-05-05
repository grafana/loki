package retention

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/local"
	util_log "github.com/cortexproject/cortex/pkg/util/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"go.etcd.io/bbolt"

	"github.com/grafana/loki/pkg/storage"
)

var (
	bucketName  = []byte("index")
	chunkBucket = []byte("chunks")
	empty       = []byte("-")
)

const (
	logMetricName = "logs"
	markersFolder = "markers"
)

type TableMarker interface {
	// MarkForDelete marks chunks to delete for a given table and returns if it's empty and how many marks were created.
	MarkForDelete(ctx context.Context, tableName string, db *bbolt.DB) (bool, int64, error)
}

type Marker struct {
	workingDirectory string
	config           storage.SchemaConfig
	expiration       ExpirationChecker
	markerMetrics    *markerMetrics
}

func NewMarker(workingDirectory string, config storage.SchemaConfig, expiration ExpirationChecker, r prometheus.Registerer) (*Marker, error) {
	if err := validatePeriods(config); err != nil {
		return nil, err
	}
	metrics := newMarkerMetrics(r)
	return &Marker{
		workingDirectory: workingDirectory,
		config:           config,
		expiration:       expiration,
		markerMetrics:    metrics,
	}, nil
}

// MarkForDelete marks all chunks expired for a given table.
func (t *Marker) MarkForDelete(ctx context.Context, tableName string, db *bbolt.DB) (bool, int64, error) {
	start := time.Now()
	status := statusSuccess
	defer func() {
		t.markerMetrics.tableProcessedDurationSeconds.WithLabelValues(tableName, status).Observe(time.Since(start).Seconds())
		level.Debug(util_log.Logger).Log("msg", "finished to process table", "table", tableName, "duration", time.Since(start))
	}()
	level.Debug(util_log.Logger).Log("msg", "starting to process table", "table", tableName)

	empty, markCount, err := t.markTable(ctx, tableName, db)
	if err != nil {
		status = statusFailure
		return false, 0, err
	}
	return empty, markCount, nil
}

func (t *Marker) markTable(ctx context.Context, tableName string, db *bbolt.DB) (bool, int64, error) {
	schemaCfg, ok := schemaPeriodForTable(t.config, tableName)
	if !ok {
		return false, 0, fmt.Errorf("could not find schema for table: %s", tableName)
	}

	markerWriter, err := NewMarkerStorageWriter(t.workingDirectory)
	if err != nil {
		return false, 0, fmt.Errorf("failed to create marker writer: %w", err)
	}

	var empty bool
	err = db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(bucketName)
		if bucket == nil {
			return nil
		}

		chunkIt, err := newChunkIndexIterator(bucket, schemaCfg)
		if err != nil {
			return fmt.Errorf("failed to create chunk index iterator: %w", err)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		empty, err = markforDelete(ctx, markerWriter, chunkIt, newSeriesCleaner(bucket, schemaCfg), t.expiration)
		if err != nil {
			return err
		}
		t.markerMetrics.tableMarksCreatedTotal.WithLabelValues(tableName).Add(float64(markerWriter.Count()))
		if err := markerWriter.Close(); err != nil {
			return fmt.Errorf("failed to close marker writer: %w", err)
		}
		return nil
	})
	if err != nil {
		return false, 0, err
	}
	if empty {
		t.markerMetrics.tableProcessedTotal.WithLabelValues(tableName, tableActionDeleted).Inc()
		return empty, markerWriter.Count(), nil
	}
	if markerWriter.Count() == 0 {
		t.markerMetrics.tableProcessedTotal.WithLabelValues(tableName, tableActionNone).Inc()
		return empty, markerWriter.Count(), nil
	}
	t.markerMetrics.tableProcessedTotal.WithLabelValues(tableName, tableActionModified).Inc()
	return empty, markerWriter.Count(), nil
}

func markforDelete(ctx context.Context, marker MarkerStorageWriter, chunkIt ChunkEntryIterator, seriesCleaner SeriesCleaner, expiration ExpirationChecker) (bool, error) {
	seriesMap := newUserSeriesMap()
	empty := true
	now := model.Now()
	for chunkIt.Next() {
		if chunkIt.Err() != nil {
			return false, chunkIt.Err()
		}
		c := chunkIt.Entry()
		if expiration.Expired(c, now) {
			seriesMap.Add(c.SeriesID, c.UserID)
			if err := chunkIt.Delete(); err != nil {
				return false, err
			}
			if err := marker.Put(c.ChunkID); err != nil {
				return false, err
			}
			continue
		}
		empty = false
	}
	if empty {
		return true, nil
	}
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	return false, seriesMap.ForEach(func(seriesID, userID []byte) error {
		return seriesCleaner.Cleanup(seriesID, userID)
	})
}

type DeleteClient interface {
	DeleteObject(ctx context.Context, objectKey string) error
}

type DeleteClientFunc func(ctx context.Context, objectKey string) error

func (d DeleteClientFunc) DeleteObject(ctx context.Context, objectKey string) error {
	return d(ctx, objectKey)
}

func NewDeleteClient(objectClient chunk.ObjectClient) DeleteClient {
	// filesystem encode64 keys on disk. useful for testing.
	if fs, ok := objectClient.(*local.FSObjectClient); ok {
		return DeleteClientFunc(func(ctx context.Context, objectKey string) error {
			return fs.DeleteObject(ctx, base64.StdEncoding.EncodeToString([]byte(objectKey)))
		})
	}
	return objectClient
}

type Sweeper struct {
	markerProcessor MarkerProcessor
	deleteClient    DeleteClient
	sweeperMetrics  *sweeperMetrics
}

func NewSweeper(workingDir string, deleteClient DeleteClient, deleteWorkerCount int, minAgeDelete time.Duration, r prometheus.Registerer) (*Sweeper, error) {
	m := newSweeperMetrics(r)
	p, err := newMarkerStorageReader(workingDir, deleteWorkerCount, minAgeDelete, m)
	if err != nil {
		return nil, err
	}
	return &Sweeper{
		markerProcessor: p,
		deleteClient:    deleteClient,
		sweeperMetrics:  m,
	}, nil
}

func (s *Sweeper) Start() {
	s.markerProcessor.Start(func(ctx context.Context, chunkId []byte) error {
		status := statusSuccess
		start := time.Now()
		defer func() {
			s.sweeperMetrics.deleteChunkDurationSeconds.WithLabelValues(status).Observe(time.Since(start).Seconds())
		}()
		chunkIDString := unsafeGetString(chunkId)
		err := s.deleteClient.DeleteObject(ctx, chunkIDString)
		if err == chunk.ErrStorageObjectNotFound {
			status = statusNotFound
			level.Debug(util_log.Logger).Log("msg", "delete on not found chunk", "chunkID", chunkIDString)
			return nil
		}
		if err != nil {
			level.Error(util_log.Logger).Log("msg", "error deleting chunk", "chunkID", chunkIDString, "err", err)
			status = statusFailure
		}
		return err
	})
}

func (s *Sweeper) Stop() {
	s.markerProcessor.Stop()
}
