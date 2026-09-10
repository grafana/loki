package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore"
	"go.uber.org/atomic"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/logline/format"
)

var errInvalidMeta = errors.New("invalid meta")

// Snapshot is an immutable point-in-time view of all indexes in storage.
// It is replaced atomically on each Poll. Consumers that need to check
// multiple properties should grab the snapshot once and query it directly
// rather than making repeated calls through the Store.
type Snapshot struct {
	all    []Meta
	allIDs map[string]struct{}
	// compactedAt maps source ID → CreatedAt of the newest merged index covering it.
	// Used to measure the grace period before source deletion.
	compactedAt map[string]time.Time
	active      []Meta
}

// All returns every index in the snapshot. The returned slice is a copy.
func (s *Snapshot) All() []Meta {
	if s == nil {
		return []Meta{}
	}
	result := make([]Meta, len(s.all))
	copy(result, s.all)
	return result
}

// Active returns all non-compacted indexes. The returned slice is a copy.
func (s *Snapshot) Active() []Meta {
	if s == nil {
		return []Meta{}
	}
	result := make([]Meta, len(s.active))
	copy(result, s.active)
	return result
}

// IsCompacted returns true if the given index ID appears as a compacted
// source (i.e., it is covered by a merged index).
func (s *Snapshot) IsCompacted(id string) bool {
	if s == nil {
		return false
	}
	_, ok := s.compactedAt[id]
	return ok
}

// Contains reports whether the given index ID exists in the snapshot.
func (s *Snapshot) Contains(id string) bool {
	if s == nil {
		return false
	}
	_, ok := s.allIDs[id]
	return ok
}

// EligibleForDeletion returns Metas that are safe to delete given the
// provided cutoff times:
//   - A compacted source index is eligible when the merged index covering it
//     was created before graceCutoff.
//   - An active index is eligible when its MaxLogTs is before retentionCutoff.
//
// In both cases, merged indexes whose compacted sources still exist are
// excluded (canDelete check).
func (s *Snapshot) EligibleForDeletion(retentionCutoff, graceCutoff time.Time) []Meta {
	if s == nil {
		return []Meta{}
	}
	var result []Meta
	for _, m := range s.all {
		if _, ok := s.canDelete(m); !ok {
			continue
		}
		if compactedTime, isCompacted := s.compactedAt[m.ID()]; isCompacted {
			if compactedTime.Before(graceCutoff) {
				result = append(result, m)
			}
		} else {
			if m.MaxLogTs.Before(retentionCutoff) {
				result = append(result, m)
			}
		}
	}
	if result == nil {
		return []Meta{}
	}
	return result
}

// canDelete reports whether m can be safely removed from storage.
// A merged index (non-empty CompactedFrom) is not deletable while any of its
// compacted sources still exist — deleting it would un-compact those sources.
func (s *Snapshot) canDelete(m Meta) (string, bool) {
	if len(m.CompactedFrom) == 0 {
		return "", true
	}
	for _, sourceID := range m.CompactedFrom {
		if _, exists := s.allIDs[sourceID]; exists {
			return sourceID, false
		}
	}
	return "", true
}

// Store manages read/write access to logline index files in object storage.
type Store struct {
	bucket     objstore.Bucket
	cfg        Config
	logger     log.Logger
	metrics    *Metrics
	snapshot   atomic.Pointer[Snapshot]
	pollCh     chan *Snapshot
	knownMetas map[string]Meta // cache of already-fetched metas; keyed by Meta.ID() ("date/storageID")
}

// NewStore creates a Store with a pre-existing bucket. Validates cfg and applies defaults.
func NewStore(bucket objstore.Bucket, cfg Config, logger log.Logger, reg prometheus.Registerer) (*Store, error) {
	if bucket == nil {
		return nil, fmt.Errorf("bucket cannot be nil")
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid store config: %w", err)
	}
	return newStore(bucket, cfg, logger, reg)
}

