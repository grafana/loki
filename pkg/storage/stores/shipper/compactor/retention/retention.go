package retention

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"go.etcd.io/bbolt"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/client"
	"github.com/grafana/loki/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/series/index"
	util_log "github.com/grafana/loki/pkg/util/log"
)

var chunkBucket = []byte("chunks")

const (
	logMetricName = "logs"
	markersFolder = "markers"
	separator     = "\000"
)

var errNoChunksFound = errors.New("no chunks found in table, please check if there are really no chunks and manually drop the table or " +
	"see if there is a bug causing us to drop whole index table")

type TableMarker interface {
	// MarkForDelete marks chunks to delete for a given table and returns if it's empty or modified.
	MarkForDelete(ctx context.Context, tableName, userID string, db *bbolt.DB, logger log.Logger) (bool, bool, error)
}

type Marker struct {
	workingDirectory string
	config           config.SchemaConfig
	expiration       ExpirationChecker
	markerMetrics    *markerMetrics
	chunkClient      client.Client
}

func NewMarker(workingDirectory string, config config.SchemaConfig, expiration ExpirationChecker, chunkClient client.Client, r prometheus.Registerer) (*Marker, error) {
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
func (t *Marker) MarkForDelete(ctx context.Context, tableName, userID string, db *bbolt.DB, logger log.Logger) (bool, bool, error) {
	start := time.Now()
	status := statusSuccess
	defer func() {
		t.markerMetrics.tableProcessedDurationSeconds.WithLabelValues(tableName, status).Observe(time.Since(start).Seconds())
		level.Debug(logger).Log("msg", "finished to process table", "duration", time.Since(start))
	}()
	level.Debug(logger).Log("msg", "starting to process table")

	empty, modified, err := t.markTable(ctx, tableName, userID, db)
	if err != nil {
		status = statusFailure
		return false, false, err
	}
	return empty, modified, nil
}

func (t *Marker) markTable(ctx context.Context, tableName, userID string, db *bbolt.DB) (bool, bool, error) {
	schemaCfg, ok := schemaPeriodForTable(t.config, tableName)
	if !ok {
		return false, false, fmt.Errorf("could not find schema for table: %s", tableName)
	}

	markerWriter, err := NewMarkerStorageWriter(t.workingDirectory)
	if err != nil {
		return false, false, fmt.Errorf("failed to create marker writer: %w", err)
	}

	var empty, modified bool
	err = db.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket(local.IndexBucketName)
		if bucket == nil {
			return nil
		}

		chunkIt, err := NewChunkIndexIterator(bucket, schemaCfg)
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

		empty, modified, err = markforDelete(ctx, tableName, markerWriter, chunkIt, newSeriesCleaner(bucket, schemaCfg, tableName), t.expiration, chunkRewriter)
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
		return false, false, err
	}
	if empty {
		t.markerMetrics.tableProcessedTotal.WithLabelValues(tableName, userID, tableActionDeleted).Inc()
		return empty, true, nil
	}
	if !modified {
		t.markerMetrics.tableProcessedTotal.WithLabelValues(tableName, userID, tableActionNone).Inc()
		return empty, modified, nil
	}
	t.markerMetrics.tableProcessedTotal.WithLabelValues(tableName, userID, tableActionModified).Inc()
	return empty, modified, nil
}

