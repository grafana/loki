package retention

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/logproto"
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

type Chunk struct {
	ChunkID string
	From    model.Time
	Through model.Time
}

func (c Chunk) String() string {
	return fmt.Sprintf("ChunkID: %s", c.ChunkID)
}

type Series interface {
	SeriesID() []byte
	UserID() []byte
	Labels() labels.Labels
	Chunks() []Chunk
	Reset(seriesID, userID []byte, labels labels.Labels)
	AppendChunks(ref ...Chunk)
}

func NewSeries() Series {
	return &series{}
}

type series struct {
	seriesID, userID []byte
	labels           labels.Labels
	chunks           []Chunk
}

func (s *series) SeriesID() []byte {
	return s.seriesID
}

func (s *series) UserID() []byte {
	return s.userID
}

func (s *series) Labels() labels.Labels {
	return s.labels
}

func (s *series) Chunks() []Chunk {
	return s.chunks
}

func (s *series) Reset(seriesID, userID []byte, labels labels.Labels) {
	s.seriesID = seriesID
	s.userID = userID
	s.labels = labels
	s.chunks = s.chunks[:0]
}

func (s *series) AppendChunks(ref ...Chunk) {
	s.chunks = append(s.chunks, ref...)
}

type SeriesCallback func(series Series) (err error)

type SeriesIterator interface {
	ForEachSeries(ctx context.Context, callback SeriesCallback) error
}

type IndexCleaner interface {
	RemoveChunk(from, through model.Time, userID []byte, labels labels.Labels, chunkID string) error
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
	IndexChunk(chunkRef logproto.ChunkRef, lbls labels.Labels, sizeInKB uint32, logEntriesCount uint32) (chunkIndexed bool, err error)
}

type IndexProcessor interface {
	SeriesIterator
	chunkIndexer
	IndexCleaner
}

var errNoChunksFound = errors.New("no chunks found in table, please check if there are really no chunks and manually drop the table or " +
	"see if there is a bug causing us to drop whole index table")