func newStore(bucket objstore.Bucket, cfg Config, logger log.Logger, reg prometheus.Registerer) (*Store, error) {
	if logger == nil {
		logger = log.NewNopLogger()
	}
	s := &Store{
		bucket:     bucket,
		cfg:        cfg,
		logger:     logger,
		metrics:    NewMetrics(reg),
		pollCh:     make(chan *Snapshot, 1),
		knownMetas: make(map[string]Meta),
	}
	// Initialize with empty snapshot so Indexes() never races on nil pointer.
	s.snapshot.Store(&Snapshot{allIDs: map[string]struct{}{}, compactedAt: map[string]time.Time{}})
	return s, nil
}

// IndexExists checks whether an index with the given meta already exists
// in object storage. Checks for the meta.json commit marker, which is
// written last during PutIndex — its presence means the index is complete.
func (s *Store) IndexExists(ctx context.Context, meta Meta) (bool, error) {
	exists, err := s.bucket.Exists(ctx, meta.MetaPath())
	if err != nil && s.bucket.IsObjNotFoundErr(err) {
		return false, nil
	}
	return exists, err
}

// GetIndex downloads the raw index data for the given ID ("date/id").
// The caller must close the returned ReadCloser.
// Returns an error if the ID format is invalid or the object does not exist.
func (s *Store) GetIndex(ctx context.Context, id string) (io.ReadCloser, error) {
	meta, err := metaFromID(id)
	if err != nil {
		return nil, err
	}
	rc, err := s.bucket.Get(ctx, meta.IndexPath())
	if err != nil {
		return nil, fmt.Errorf("get index %s: %w", id, err)
	}
	return rc, nil
}

// GetIndexReaderAt returns an io.ReaderAt backed by range reads against
// object storage. Each ReadAt call issues a single GetRange request,
// avoiding the need to download the full index into memory.
func (s *Store) GetIndexReaderAt(ctx context.Context, meta Meta) io.ReaderAt {
	return NewBucketReaderAt(ctx, s.bucket, meta.IndexPath())
}

// GetIndexReadAheadReaderAt returns an io.ReaderAt that prefetches chunks
// from object storage, amortizing round-trip latency across many small reads.
// fileSize must be the exact size of the index data file. chunkSize controls
// the prefetch granularity; 0 uses the default (32 MB).
func (s *Store) GetIndexReadAheadReaderAt(ctx context.Context, meta Meta, chunkSize int) io.ReaderAt {
	return NewReadAheadReaderAt(ctx, s.bucket, meta.IndexPath(), meta.SizeBytes, chunkSize)
}

// GetMeta fetches and parses the meta.json for the given ID ("date/id").
// Date and StorageID are always overwritten from the path.
// Returns an error if the ID format is invalid, the object does not exist, or
// the JSON is malformed.
func (s *Store) GetMeta(ctx context.Context, id string) (Meta, error) {
	m, err := s.loadMeta(ctx, id)
	if err != nil {
		return Meta{}, fmt.Errorf("get meta %s: %w", id, err)
	}
	return m, nil
}