func markforDelete(ctx context.Context, tableName string, marker MarkerStorageWriter, chunkIt ChunkEntryIterator, seriesCleaner SeriesCleaner, expiration ExpirationChecker, chunkRewriter *chunkRewriter) (bool, bool, error) {
	seriesMap := newUserSeriesMap()
	// tableInterval holds the interval for which the table is expected to have the chunks indexed
	tableInterval := ExtractIntervalFromTableName(tableName)
	empty := true
	modified := false
	now := model.Now()
	chunksFound := false

	for chunkIt.Next() {
		if chunkIt.Err() != nil {
			return false, false, chunkIt.Err()
		}
		chunksFound = true
		c := chunkIt.Entry()
		seriesMap.Add(c.SeriesID, c.UserID, c.Labels)

		// see if the chunk is deleted completely or partially
		if expired, nonDeletedIntervalFilters := expiration.Expired(c, now); expired {
			if len(nonDeletedIntervalFilters) > 0 {
				wroteChunks, err := chunkRewriter.rewriteChunk(ctx, c, nonDeletedIntervalFilters)
				if err != nil {
					return false, false, fmt.Errorf("failed to rewrite chunk %s for intervals %+v with error %s", c.ChunkID, nonDeletedIntervalFilters, err)
				}

				if wroteChunks {
					// we have re-written chunk to the storage so the table won't be empty and the series are still being referred.
					empty = false
					seriesMap.MarkSeriesNotDeleted(c.SeriesID, c.UserID)
				}
			}

			if err := chunkIt.Delete(); err != nil {
				return false, false, err
			}
			modified = true

			// Mark the chunk for deletion only if it is completely deleted, or this is the last table that the chunk is index in.
			// For a partially deleted chunk, if we delete the source chunk before all the tables which index it are processed then
			// the retention would fail because it would fail to find it in the storage.
			if len(nonDeletedIntervalFilters) == 0 || c.Through <= tableInterval.End {
				if err := marker.Put(c.ChunkID); err != nil {
					return false, false, err
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
					return false, false, err
				}
				modified = true
				continue
			}
		}

		empty = false
		seriesMap.MarkSeriesNotDeleted(c.SeriesID, c.UserID)
	}
	if !chunksFound {
		return false, false, errNoChunksFound
	}
	if empty {
		return true, true, nil
	}
	if ctx.Err() != nil {
		return false, false, ctx.Err()
	}

	return false, modified, seriesMap.ForEach(func(info userSeriesInfo) error {
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
	chunkClient client.Client
	tableName   string
	bucket      *bbolt.Bucket
	scfg        config.SchemaConfig

	seriesStoreSchema index.SeriesStoreSchema
}

func newChunkRewriter(chunkClient client.Client, schemaCfg config.PeriodConfig,
	tableName string, bucket *bbolt.Bucket,
) (*chunkRewriter, error) {
	seriesStoreSchema, err := index.CreateSchema(schemaCfg)
	if err != nil {
		return nil, err
	}

	return &chunkRewriter{
		chunkClient:       chunkClient,
		tableName:         tableName,
		bucket:            bucket,
		scfg:              config.SchemaConfig{Configs: []config.PeriodConfig{schemaCfg}},
		seriesStoreSchema: seriesStoreSchema,
	}, nil
}

func (c *chunkRewriter) rewriteChunk(ctx context.Context, ce ChunkEntry, intervalFilters []IntervalFilter) (bool, error) {
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

	for _, ivf := range intervalFilters {
		start := ivf.Interval.Start
		end := ivf.Interval.End

		newChunkData, err := chks[0].Data.Rebound(start, end, ivf.Filter)
		if err != nil {
			if errors.Is(err, chunk.ErrSliceNoDataInRange) {
				level.Info(util_log.Logger).Log("msg", "Rebound leaves an empty chunk", "chunk ref", string(ce.ChunkRef.ChunkID))
				// skip empty chunks
				continue
			}
			return false, err
		}

		facade, ok := newChunkData.(*chunkenc.Facade)
		if !ok {
			return false, errors.New("invalid chunk type")
		}

		newChunk := chunk.NewChunk(
			userID, chks[0].FingerprintModel(), chks[0].Metric,
			facade,
			start,
			end,
		)

		err = newChunk.Encode()
		if err != nil {
			return false, err
		}

		entries, err := c.seriesStoreSchema.GetChunkWriteEntries(newChunk.From, newChunk.Through, userID, "logs", newChunk.Metric, c.scfg.ExternalKey(newChunk.ChunkRef))
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
