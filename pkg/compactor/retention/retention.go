package retention

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	chunk_util "github.com/grafana/loki/v3/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/filter"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

var chunkBucket = []byte("chunks")

const (
	MarkersFolder = "markers"
)

type ChunkRef struct {
	UserID   []byte
	SeriesID []byte
	ChunkID  []byte
	From     model.Time
	Through  model.Time
}

func (c ChunkRef) String() string {
	return fmt.Sprintf("UserID: %s , SeriesID: %s , Time: [%s,%s]", c.UserID, c.SeriesID, c.From, c.Through)
}

type ChunkEntry struct {
	ChunkRef
	Labels labels.Labels
}

type ChunkEntryCallback func(ChunkEntry) (deleteChunk bool, err error)

type ChunkIterator interface {
	ForEachChunk(ctx context.Context, callback ChunkEntryCallback) error
}

type SeriesCleaner interface {
	// CleanupSeries is for cleaning up the series that do have any chunks left in the index.
	// It would only be called for the series that have all their chunks deleted without adding new ones.
	CleanupSeries(userID []byte, lbls labels.Labels) error
}

type chunkIndexer interface {
	// IndexChunk is for indexing a new chunk that was built from an existing chunk while processing delete requests.
	// It should return true if the chunk was indexed else false if not.
	// The implementation could skip indexing a chunk due to it not belonging to the table.
	// ToDo(Sandeep): We already have a check in the caller of IndexChunk to check if the chunk belongs to the table.
	// See if we can drop the redundant check in the underlying implementation.
	IndexChunk(chunk chunk.Chunk) (chunkIndexed bool, err error)
}

type IndexProcessor interface {
	ChunkIterator
	chunkIndexer
	SeriesCleaner
}

var errNoChunksFound = errors.New("no chunks found in table, please check if there are really no chunks and manually drop the table or " +
	"see if there is a bug causing us to drop whole index table")

type TableMarker interface {
	// MarkForDelete marks chunks to delete for a given table and returns if it's empty or modified.
	MarkForDelete(ctx context.Context, tableName, userID string, indexProcessor IndexProcessor, logger log.Logger) (bool, bool, error)
}

type Marker struct {
	workingDirectory string
	expiration       ExpirationChecker
	markerMetrics    *markerMetrics
	chunkClient      client.Client
	markTimeout      time.Duration
}

func NewMarker(workingDirectory string, expiration ExpirationChecker, markTimeout time.Duration, chunkClient client.Client, r prometheus.Registerer) (*Marker, error) {
	return &Marker{
		workingDirectory: workingDirectory,
		expiration:       expiration,
		markerMetrics:    newMarkerMetrics(r),
		chunkClient:      chunkClient,
		markTimeout:      markTimeout,
	}, nil
}

// MarkForDelete marks all chunks expired for a given table.
func (t *Marker) MarkForDelete(ctx context.Context, tableName, userID string, indexProcessor IndexProcessor, logger log.Logger) (bool, bool, error) {
	start := time.Now()
	status := statusSuccess
	defer func() {
		t.markerMetrics.tableProcessedDurationSeconds.WithLabelValues(tableName, status).Observe(time.Since(start).Seconds())
		level.Debug(logger).Log("msg", "finished to process table", "duration", time.Since(start))
	}()
	level.Debug(logger).Log("msg", "starting to process table")

	empty, modified, err := t.markTable(ctx, tableName, userID, indexProcessor, logger)
	if err != nil {
		status = statusFailure
		return false, false, err
	}
	return empty, modified, nil
}