// PutIndex uploads an index file and its metadata to object storage.
// CreatedAt is set automatically. The caller must populate Hash, SizeBytes,
// and IndexHeader before calling (Hash and SizeBytes via Meta.SetFileInfo). For streaming
// producers that don't have the full bytes on disk, use PutIndexStreaming.
func (s *Store) PutIndex(ctx context.Context, indexData io.Reader, meta Meta) error {
	if err := meta.Validate(); err != nil {
		return fmt.Errorf("invalid meta: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	meta.CreatedAt = time.Now().UTC()
	metaBytes, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("failed to marshal meta: %w", err)
	}

	// Upload index data first — meta acts as the commit marker.
	// If index upload fails, meta is never written and the index is invisible to Poll.
	if err := s.bucket.Upload(ctx, meta.IndexPath(), indexData); err != nil {
		return fmt.Errorf("failed to upload index data: %w", err)
	}

	if err := s.bucket.Upload(ctx, meta.MetaPath(), bytes.NewReader(metaBytes)); err != nil {
		return fmt.Errorf("failed to upload meta.json: %w", err)
	}

	level.Info(s.logger).Log(
		"msg", "wrote index",
		"path", meta.IndexPath(),
		"min_time", meta.MinLogTs.Format(time.RFC3339),
		"max_time", meta.MaxLogTs.Format(time.RFC3339),
		"compacted_from", strings.Join(meta.CompactedFrom, ","),
	)
	return nil
}

// PutIndexStreaming uploads an index whose bytes are produced by write, then
// uploads its meta.json. The caller's write callback receives an io.Writer
// whose bytes are streamed directly to object storage; no local file is
// created. The store populates all three post-write fields on meta from the
// stream and the callback return: Hash and SizeBytes come from a tee hasher,
// IndexHeader comes from the returned HeaderInfo (typically the writer's or
// merger's Info() result).
//
// Upload ordering matches PutIndex: index data first, then meta.json as the
// commit marker. If the write callback returns an error, the upload is
// cancelled and meta.json is not written.
func (s *Store) PutIndexStreaming(ctx context.Context, meta *Meta, write func(io.Writer) (format.HeaderInfo, error)) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	pr, pw := io.Pipe()
	hw := newHashCountingWriter(pw)

	uploadErr := make(chan error, 1)
	go func() {
		err := s.bucket.Upload(ctx, meta.IndexPath(), pr)
		// Always close the read end so an early Upload return (cancellation,
		// internal retry that abandons the reader, network error) unblocks
		// the writer instead of parking it forever on pw.Write.
		pr.CloseWithError(err)
		uploadErr <- err
	}()

	info, writeErr := write(hw)

	if writeErr != nil {
		pw.CloseWithError(writeErr)
		<-uploadErr
		return fmt.Errorf("produce index bytes: %w", writeErr)
	}
	if err := pw.Close(); err != nil {
		<-uploadErr
		return fmt.Errorf("close pipe writer: %w", err)
	}
	if err := <-uploadErr; err != nil {
		return fmt.Errorf("failed to upload index data: %w", err)
	}

	meta.Hash = hw.Sum()
	meta.SizeBytes = hw.Count()
	meta.IndexHeader = &info

	if err := meta.Validate(); err != nil {
		return fmt.Errorf("invalid meta after streaming write: %w", err)
	}

	meta.CreatedAt = time.Now().UTC()
	metaBytes, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("failed to marshal meta: %w", err)
	}

	if err := s.bucket.Upload(ctx, meta.MetaPath(), bytes.NewReader(metaBytes)); err != nil {
		return fmt.Errorf("failed to upload meta.json: %w", err)
	}

	level.Info(s.logger).Log(
		"msg", "wrote index",
		"path", meta.IndexPath(),
		"min_time", meta.MinLogTs.Format(time.RFC3339),
		"max_time", meta.MaxLogTs.Format(time.RFC3339),
		"compacted_from", strings.Join(meta.CompactedFrom, ","),
	)
	return nil
}

// Poll performs a single synchronous poll of object storage and updates the snapshot.
// Intended for use in tests from external packages. Production code should use StartPolling.
func (s *Store) Poll(ctx context.Context) error {
	return s.poll(ctx)
}

// ClearKnownMetas empties the delta-poll cache, forcing the next Poll to re-fetch all metas.
// Intended for tests that mutate meta.json in place (e.g., to simulate grace-period expiry).
// Not goroutine-safe; call only from the goroutine that calls Poll.
func (s *Store) ClearKnownMetas() {
	s.knownMetas = make(map[string]Meta)
}