type TableMarker interface {
	// FindAndMarkChunksForDeletion marks chunks to delete for a given table and returns if it's empty or modified.
	FindAndMarkChunksForDeletion(ctx context.Context, tableName, userID string, indexProcessor IndexProcessor, logger log.Logger) (bool, bool, error)

	// MarkChunksForDeletion marks the given list of chunks for deletion
	MarkChunksForDeletion(tableName string, chunks []string) error
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

// FindAndMarkChunksForDeletion finds expired chunks using the ExpirationChecker from the given table and marks them for deletion.
func (t *Marker) FindAndMarkChunksForDeletion(ctx context.Context, tableName, userID string, indexProcessor IndexProcessor, logger log.Logger) (bool, bool, error) {
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

// MarkChunksForDeletion marks the given list of chunks for deletion
func (t *Marker) MarkChunksForDeletion(tableName string, chunks []string) error {
	markerWriter, err := NewMarkerStorageWriter(t.workingDirectory)
	if err != nil {
		return fmt.Errorf("failed to create marker writer: %w", err)
	}

	for _, chunk := range chunks {
		if err := markerWriter.Put(unsafeGetBytes(chunk)); err != nil {
			return err
		}
	}

	t.markerMetrics.tableMarksCreatedTotal.WithLabelValues(tableName).Add(float64(markerWriter.Count()))
	if err := markerWriter.Close(); err != nil {
		return fmt.Errorf("failed to close marker writer: %w", err)
	}

	return nil
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

	err := indexFile.ForEachSeries(iterCtx, func(s Series) error {
		if seriesMap.HasSeries(s.SeriesID(), s.UserID()) {
			return fmt.Errorf("series should not be repeated. Series %s already seen earlier", s.SeriesID())
		}
		seriesMap.Add(s.SeriesID(), s.UserID(), s.Labels())

		chunks := s.Chunks()
		if len(chunks) == 0 {
			return nil
		}

		chunksFound = true
		seriesStart := chunks[0].From
		for i := 0; i < len(chunks); i++ {
			if chunks[i].From < seriesStart {
				seriesStart = chunks[i].From
			}
		}

		if expiration.CanSkipSeries(s.UserID(), s.Labels(), s.SeriesID(), seriesStart, tableName, now) {
			seriesMap.MarkSeriesNotDeleted(s.SeriesID(), s.UserID())
			empty = false
			return nil
		}

		// Removing logs with filter is an intensive operation. However, tracking processed series is not free either.
		// We want to only track series which have logs to be removed with filter, to skip the ones we have already processed
		// and not have too much data for tracking.
		seriesHasLogsToRemoveWithFilter := false
		for i := 0; i < len(chunks) && iterCtx.Err() == nil; i++ {
			c := chunks[i]
			// see if the chunk is deleted completely or partially
			if expired, filterFunc := expiration.Expired(s.UserID(), c, s.Labels(), s.SeriesID(), tableName, now); expired {
				linesDeleted := true // tracks whether we deleted at least some data from the chunk
				if filterFunc != nil {
					seriesHasLogsToRemoveWithFilter = true
					wroteChunks := false
					var err error
					wroteChunks, linesDeleted, err = chunkRewriter.rewriteChunk(ctx, s.UserID(), c, tableInterval, filterFunc)
					if err != nil {
						return fmt.Errorf("failed to rewrite chunk %s with error %s", c.ChunkID, err)
					}

					if wroteChunks {
						// we have re-written chunk to the storage so the table won't be empty and the series are still being referred.
						empty = false
						seriesMap.MarkSeriesNotDeleted(s.SeriesID(), s.UserID())
					}
				}

				if linesDeleted {
					modified = true

					// Mark the chunk for deletion only if it is completely deleted, or this is the last table that the chunk is index in.
					// For a partially deleted chunk, if we delete the source chunk before all the tables which index it are processed then
					// the retention would fail because it would fail to find it in the storage.
					if filterFunc == nil || c.From >= tableInterval.Start {
						if err := marker.Put(unsafeGetBytes(c.ChunkID)); err != nil {
							return err
						}
					}
					if err := indexFile.RemoveChunk(c.From, c.Through, s.UserID(), s.Labels(), c.ChunkID); err != nil {
						return fmt.Errorf("failed to remove chunk %s from index with error %s", c.ChunkID, err)
					}
					continue
				}
			}

			// The chunk is not deleted, now see if we can drop its index entry based on end time from tableInterval.
			// If chunk end time is after the end time of tableInterval, it means the chunk would also be indexed in the next table.
			// We would now check if the end time of the tableInterval is out of retention period so that
			// we can drop the chunk entry from this table without removing the chunk from the store.
			if c.Through.After(tableInterval.End) {
				if expiration.DropFromIndex(s.UserID(), c, labels.EmptyLabels(), tableInterval.End, now) {
					modified = true
					if err := indexFile.RemoveChunk(c.From, c.Through, s.UserID(), s.Labels(), c.ChunkID); err != nil {
						return fmt.Errorf("failed to remove chunk %s from index with error %s", c.ChunkID, err)
					}
					continue
				}
			}

			empty = false
			seriesMap.MarkSeriesNotDeleted(s.SeriesID(), s.UserID())
		}
		if err := iterCtx.Err(); err != nil {
			return err
		}

		if seriesHasLogsToRemoveWithFilter {
			if err := expiration.MarkSeriesAsProcessed(s.UserID(), s.SeriesID(), s.Labels(), tableName); err != nil {
				return err
			}
		}
		return nil
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
	backoffConfig   backoff.Config
}

func NewSweeper(
	workingDir string,
	deleteClient ChunkClient,
	deleteWorkerCount int,
	minAgeDelete time.Duration,
	backoffConfig backoff.Config,
	r prometheus.Registerer,
) (*Sweeper, error) {
	m := newSweeperMetrics(r)

	p, err := newMarkerStorageReader(workingDir, deleteWorkerCount, minAgeDelete, m)
	if err != nil {
		return nil, err
	}
	return &Sweeper{
		markerProcessor: p,
		chunkClient:     deleteClient,
		sweeperMetrics:  m,
		backoffConfig:   backoffConfig,
	}, nil
}

func (s *Sweeper) Start() {
	s.markerProcessor.Start(s.deleteChunk)
}

func (s *Sweeper) deleteChunk(ctx context.Context, chunkID []byte) error {
	status := statusSuccess
	start := time.Now()
	defer func() {
		s.sweeperMetrics.deleteChunkDurationSeconds.WithLabelValues(status).Observe(time.Since(start).Seconds())
	}()
	chunkIDString := unsafeGetString(chunkID)
	userID, err := getUserIDFromChunkID(chunkID)
	if err != nil {
		return err
	}

	retry := backoff.New(ctx, s.backoffConfig)
	for retry.Ongoing() {
		err = s.chunkClient.DeleteChunk(ctx, unsafeGetString(userID), chunkIDString)
		if err == nil {
			return nil
		}
		if s.chunkClient.IsChunkNotFoundErr(err) {
			status = statusNotFound
			level.Debug(util_log.Logger).Log("msg", "delete on not found chunk", "chunkID", chunkIDString)
			return nil
		}
		retry.Wait()
	}

	level.Error(util_log.Logger).Log("msg", "error deleting chunk", "chunkID", chunkIDString, "err", err)
	status = statusFailure
	return err
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
func (c *chunkRewriter) rewriteChunk(ctx context.Context, userID []byte, ce Chunk, tableInterval model.Interval, filterFunc filter.Func) (wroteChunks bool, linesDeleted bool, err error) {
	userIDStr := unsafeGetString(userID)

	chk, err := chunk.ParseExternalKey(userIDStr, ce.ChunkID)
	if err != nil {
		return false, false, err
	}

	chks, err := c.chunkClient.GetChunks(ctx, []chunk.Chunk{chk})
	if err != nil {
		return false, false, err
	}

	if len(chks) != 1 {
		return false, false, fmt.Errorf("expected 1 entry for chunk %s but found %d in storage", ce.ChunkID, len(chks))
	}

	newChunkData, err := chks[0].Data.Rebound(ce.From, ce.Through, func(ts time.Time, s string, structuredMetadata labels.Labels) bool {
		if filterFunc(ts, s, structuredMetadata) {
			linesDeleted = true
			return true
		}

		return false
	})
	if err != nil {
		if errors.Is(err, chunk.ErrSliceNoDataInRange) {
			level.Info(util_log.Logger).Log("msg", "Delete request filterFunc leaves an empty chunk", "chunk ref", ce.ChunkID)
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
		userIDStr, chks[0].FingerprintModel(), chks[0].Metric,
		facade,
		newChunkStart,
		newChunkEnd,
	)

	err = newChunk.Encode()
	if err != nil {
		return false, false, err
	}

	approxKB := math.Round(float64(newChunk.Data.UncompressedSize()) / float64(1<<10))
	uploadChunk, err := c.chunkIndexer.IndexChunk(newChunk.ChunkRef, newChunk.Metric, uint32(approxKB), uint32(newChunk.Data.Entries()))
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

		if err := os.WriteFile(filepath.Join(targetDir, marker.Name()), data, 0640); err != nil { // #nosec G306 -- this is fencing off the "other" permissions -- nosemgrep: incorrect-default-permissions
			return fmt.Errorf("write marker file: %w", err)
		}
	}

	return nil
}