func (t *Marker) markTable(ctx context.Context, tableName, userID string, indexProcessor IndexProcessor, logger log.Logger) (bool, bool, error) {
	markerWriter, err := NewMarkerStorageWriter(t.workingDirectory)
	if err != nil {
		return false, false, fmt.Errorf("failed to create marker writer: %w", err)
	}

	if ctx.Err() != nil {
		return false, false, ctx.Err()
	}

	chunkRewriter := newChunkRewriter(t.chunkClient, tableName, indexProcessor)

	empty, modified, err := markForDelete(ctx, t.markTimeout, tableName, markerWriter, indexProcessor, t.expiration, chunkRewriter, logger)
	if err != nil {
		return false, false, err
	}

	t.markerMetrics.tableMarksCreatedTotal.WithLabelValues(tableName).Add(float64(markerWriter.Count()))
	if err := markerWriter.Close(); err != nil {
		return false, false, fmt.Errorf("failed to close marker writer: %w", err)
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

func markForDelete(
	ctx context.Context,
	timeout time.Duration,
	tableName string,
	marker MarkerStorageWriter,
	indexFile IndexProcessor,
	expiration ExpirationChecker,
	chunkRewriter *chunkRewriter,
	logger log.Logger,
) (bool, bool, error) {
	seriesMap := newUserSeriesMap()
	// tableInterval holds the interval for which the table is expected to have the chunks indexed
	tableInterval := ExtractIntervalFromTableName(tableName)
	empty := true
	modified := false
	now := model.Now()
	chunksFound := false

	// This is a fresh context so we know when deletes timeout vs something going
	// wrong with the other context
	iterCtx, cancel := ctxForTimeout(timeout)
	defer cancel()

	err := indexFile.ForEachChunk(iterCtx, func(c ChunkEntry) (bool, error) {
		chunksFound = true
		seriesMap.Add(c.SeriesID, c.UserID, c.Labels)

		// see if the chunk is deleted completely or partially
		if expired, filterFunc := expiration.Expired(c, now); expired {
			linesDeleted := true // tracks whether we deleted at least some data from the chunk
			if filterFunc != nil {
				wroteChunks := false
				var err error
				wroteChunks, linesDeleted, err = chunkRewriter.rewriteChunk(ctx, c, tableInterval, filterFunc)
				if err != nil {
					return false, fmt.Errorf("failed to rewrite chunk %s with error %s", c.ChunkID, err)
				}

				if wroteChunks {
					// we have re-written chunk to the storage so the table won't be empty and the series are still being referred.
					empty = false
					seriesMap.MarkSeriesNotDeleted(c.SeriesID, c.UserID)
				}
			}

			if linesDeleted {
				modified = true

				// Mark the chunk for deletion only if it is completely deleted, or this is the last table that the chunk is index in.
				// For a partially deleted chunk, if we delete the source chunk before all the tables which index it are processed then
				// the retention would fail because it would fail to find it in the storage.
				if filterFunc == nil || c.From >= tableInterval.Start {
					if err := marker.Put(c.ChunkID); err != nil {
						return false, err
					}
				}
				return true, nil
			}
		}

		// The chunk is not deleted, now see if we can drop its index entry based on end time from tableInterval.
		// If chunk end time is after the end time of tableInterval, it means the chunk would also be indexed in the next table.
		// We would now check if the end time of the tableInterval is out of retention period so that
		// we can drop the chunk entry from this table without removing the chunk from the store.
		if c.Through.After(tableInterval.End) {
			if expiration.DropFromIndex(c, tableInterval.End, now) {
				modified = true
				return true, nil
			}
		}

		empty = false
		seriesMap.MarkSeriesNotDeleted(c.SeriesID, c.UserID)
		return false, nil
	})
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) && errors.Is(iterCtx.Err(), context.DeadlineExceeded) {
			// Deletes timed out. Don't return an error so compaction can continue and deletes can be retried
			level.Warn(logger).Log("msg", "Timed out while running delete")
			expiration.MarkPhaseTimedOut()
		} else {
			return false, false, err
		}
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

		return indexFile.CleanupSeries(info.UserID(), info.lbls)
	})
}

func ctxForTimeout(t time.Duration) (context.Context, context.CancelFunc) {
	if t == 0 {
		return context.Background(), func() {}
	}
	return context.WithTimeout(context.Background(), t)
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
	chunkClient  client.Client
	tableName    string
	chunkIndexer chunkIndexer
}

func newChunkRewriter(chunkClient client.Client, tableName string, chunkIndexer chunkIndexer) *chunkRewriter {
	return &chunkRewriter{
		chunkClient:  chunkClient,
		tableName:    tableName,
		chunkIndexer: chunkIndexer,
	}
}

