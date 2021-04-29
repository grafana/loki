package retention

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/local"
	chunk_util "github.com/cortexproject/cortex/pkg/chunk/util"
	util_log "github.com/cortexproject/cortex/pkg/util/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"go.etcd.io/bbolt"

	"github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/storage/stores/shipper/util"
	shipper_util "github.com/grafana/loki/pkg/storage/stores/shipper/util"
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

// TableCompactor can compact tables.
type TableCompactor interface {
	// Compact compacts the given tableName and output the result at the destinationPath then returns the object key.
	// The object key is expected to be already uploaded and the local path is expected to be a copy of it.
	Compact(ctx context.Context, tableName string, destinationPath string) (string, error)
}

type Marker struct {
	workingDirectory string
	config           storage.SchemaConfig
	objectClient     chunk.ObjectClient
	expiration       ExpirationChecker
	markerMetrics    *markerMetrics
	compactor        TableCompactor
}

func NewMarker(workingDirectory string, config storage.SchemaConfig, objectClient chunk.ObjectClient, expiration ExpirationChecker, compactor TableCompactor, r prometheus.Registerer) (*Marker, error) {
	if err := validatePeriods(config); err != nil {
		return nil, err
	}
	metrics := newMarkerMetrics(r)
	return &Marker{
		workingDirectory: workingDirectory,
		config:           config,
		objectClient:     objectClient,
		expiration:       expiration,
		markerMetrics:    metrics,
		compactor:        compactor,
	}, nil
}

// MarkForDelete marks all chunks expired for a given table.
func (t *Marker) MarkForDelete(ctx context.Context, tableName string) error {
	start := time.Now()
	status := statusSuccess
	defer func() {
		t.markerMetrics.tableProcessedDurationSeconds.WithLabelValues(tableName, status).Observe(time.Since(start).Seconds())
		level.Debug(util_log.Logger).Log("msg", "finished to process table", "table", tableName, "duration", time.Since(start))
	}()
	level.Debug(util_log.Logger).Log("msg", "starting to process table", "table", tableName)

	if err := t.markTable(ctx, tableName); err != nil {
		status = statusFailure
		return err
	}
	return nil
}

func (t *Marker) markTable(ctx context.Context, tableName string) error {
	tableDirectory := path.Join(t.workingDirectory, tableName)
	err := chunk_util.EnsureDirectory(tableDirectory)
	if err != nil {
		return err
	}
	defer func() {
		if err := os.RemoveAll(tableDirectory); err != nil {
			level.Warn(util_log.Logger).Log("msg", "failed to remove temporary table directory", "err", err, "path", tableDirectory)
		}
	}()

	downloadAt := filepath.Join(tableDirectory, fmt.Sprintf("retention-%d", time.Now().UnixNano()))

	objects, err := util.ListDirectory(ctx, tableName, t.objectClient)
	if err != nil {
		return err
	}
	if len(objects) != 1 {
		level.Info(util_log.Logger).Log("msg", "compacting table before applying retention", "table", tableName)
		// if there are more than one table file let's compact first.
		objectKey, err := t.compactor.Compact(ctx, tableName, downloadAt)
		if err != nil {
			level.Error(util_log.Logger).Log("msg", "failed to compact files before retention", "table", tableName, "err", err)
			return err
		}
		return t.markTableFromPath(ctx, tableName, objectKey, downloadAt)
	}

	objectKey := objects[0].Key

	if shipper_util.IsDirectory(objectKey) {
		level.Debug(util_log.Logger).Log("msg", "skipping retention no table file found", "objectKey", objectKey)
		return nil
	}

	err = shipper_util.GetFileFromStorage(ctx, t.objectClient, objectKey, downloadAt)
	if err != nil {
		level.Warn(util_log.Logger).Log("msg", "failed to download table", "err", err, "path", downloadAt, "objectKey", objectKey)
		return err
	}
	return t.markTableFromPath(ctx, tableName, objectKey, downloadAt)
}

func (t *Marker) markTableFromPath(ctx context.Context, tableName, objectKey, filepath string) error {
	db, err := shipper_util.SafeOpenBoltdbFile(filepath)
	if err != nil {
		level.Warn(util_log.Logger).Log("msg", "failed to open db", "err", err, "path", filepath)
		return err
	}

	defer func() {
		if err := db.Close(); err != nil {
			level.Warn(util_log.Logger).Log("msg", "failed to close local db", "err", err)
		}
	}()

	schemaCfg, ok := schemaPeriodForTable(t.config, tableName)
	if !ok {
		return fmt.Errorf("could not find schema for table: %s", tableName)
	}

	markerWriter, err := NewMarkerStorageWriter(t.workingDirectory)
	if err != nil {
		return fmt.Errorf("failed to create marker writer: %w", err)
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

		empty, err = markforDelete(markerWriter, chunkIt, newSeriesCleaner(bucket, schemaCfg), t.expiration)
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
		return err
	}
	// if the index is empty we can delete the index table.
	if empty {
		t.markerMetrics.tableProcessedTotal.WithLabelValues(tableName, tableActionDeleted).Inc()
		return t.objectClient.DeleteObject(ctx, objectKey)
	}
	// No chunks to delete means no changes to the remote index, we don't need to upload it.
	if markerWriter.Count() == 0 {
		t.markerMetrics.tableProcessedTotal.WithLabelValues(tableName, tableActionNone).Inc()
		return nil
	}
	t.markerMetrics.tableProcessedTotal.WithLabelValues(tableName, tableActionModified).Inc()
	return t.uploadDB(ctx, db, objectKey)
}

func (t *Marker) uploadDB(ctx context.Context, db *bbolt.DB, objectKey string) error {
	sourcePath := db.Path()
	if strings.HasSuffix(objectKey, ".gz") {
		compressedPath := fmt.Sprintf("%s.gz", sourcePath)
		err := shipper_util.CompressFile(sourcePath, compressedPath)
		if err != nil {
			return err
		}
		defer func() {
			os.Remove(compressedPath)
		}()
		sourcePath = compressedPath
	}
	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer func() {
		if err := sourceFile.Close(); err != nil {
			level.Error(util_log.Logger).Log("msg", "failed to close file", "path", sourceFile, "err", err)
		}
	}()
	return t.objectClient.PutObject(ctx, objectKey, sourceFile)
}

func markforDelete(marker MarkerStorageWriter, chunkIt ChunkEntryIterator, seriesCleaner SeriesCleaner, expiration ExpirationChecker) (bool, error) {
	seriesMap := newUserSeriesMap()
	empty := true
	for chunkIt.Next() {
		if chunkIt.Err() != nil {
			return false, chunkIt.Err()
		}
		c := chunkIt.Entry()
		if expiration.Expired(c) {
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