// StartPolling runs an initial blocking poll, then polls in the background at cfg.PollInterval.
// Tests should call Poll() directly for synchronous behavior.
func (s *Store) StartPolling(ctx context.Context) error {
	if err := s.poll(ctx); err != nil {
		s.metrics.pollErrors.Inc()
		level.Error(s.logger).Log("msg", "initial poll failed", "err", err)
		return err
	}

	level.Info(s.logger).Log("msg", "starting index store polling", "interval", s.cfg.PollInterval)
	go func() {
		defer close(s.pollCh)

		ticker := time.NewTicker(s.cfg.PollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if err := s.poll(ctx); err != nil {
					s.metrics.pollErrors.Inc()
					level.Error(s.logger).Log("msg", "poll failed", "err", err)
				} else {
					select {
					case s.pollCh <- s.snapshot.Load():
					default: // consumer busy, they'll get the next one
					}
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	return nil
}

// IndexesForRange returns active indexes whose time range overlaps [start, end].
// Indexes whose MinRecordTs falls within the ingester window (configured via
// QueryIngestersWithin) are excluded — ingesters cover that data.
// No I/O; reads from the current snapshot. Always returns non-nil.
func (s *Store) IndexesForRange(start, end time.Time) []Meta {
	return s.indexesForRange(start, end, false)
}

// IndexesExcludedByIngesterWindow returns active indexes whose log time range
// overlaps [start, end] but whose MinRecordTs falls inside the ingester window
// (so IndexesForRange would omit them). No I/O; always returns non-nil.
func (s *Store) IndexesExcludedByIngesterWindow(start, end time.Time) []Meta {
	return s.indexesForRange(start, end, true)
}

func (s *Store) indexesForRange(start, end time.Time, onlyIngesterWindow bool) []Meta {
	snap := s.snapshot.Load()
	if snap == nil {
		return []Meta{}
	}
	ingesterCutoff := time.Time{}
	if s.cfg.QueryIngestersWithin > 0 {
		ingesterCutoff = time.Now().Add(-s.cfg.QueryIngestersWithin)
	}
	var result []Meta
	for _, m := range snap.active {
		if m.Date < s.cfg.MinDate {
			continue
		}
		if m.MaxLogTs.Before(start) || end.Before(m.MinLogTs) {
			continue
		}
		inWindow := !ingesterCutoff.IsZero() && m.MinRecordTs.After(ingesterCutoff)
		// Keep only the requested side of the ingester-window filter.
		if onlyIngesterWindow != inWindow {
			continue
		}
		result = append(result, m)
	}
	if result == nil {
		return []Meta{}
	}
	return result
}

// EligibleForDeletion returns Metas from the current snapshot that are
// safe to delete based on the store's retention and compaction grace period.
// Delegates to Snapshot.EligibleForDeletion with the store's config values.
func (s *Store) EligibleForDeletion(now time.Time) []Meta {
	snap := s.snapshot.Load()
	if snap == nil {
		return []Meta{}
	}
	retentionCutoff := now.Add(-s.cfg.RetentionDuration)
	graceCutoff := now.Add(-s.cfg.CompactionGracePeriod)
	return snap.EligibleForDeletion(retentionCutoff, graceCutoff)
}

// PollNotify returns a channel that receives the new snapshot after each
// successful background poll. The channel is buffered(1) with non-blocking
// send — if the consumer falls behind, intermediate snapshots are skipped.
// The channel is closed when the polling goroutine exits.
func (s *Store) PollNotify() <-chan *Snapshot {
	return s.pollCh
}

// Snapshot returns the current immutable snapshot. Callers that need to
// check multiple properties (e.g., Active + IsCompacted in a loop) should
// grab the snapshot once rather than calling through the Store repeatedly.
func (s *Store) Snapshot() *Snapshot {
	return s.snapshot.Load()
}

// MinDate returns the configured minimum trusted index date as a UTC timestamp
// at midnight. If parsing fails, zero time is returned.
func (s *Store) MinDate() time.Time {
	// cfg.MinDate is validated by Config.Validate() before Store construction.
	minDate, err := time.Parse("2006-01-02", s.cfg.MinDate)
	if err != nil {
		return time.Time{}
	}
	return minDate.UTC()
}

// DeleteIndex removes the index data and metadata files for the given meta from object storage.
//
// Safety check: refuses to delete a merged index whose compacted sources still exist
// in the snapshot. Deleting such an index would un-compact its sources (they'd reappear
// as active on the next Poll). DeleteIndex compacted sources first.
//
// Not-found errors are ignored for idempotency. Other errors are returned.
//
// Deletion order: meta.json first, then index data. Removing the commit marker first
// ensures Poll stops discovering the index immediately; any orphaned index data file left
// by a partial delete does not cause read errors because nothing references it.
func (s *Store) DeleteIndex(ctx context.Context, meta Meta) error {
	snap := s.snapshot.Load()
	if snap == nil {
		return fmt.Errorf("snapshot not initialized")
	}
	if blockingID, ok := snap.canDelete(meta); !ok {
		return fmt.Errorf("cannot delete merged index %s: compacted source %s still exists", meta.ID(), blockingID)
	}

	if err := s.bucket.Delete(ctx, meta.MetaPath()); err != nil && !s.bucket.IsObjNotFoundErr(err) {
		return fmt.Errorf("failed to delete meta: %w", err)
	}
	if err := s.bucket.Delete(ctx, meta.IndexPath()); err != nil && !s.bucket.IsObjNotFoundErr(err) {
		return fmt.Errorf("failed to delete index data for %s (meta already deleted): %w", meta.ID(), err)
	}

	level.Info(s.logger).Log("msg", "deleted index", "id", meta.ID())
	return nil
}

// poll rebuilds the snapshot from object storage.
// Not goroutine-safe: knownMetas is accessed without a mutex. poll() must be called from a
// single goroutine at a time. The snapshot is swapped atomically for lock-free reads.
func (s *Store) poll(ctx context.Context) error {
	start := time.Now()
	level.Debug(s.logger).Log("msg", "poll started")

	concurrency := max(s.cfg.PollConcurrency, 1)

	// Phase 1: collect all date/id prefixes.
	// First, list date-level prefixes (single Iter call).
	var dates []string
	if err := s.bucket.Iter(ctx, "", func(datePfx string) error {
		if s.shouldSkipDatePrefix(datePfx) {
			return nil
		}
		dates = append(dates, datePfx)
		return nil
	}); err != nil {
		return fmt.Errorf("poll list dates failed: %w", err)
	}

	// Then list hash prefixes under each date concurrently.
	datePrefixes := make([][]string, len(dates))

	g := errgroup.Group{}
	g.SetLimit(concurrency)
	for i, date := range dates {
		g.Go(func() error {
			return s.bucket.Iter(ctx, date, func(hashPfx string) error {
				datePrefixes[i] = append(datePrefixes[i], hashPfx)
				return nil
			})
		})
	}
	if err := g.Wait(); err != nil {
		return fmt.Errorf("poll list failed: %w", err)
	}

	var prefixes []string
	for _, dp := range datePrefixes {
		prefixes = append(prefixes, dp...)
	}

	if len(prefixes) == 0 {
		s.knownMetas = make(map[string]Meta) // all indexes gone; prune the cache
		s.snapshot.Store(buildSnapshot(nil))
		s.metrics.pollDuration.Observe(time.Since(start).Seconds())
		s.metrics.indexes.Reset()
		s.metrics.indexBytes.Reset()
		level.Info(s.logger).Log("msg", "poll completed", "duration", time.Since(start), "total", 0, "active", 0, "compacted", 0, "fetched", 0)
		return nil
	}

	// Phase 2: delta fetch — only fetch metas for prefixes not already in knownMetas.
	// meta.json is immutable once written; cached values are always valid.
	type fetchResult struct {
		meta Meta
		ok   bool
	}

	// Classify prefixes: reuse cached metas or queue for fetch.
	// nextKnown is rebuilt from listed prefixes only — entries absent from the listing
	// are implicitly dropped, so deleted indexes disappear without explicit bookkeeping.
	nextKnown := make(map[string]Meta, len(prefixes))
	var toFetch []string

	for _, pfx := range prefixes {
		id := strings.TrimSuffix(pfx, "/")
		if m, ok := s.knownMetas[id]; ok {
			nextKnown[id] = m
		} else {
			toFetch = append(toFetch, pfx)
		}
	}

	// Fetch only the unknown metas concurrently.
	results := make([]fetchResult, len(toFetch))
	g = errgroup.Group{}
	g.SetLimit(concurrency)
	for i, pfx := range toFetch {
		g.Go(func() error {
			meta, ok, err := s.fetchMeta(ctx, pfx)
			if err != nil {
				return err
			}
			results[i] = fetchResult{meta: meta, ok: ok}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return fmt.Errorf("poll failed: %w", err)
	}

	// Populate nextKnown from newly fetched results.
	for i, pfx := range toFetch {
		if results[i].ok {
			id := strings.TrimSuffix(pfx, "/")
			nextKnown[id] = results[i].meta
		}
	}

	// Replace known metas. Only entries still in the listing are retained.
	s.knownMetas = nextKnown

	entries := make([]Meta, 0, len(nextKnown))
	for _, m := range nextKnown {
		entries = append(entries, m)
	}

	snap := buildSnapshot(entries)
	s.snapshot.Store(snap)

	// Per-date index counts and bytes.
	s.metrics.indexes.Reset()
	s.metrics.indexBytes.Reset()
	now := time.Now()
	ingesterCutoff := time.Time{}
	if s.cfg.QueryIngestersWithin > 0 {
		ingesterCutoff = now.Add(-s.cfg.QueryIngestersWithin)
	}
	dateCutoff := now.AddDate(0, 0, -7).Format("2006-01-02")
	for _, m := range snap.all {
		state := "active"
		if _, isCompacted := snap.compactedAt[m.ID()]; isCompacted {
			state = "compacted"
		} else if m.Date < s.cfg.MinDate {
			state = "pre_min_date"
		} else if !ingesterCutoff.IsZero() && m.MinRecordTs.After(ingesterCutoff) {
			state = "ingester_window"
		}
		date := m.Date
		if date < dateCutoff {
			date = "past"
		}
		s.metrics.indexes.WithLabelValues(state, date).Inc()
		s.metrics.indexBytes.WithLabelValues(state, date).Add(float64(m.SizeBytes))
	}

	elapsed := time.Since(start)
	s.metrics.pollDuration.Observe(elapsed.Seconds())
	level.Info(s.logger).Log(
		"msg", "poll completed",
		"duration", elapsed,
		"total", len(snap.all),
		"active", len(snap.active),
		"compacted", len(snap.all)-len(snap.active),
		"fetched", len(toFetch),
	)
	return nil
}

func (s *Store) shouldSkipDatePrefix(datePfx string) bool {
	date := strings.TrimSuffix(datePfx, "/")

	// MinDate is only comparable lexicographically for canonical YYYY-MM-DD
	// partitions. Keep malformed prefixes visible to the existing validation
	// path instead of silently hiding unexpected object layouts.
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return false
	}
	return date < s.cfg.MinDate
}

// fetchMeta returns (meta, true, nil) on success, (Meta{}, false, nil) if the
// meta.json does not exist (partial write in progress), or (Meta{}, false, err)
// on I/O failure or invalid meta.
func (s *Store) fetchMeta(ctx context.Context, metaPath string) (Meta, bool, error) {
	metaPath = strings.TrimSuffix(metaPath, "/") // bucket.Iter returns a "path prefix" with a trailing slash; remove it for metaFromID
	meta, err := s.loadMeta(ctx, metaPath)
	if err != nil {
		if s.bucket.IsObjNotFoundErr(err) {
			return Meta{}, false, nil // partial write in progress
		}
		return Meta{}, false, fmt.Errorf("failed to get meta.json at %s: %w", metaPath, err)
	}

	return meta, true, nil
}

func (s *Store) loadMeta(ctx context.Context, id string) (Meta, error) {
	pathMeta, err := metaFromID(id)
	if err != nil {
		return Meta{}, fmt.Errorf("%w: %v", errInvalidMeta, err)
	}

	rc, err := s.bucket.Get(ctx, pathMeta.MetaPath())
	if err != nil {
		return Meta{}, err
	}
	defer rc.Close()

	var meta Meta
	if err := json.NewDecoder(io.LimitReader(rc, 1<<20)).Decode(&meta); err != nil {
		return Meta{}, fmt.Errorf("%w: decode %s: %v", errInvalidMeta, id, err)
	}
	if meta.MinLogTs.IsZero() || meta.MaxLogTs.IsZero() {
		return Meta{}, fmt.Errorf("%w: zero MinTime or MaxTime in %s", errInvalidMeta, id)
	}
	if meta.Date != pathMeta.Date {
		return Meta{}, fmt.Errorf("%w: mismatched date path=%q meta=%q", errInvalidMeta, pathMeta.Date, meta.Date)
	}
	// Validate StorageID consistency between path and meta.json.
	// New indexes serialize StorageID as "id" in meta.json; legacy indexes omit it.
	if meta.StorageID != "" {
		if meta.StorageID != pathMeta.StorageID {
			return Meta{}, fmt.Errorf("%w: mismatched storage id path=%q meta=%q", errInvalidMeta, pathMeta.StorageID, meta.StorageID)
		}
	} else {
		// Legacy index: no StorageID in JSON, path component equals Hash.
		meta.StorageID = pathMeta.StorageID
		if meta.Hash != pathMeta.StorageID {
			return Meta{}, fmt.Errorf("%w: mismatched hash path=%q meta=%q", errInvalidMeta, pathMeta.StorageID, meta.Hash)
		}
	}

	// Legacy indexes omit SizeBytes from meta.json; fall back to bucket Attributes.
	if meta.SizeBytes == 0 {
		attrs, err := s.bucket.Attributes(ctx, meta.IndexPath())
		if err != nil {
			return Meta{}, err
		}
		meta.SizeBytes = attrs.Size
	}

	return meta, nil
}

func buildSnapshot(entries []Meta) *Snapshot {
	snap := &Snapshot{
		all:         entries,
		allIDs:      make(map[string]struct{}, len(entries)),
		compactedAt: make(map[string]time.Time),
	}
	for _, m := range entries {
		snap.allIDs[m.ID()] = struct{}{}
	}
	// If a source is covered by multiple merged indexes, keep the most recent CreatedAt
	// (conservative: wait for the newest covering index to age past the grace period).
	for _, m := range entries {
		for _, sourceID := range m.CompactedFrom {
			if existing, ok := snap.compactedAt[sourceID]; !ok || m.CreatedAt.After(existing) {
				snap.compactedAt[sourceID] = m.CreatedAt
			}
		}
	}
	for _, m := range entries {
		if _, isCompacted := snap.compactedAt[m.ID()]; !isCompacted {
			snap.active = append(snap.active, m)
		}
	}
	return snap
}

// metaFromID is only to be used as a helper func for the above path funcs. it does not
// create a full meta
func metaFromID(id string) (Meta, error) {
	parts := strings.SplitN(id, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return Meta{}, fmt.Errorf("invalid index id %q: expected \"date/id\"", id)
	}
	// Validate date format (YYYY-MM-DD) to block path traversal.
	if _, err := time.Parse("2006-01-02", parts[0]); err != nil {
		return Meta{}, fmt.Errorf("invalid index id %q: bad date: %w", id, err)
	}
	if parts[1] == "." || parts[1] == ".." || strings.Contains(parts[1], "/") {
		return Meta{}, fmt.Errorf("invalid index id %q: invalid storage id", id)
	}

	return Meta{Date: parts[0], StorageID: parts[1]}, nil
}