// rewriteChunk rewrites a chunk after filtering out logs using filterFunc.
// It first builds a newChunk using filterFunc.
// If the newChunk is same as the original chunk then there is nothing to do here, wroteChunks and linesDeleted both would be false.
// If the newChunk is different, linesDeleted would be true.
// The newChunk is indexed and uploaded only if it belongs to the current index table being processed,
// the status of which is set to wroteChunks.
func (c *chunkRewriter) rewriteChunk(ctx context.Context, ce ChunkEntry, tableInterval model.Interval, filterFunc filter.Func) (wroteChunks bool, linesDeleted bool, err error) {
	userID := unsafeGetString(ce.UserID)
	chunkID := unsafeGetString(ce.ChunkID)

	chk, err := chunk.ParseExternalKey(userID, chunkID)
	if err != nil {
		return false, false, err
	}

	chks, err := c.chunkClient.GetChunks(ctx, []chunk.Chunk{chk})
	if err != nil {
		return false, false, err
	}

	if len(chks) != 1 {
		return false, false, fmt.Errorf("expected 1 entry for chunk %s but found %d in storage", chunkID, len(chks))
	}

	newChunkData, err := chks[0].Data.Rebound(ce.From, ce.Through, func(ts time.Time, s string, structuredMetadata ...labels.Label) bool {
		if filterFunc(ts, s, structuredMetadata...) {
			linesDeleted = true
			return true
		}

		return false
	})
	if err != nil {
		if errors.Is(err, chunk.ErrSliceNoDataInRange) {
			level.Info(util_log.Logger).Log("msg", "Delete request filterFunc leaves an empty chunk", "chunk ref", string(ce.ChunkRef.ChunkID))
			return false, true, nil
		}
		return false, false, err
	}

	// if no lines were deleted then there is nothing to do
	if !linesDeleted {
		return false, false, nil
	}

	facade, ok := newChunkData.(*chunkenc.Facade)
	if !ok {
		return false, false, errors.New("invalid chunk type")
	}

	newChunkStart, newChunkEnd := util.RoundToMilliseconds(facade.Bounds())

	// new chunk is out of range for this table so don't upload and index it
	if newChunkStart > tableInterval.End || newChunkEnd < tableInterval.Start {
		return false, linesDeleted, nil
	}

	newChunk := chunk.NewChunk(
		userID, chks[0].FingerprintModel(), chks[0].Metric,
		facade,
		newChunkStart,
		newChunkEnd,
	)

	err = newChunk.Encode()
	if err != nil {
		return false, false, err
	}

	uploadChunk, err := c.chunkIndexer.IndexChunk(newChunk)
	if err != nil {
		return false, false, err
	}

	// upload chunk only if an entry was written
	if uploadChunk {
		err = c.chunkClient.PutChunks(ctx, []chunk.Chunk{newChunk})
		if err != nil {
			return false, false, err
		}
		wroteChunks = true
	}

	return wroteChunks, linesDeleted, nil
}

// CopyMarkers checks for markers in the src dir and copies them to the dst.
func CopyMarkers(src string, dst string) error {
	markersDir := filepath.Join(src, MarkersFolder)
	info, err := os.Stat(markersDir)
	if err != nil {
		if os.IsNotExist(err) {
			// nothing to migrate
			return nil
		}

		return err
	}

	if !info.IsDir() {
		return nil
	}

	markers, err := os.ReadDir(markersDir)
	if err != nil {
		return fmt.Errorf("read markers dir: %w", err)
	}

	targetDir := filepath.Join(dst, MarkersFolder)
	if err := chunk_util.EnsureDirectory(targetDir); err != nil {
		return fmt.Errorf("ensure target markers dir: %w", err)
	}

	level.Info(util_log.Logger).Log("msg", fmt.Sprintf("found markers in retention dir %s, moving them to period specific dir: %s", markersDir, targetDir))
	for _, marker := range markers {
		if marker.IsDir() {
			continue
		}

		data, err := os.ReadFile(filepath.Join(markersDir, marker.Name()))
		if err != nil {
			return fmt.Errorf("read marker file: %w", err)
		}

		if err := os.WriteFile(filepath.Join(targetDir, marker.Name()), data, 0o666); err != nil {
			return fmt.Errorf("write marker file: %w", err)
		}
	}

	return nil
}
