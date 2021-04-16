package retention

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/cortexproject/cortex/pkg/chunk"
	chunk_util "github.com/cortexproject/cortex/pkg/chunk/util"
	util_log "github.com/cortexproject/cortex/pkg/util/log"
	"github.com/go-kit/kit/log/level"
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
	delimiter     = "/"
	markersFolder = "markers"
)

type Marker struct {
	workingDirectory string
	config           storage.SchemaConfig
	objectClient     chunk.ObjectClient
	expiration       ExpirationChecker
}

func NewMarker(workingDirectory string, config storage.SchemaConfig, objectClient chunk.ObjectClient, expiration ExpirationChecker) *Marker {
	return &Marker{
		workingDirectory: workingDirectory,
		config:           config,
		objectClient:     objectClient,
	}
}

func (t *Marker) MarkTableForDelete(ctx context.Context, tableName string) error {
	objects, err := util.ListDirectory(ctx, tableName, t.objectClient)
	if err != nil {
		return err
	}

	if len(objects) != 1 {
		// todo(1): in the future we would want to support more tables so that we can apply retention below 1d.
		// for simplicity and to avoid conflict with compactor we'll support only compacted db file.
		// Possibly we should apply retention right before the compactor upload compacted db.

		// todo(2): Depending on the retention rules we should be able to skip tables.
		// For instance if there isn't a retention rules below 1 week, then we can skip the first 7 tables.
		level.Debug(util_log.Logger).Log("msg", "skipping retention for non-compacted table", "name", tableName)
		return nil
	}
	tableKey := objects[0].Key

	if shipper_util.IsDirectory(tableKey) {
		level.Debug(util_log.Logger).Log("msg", "skipping retention no table file found", "key", tableKey)
		return nil
	}

	tableDirectory := path.Join(t.workingDirectory, tableName)
	err = chunk_util.EnsureDirectory(tableDirectory)
	if err != nil {
		return err
	}

	downloadAt := filepath.Join(t.workingDirectory, tableName)

	err = shipper_util.GetFileFromStorage(ctx, t.objectClient, tableKey, downloadAt)
	if err != nil {
		return err
	}

	db, err := shipper_util.SafeOpenBoltdbFile(downloadAt)
	if err != nil {
		return err
	}

	defer func() {
		if err := os.Remove(db.Path()); err != nil {
			level.Warn(util_log.Logger).Log("msg", "failed to removed downloaded db", "err", err)
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

		seriesCleaner := newSeriesCleaner(bucket, schemaCfg)
		empty, err = markforDelete(markerWriter, chunkIt, seriesCleaner, t.expiration)
		if err != nil {
			return err
		}
		if err := markerWriter.Close(); err != nil {
			return fmt.Errorf("failed to close marker writer: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err := db.Close(); err != nil {
		return err
	}
	// either delete if all entries are removed or upload the new file.
	if empty {
		return t.objectClient.DeleteObject(ctx, tableName+delimiter)
	}
	// No chunks to delete means no changes to the remote index, we don't need to upload it.
	if markerWriter.Count() == 0 {
		return nil
	}
	return t.uploadDB(ctx, db, tableKey)
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
	var empty bool
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
