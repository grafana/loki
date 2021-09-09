package retention

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"go.etcd.io/bbolt"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/storage/chunk"
	util_log "github.com/grafana/loki/pkg/util/log"
)

var (
	bucketName  = []byte("index")
	chunkBucket = []byte("chunks")
)

const (
	logMetricName = "logs"
	markersFolder = "markers"
	separator     = "\000"
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
	chunkClient      chunk.Client
}

func NewMarker(workingDirectory string, config storage.SchemaConfig, expiration ExpirationChecker, chunkClient chunk.Client, r prometheus.Registerer) (*Marker, error) {
	if err := validatePeriods(config); err != nil {
		return nil, err
	}
	metrics := newMarkerMetrics(r)
	return &Marker{
		workingDirectory: workingDirectory,
		config:           config,
		expiration:       expiration,
		markerMetrics:    metrics,
		chunkClient:      chunkClient,
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
		chunkRewriter, err := newChunkRewriter(t.chunkClient, schemaCfg, tableName, bucket)
		if err != nil {
			return err
		}

		empty, err = markforDelete(ctx, tableName, markerWriter, chunkIt, newSeriesCleaner(bucket, schemaCfg, tableName), t.expiration, chunkRewriter)
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

func markforDelete(ctx context.Context, tableName string, marker MarkerStorageWriter, chunkIt ChunkEntryIterator, seriesCleaner SeriesCleaner, expiration ExpirationChecker, chunkRewriter *chunkRewriter) (bool, error) {
	seriesMap := newUserSeriesMap()
	// tableInterval holds the interval for which the table is expected to have the chunks indexed
	tableInterval := ExtractIntervalFromTableName(tableName)
	empty := true
	now := model.Now()

	for chunkIt.Next() {
		if chunkIt.Err() != nil {
			return false, chunkIt.Err()
		}
		c := chunkIt.Entry()
		seriesMap.Add(c.SeriesID, c.UserID, c.Labels)

		// see if the chunk is deleted completely or partially
		if expired, nonDeletedIntervals := expiration.Expired(c, now); expired {
			if len(nonDeletedIntervals) > 0 {
				wroteChunks, err := chunkRewriter.rewriteChunk(ctx, c, nonDeletedIntervals)
				if err != nil {
					return false, err
				}

				if wroteChunks {
					// we have re-written chunk to the storage so the table won't be empty and the series are still being referred.
					empty = false
					seriesMap.MarkSeriesNotDeleted(c.SeriesID, c.UserID)
				}
			}

			if err := chunkIt.Delete(); err != nil {
				return false, err
			}

			// Mark the chunk for deletion only if it is completely deleted, or this is the last table that the chunk is index in.
			// For a partially deleted chunk, if we delete the source chunk before all the tables which index it are processed then
			// the retention would fail because it would fail to find it in the storage.
			if len(nonDeletedIntervals) == 0 || c.Through <= tableInterval.End {
				if err := marker.Put(c.ChunkID); err != nil {
					return false, err
				}
			}
			continue
		}

		// The chunk is not deleted, now see if we can drop its index entry based on end time from tableInterval.
		// If chunk end time is after the end time of tableInterval, it means the chunk would also be indexed in the next table.
		// We would now check if the end time of the tableInterval is out of retention period so that
		// we can drop the chunk entry from this table without removing the chunk from the store.
		if c.Through.After(tableInterval.End) {
			if expiration.DropFromIndex(c, tableInterval.End, now) {
				if err := chunkIt.Delete(); err != nil {
					return false, err
				}
				continue
			}
		}

		empty = false
		seriesMap.MarkSeriesNotDeleted(c.SeriesID, c.UserID)
	}
	if empty {
		return true, nil
	}
	if ctx.Err() != nil {
		return false, ctx.Err()
	}

	return false, seriesMap.ForEach(func(info userSeriesInfo) error {
		if !info.isDeleted {
			return nil
		}

		return seriesCleaner.Cleanup(info.UserID(), info.lbls)
	})
}

type ChunkClient interface {
	DeleteChunk(ctx context.Context, userID, chunkID string) error
	IsChunkNotFoundErr(err error) bool
}

type Sweeper struct {
	markerProcessor MarkerProcessor
	chunkClient     ChunkClient
	sweeperMetrics  *sweeperMetrics
}

func NewSweeper(workingDir string, deleteClient ChunkClient, deleteWorkerCount int, minAgeDelete time.Duration, r prometheus.Registerer) (*Sweeper, error) {
	m := newSweeperMetrics(r)
	p, err := newMarkerStorageReader(workingDir, deleteWorkerCount, minAgeDelete, m)
	if err != nil {
		return nil, err
	}
	return &Sweeper{
		markerProcessor: p,
		chunkClient:     deleteClient,
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
		userID, err := getUserIDFromChunkID(chunkId)
		if err != nil {
			return err
		}

		err = s.chunkClient.DeleteChunk(ctx, unsafeGetString(userID), chunkIDString)
		if s.chunkClient.IsChunkNotFoundErr(err) {
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

func getUserIDFromChunkID(chunkID []byte) ([]byte, error) {
	idx := bytes.IndexByte(chunkID, '/')
	if idx <= 0 {
		return nil, fmt.Errorf("invalid chunk ID %q", chunkID)
	}

	return chunkID[:idx], nil
}

func (s *Sweeper) Stop() {
	s.markerProcessor.Stop()
}

type chunkRewriter struct {
	chunkClient chunk.Client
	tableName   string
	bucket      *bbolt.Bucket

	seriesStoreSchema chunk.SeriesStoreSchema
}

func newChunkRewriter(chunkClient chunk.Client, schemaCfg chunk.PeriodConfig,
	tableName string, bucket *bbolt.Bucket) (*chunkRewriter, error) {
	schema, err := schemaCfg.CreateSchema()
	if err != nil {
		return nil, err
	}

	seriesStoreSchema, ok := schema.(chunk.SeriesStoreSchema)
	if !ok {
		return nil, errors.New("invalid schema")
	}

	return &chunkRewriter{
		chunkClient:       chunkClient,
		tableName:         tableName,
		bucket:            bucket,
		seriesStoreSchema: seriesStoreSchema,
	}, nil
}

func (c *chunkRewriter) rewriteChunk(ctx context.Context, ce ChunkEntry, intervals []model.Interval) (bool, error) {
	userID := unsafeGetString(ce.UserID)
	chunkID := unsafeGetString(ce.ChunkID)

	chk, err := chunk.ParseExternalKey(userID, chunkID)
	if err != nil {
		return false, err
	}

	chks, err := c.chunkClient.GetChunks(ctx, []chunk.Chunk{chk})
	if err != nil {
		return false, err
	}

	if len(chks) != 1 {
		return false, fmt.Errorf("expected 1 entry for chunk %s but found %d in storage", chunkID, len(chks))
	}

	wroteChunks := false

	for _, interval := range intervals {
		newChunkData, err := chks[0].Data.Rebound(interval.Start, interval.End)
		if err != nil {
			return false, err
		}

		facade, ok := newChunkData.(*chunkenc.Facade)
		if !ok {
			return false, errors.New("invalid chunk type")
		}

		newChunk := chunk.NewChunk(
			userID, chks[0].Fingerprint, chks[0].Metric,
			facade,
			interval.Start,
			interval.End,
		)

		err = newChunk.Encode()
		if err != nil {
			return false, err
		}

		entries, err := c.seriesStoreSchema.GetChunkWriteEntries(interval.Start, interval.End, userID, "logs", newChunk.Metric, newChunk.ExternalKey())
		if err != nil {
			return false, err
		}

		uploadChunk := false

		for _, entry := range entries {
			// write an entry only if it belongs to this table
			if entry.TableName == c.tableName {
				key := entry.HashValue + separator + string(entry.RangeValue)
				if err := c.bucket.Put([]byte(key), nil); err != nil {
					return false, err
				}
				uploadChunk = true
			}
		}

		// upload chunk only if an entry was written
		if uploadChunk {
			err = c.chunkClient.PutChunks(ctx, []chunk.Chunk{newChunk})
			if err != nil {
				return false, err
			}
			wroteChunks = true
		}
	}

	return wroteChunks, nil
}
