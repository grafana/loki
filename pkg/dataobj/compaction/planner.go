package planner

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log/level"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/thanos-io/objstore"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/dataobj"
	compactionpb "github.com/grafana/loki/v3/pkg/dataobj/compaction/proto"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/symbolizer"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

const (
	// targetUncompressedSize is the target uncompressed size for output data objects (6GB).
	targetUncompressedSize int64 = 6 << 30

	// maxOutputMultiple is the maximum multiple of target size allowed for an output object.
	// When all estimated objects are created and none have capacity, we allow objects to grow
	// up to this multiple of the target size. The object builder will then create multiple objects, each upto target size.
	maxOutputMultiple int64 = 2

	// minFillPercent is the minimum fill percentage for an output object.
	// Objects below this threshold are candidates for merging during optimization.
	minFillPercent int64 = 70

	// compactionWindowDuration is the duration of each compaction window.
	compactionWindowDuration = 2 * time.Hour

	// maxParallelIndexReads is the maximum number of indexes to read in parallel.
	maxParallelIndexReads = 8
)

// Sizer is an interface for items that have a size.
// Used by generic bin-packing algorithm.
type Sizer interface {
	GetSize() int64
}

// BinPackResult represents a bin containing groups and their total size.
type BinPackResult[G Sizer] struct {
	Groups []G
	Size   int64
}

// IndexStreamReader reads stream metadata from index objects.
// This interface allows for mocking in tests.
type IndexStreamReader interface {
	ReadStreams(ctx context.Context, indexPath, tenant string, windowStart, windowEnd time.Time) (*IndexStreamResult, error)
}

// BucketIndexStreamReader is the production implementation that reads from object storage.
type BucketIndexStreamReader struct {
	bucket objstore.Bucket
}

// ReadStreams reads stream metadata from an index object in the bucket.
func (r *BucketIndexStreamReader) ReadStreams(ctx context.Context, indexPath, tenant string, windowStart, windowEnd time.Time) (*IndexStreamResult, error) {
	obj, err := dataobj.FromBucket(ctx, r.bucket, indexPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open index object %s: %w", indexPath, err)
	}
	return readStreamsFromIndexObject(ctx, obj, indexPath, tenant, windowStart, windowEnd)
}

// Planner creates compaction plans by reading stream metadata from indexes
// and grouping streams into output objects using bin-packing algorithms.
type Planner struct {
	bucket      objstore.Bucket
	indexReader IndexStreamReader
}

// NewPlanner creates a new Planner with the given bucket.
func NewPlanner(bucket objstore.Bucket) *Planner {
	return &Planner{
		bucket:      bucket,
		indexReader: &BucketIndexStreamReader{bucket: bucket},
	}
}

// IndexInfo contains metadata about an Index Object.
type IndexInfo struct {
	Path   string
	Tenant string
}

// CompactionPlan represents the complete plan for compacting data objects for a single tenant.
type CompactionPlan struct {
	// OutputObjects contains the planned output objects (one per bin).
	OutputObjects []*compactionpb.SingleTenantObjectSource
	// TotalUncompressedSize is the total uncompressed size across all output objects (for reporting).
	TotalUncompressedSize int64
	// LeftoverBeforeStreams contains streams with data before the compaction window.
	LeftoverBeforeStreams []*LeftoverStreamGroup
	// LeftoverAfterStreams contains streams with data after the compaction window.
	LeftoverAfterStreams []*LeftoverStreamGroup
}

// StreamGroup represents a group of stream entries that belong to the same stream
// (identified by labels hash) across multiple index objects.
type StreamGroup struct {
	// LabelsHash is the stable hash of the stream labels.
	LabelsHash uint64
	// Streams contains all the stream entries for this stream (from different indexes).
	Streams []*compactionpb.Stream
	// TotalUncompressedSize is the sum of uncompressed sizes across all streams.
	TotalUncompressedSize int64
}

// GetSize implements Sizer interface for bin-packing.
func (g *StreamGroup) GetSize() int64 { return g.TotalUncompressedSize }

// StreamInfo represents aggregated stream information from an index object.
// This is derived from the streams section which already aggregates all
// stream metadata across all data objects that the index covers.
// Embeds compactionpb.Stream for StreamID and Index fields.
type StreamInfo struct {
	// Embedded proto Stream (provides StreamID and Index fields)
	compactionpb.Stream
	// LabelsHash is the stable hash of the stream labels.
	LabelsHash uint64
	// UncompressedSize is the total uncompressed size of this stream.
	UncompressedSize int64
}

// LeftoverPlan represents the plan for collecting leftover data outside the compaction window.
// Bin-packing is done separately for data before and after the window using stream-level granularity.
type LeftoverPlan struct {
	// BeforeWindow contains planned output objects for data BEFORE the compaction window.
	BeforeWindow []*compactionpb.MultiTenantObjectSource
	// BeforeWindowSize is the total uncompressed size of BeforeWindow (for reporting).
	BeforeWindowSize int64
	// AfterWindow contains planned output objects for data AFTER the compaction window.
	AfterWindow []*compactionpb.MultiTenantObjectSource
	// AfterWindowSize is the total uncompressed size of AfterWindow (for reporting).
	AfterWindowSize int64
}

// LeftoverStreamGroup represents streams with the same labels that have leftover data.
type LeftoverStreamGroup struct {
	LabelsHash            uint64
	Streams               []LeftoverStreamInfo
	TotalUncompressedSize int64
}

// GetSize implements Sizer interface for bin-packing.
func (g *LeftoverStreamGroup) GetSize() int64 { return g.TotalUncompressedSize }

// LeftoverStreamInfo represents a stream's leftover data outside the compaction window.
// Embeds compactionpb.TenantStream since leftover data is aggregated across tenants.
type LeftoverStreamInfo struct {
	// Embedded proto TenantStream (provides Tenant, StreamID, and Index fields)
	compactionpb.TenantStream
	LabelsHash       uint64
	UncompressedSize int64
}

// StreamCollectionResult holds the result of collecting streams from indexes.
type StreamCollectionResult struct {
	// StreamGroups contains streams grouped by labels hash.
	StreamGroups []*StreamGroup
	// TotalUncompressedSize is the sum of uncompressed sizes across all stream groups.
	TotalUncompressedSize int64
	// LeftoverBeforeStreams contains streams with data before the compaction window.
	LeftoverBeforeStreams []*LeftoverStreamGroup
	// LeftoverAfterStreams contains streams with data after the compaction window.
	LeftoverAfterStreams []*LeftoverStreamGroup
}

// IndexStreamResult holds stream infos from reading an index.
type IndexStreamResult struct {
	Streams               []StreamInfo
	LeftoverBeforeStreams []LeftoverStreamInfo
	LeftoverAfterStreams  []LeftoverStreamInfo
}

func (s *Planner) buildPlan(ctx context.Context) error {
	now := time.Now()
	windowStart := now.Truncate(compactionWindowDuration).Add(-compactionWindowDuration)
	windowEnd := windowStart.Add(compactionWindowDuration)

	level.Debug(util_log.Logger).Log("msg", "Building compaction plan",
		"compaction_window_start", windowStart, "compaction_window_end", windowEnd)

	// Step 1: Find all indexes that overlap with the compaction window
	indexesToCompact, err := s.findIndexesToCompact(ctx, windowStart, windowEnd)
	if err != nil {
		return err
	}
	if len(indexesToCompact) == 0 {
		level.Debug(util_log.Logger).Log("msg", "no indexes to compact")
		return nil
	}

	// Step 2: Check if enough time has passed since the oldest object was created
	ready, err := s.checkCompactionReadiness(ctx, indexesToCompact, now)
	if err != nil {
		return err
	}
	if !ready {
		level.Info(util_log.Logger).Log("msg", "not enough time has passed to compact",
			"compaction_window_start", windowStart, "compaction_window_end", windowEnd)
		return nil
	}

	// Step 3: Build plans for each tenant and aggregate leftover streams
	tenantPlans, leftoverPlan, err := s.buildPlanFromIndexes(ctx, indexesToCompact, windowStart, windowEnd)
	if err != nil {
		return err
	}

	// Log tenant plan results
	for tenant, plan := range tenantPlans {
		level.Info(util_log.Logger).Log("msg", "Compaction plan built for tenant", "tenant", tenant,
			"output_objects", countObjects(plan.OutputObjects), "output_objects_size", plan.TotalUncompressedSize)
	}

	if leftoverPlan != nil {
		level.Info(util_log.Logger).Log("msg", "compaction plan for leftover data built",
			"before_objects", len(leftoverPlan.BeforeWindow), "before_objects_size", leftoverPlan.BeforeWindowSize,
			"after_objects", len(leftoverPlan.AfterWindow), "after_objects_size", leftoverPlan.AfterWindowSize)
	}

	return nil
}

// findIndexesToCompact scans ToC files and returns unique indexes that overlap with the compaction window.
func (s *Planner) findIndexesToCompact(ctx context.Context, windowStart, windowEnd time.Time) ([]IndexInfo, error) {
	tocs, err := s.listToCs(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list ToCs: %w", err)
	}

	// Sort ToCs in descending order (newest first)
	sort.Slice(tocs, func(i, j int) bool {
		return tocs[i] > tocs[j]
	})

	type tenantIndex struct {
		tenant, path string
	}
	seen := make(map[tenantIndex]struct{}) // tenant/path -> seen
	var indexes []IndexInfo

	for _, toc := range tocs {
		level.Info(util_log.Logger).Log("msg", "Processing ToC", "toc", toc)

		obj, err := s.readToCObject(ctx, toc)
		if err != nil {
			return nil, fmt.Errorf("failed to read ToC %s: %w", toc, err)
		}

		for result := range indexpointers.Iter(ctx, obj) {
			ptr, err := result.Value()
			if err != nil {
				return nil, fmt.Errorf("failed to iterate ToC %s: %w", toc, err)
			}

			key := tenantIndex{
				tenant: ptr.Tenant,
				path:   ptr.Path,
			}
			if _, ok := seen[key]; ok {
				continue
			}

			minTime := ptr.StartTs.UTC()
			maxTime := ptr.EndTs.UTC()

			// Skip indexes outside the compaction window
			if maxTime.Before(windowStart) || minTime.After(windowEnd) {
				continue
			}

			indexes = append(indexes, IndexInfo{
				Path:   ptr.Path,
				Tenant: ptr.Tenant,
			})
			seen[key] = struct{}{}
		}
	}

	return indexes, nil
}

// readToCObject reads and parses a ToC object from storage.
func (s *Planner) readToCObject(ctx context.Context, tocPath string) (*dataobj.Object, error) {
	objReader, err := s.bucket.Get(ctx, tocPath)
	if err != nil {
		return nil, err
	}
	defer objReader.Close()

	objectBytes, err := io.ReadAll(objReader)
	if err != nil {
		return nil, err
	}

	return dataobj.FromReaderAt(bytes.NewReader(objectBytes), int64(len(objectBytes)))
}

// checkCompactionReadiness verifies that enough time has passed since the oldest index was created.
// Returns true if compaction can proceed.
func (s *Planner) checkCompactionReadiness(ctx context.Context, indexes []IndexInfo, now time.Time) (bool, error) {
	requiredOldestTimestamp := now.Add(-compactionWindowDuration)
	oldestTimestamp := now

	for _, index := range indexes {
		attrs, err := s.bucket.Attributes(ctx, index.Path)
		if err != nil {
			return false, fmt.Errorf("failed to get attributes for index %s: %w", index.Path, err)
		}
		lastModified := attrs.LastModified.UTC()

		if oldestTimestamp.After(lastModified) {
			oldestTimestamp = lastModified
		}

		// Early exit: we found an old enough object
		if oldestTimestamp.Before(requiredOldestTimestamp) {
			return true, nil
		}
	}

	return oldestTimestamp.Before(requiredOldestTimestamp), nil
}

// buildPlanFromIndexes builds compaction plans for each tenant and aggregates leftover streams.
func (s *Planner) buildPlanFromIndexes(ctx context.Context, indexes []IndexInfo, windowStart, windowEnd time.Time) (map[string]*CompactionPlan, *LeftoverPlan, error) {
	// Group indexes by tenant
	indexesByTenant := make(map[string][]IndexInfo)
	for _, index := range indexes {
		indexesByTenant[index.Tenant] = append(indexesByTenant[index.Tenant], index)
	}

	// Build plans and aggregate leftovers
	tenantPlans := make(map[string]*CompactionPlan)
	leftoverBeforeMap := make(map[uint64]*LeftoverStreamGroup)
	leftoverAfterMap := make(map[uint64]*LeftoverStreamGroup)

	for tenant, tenantIndexes := range indexesByTenant {
		level.Info(util_log.Logger).Log("msg", "Building plan for tenant", "tenant", tenant, "num_indexes", len(tenantIndexes))

		plan, err := s.buildTenantPlan(ctx, tenant, tenantIndexes, windowStart, windowEnd)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to build plan for tenant %s: %w", tenant, err)
		}
		tenantPlans[tenant] = plan

		// Aggregate leftover streams by labels hash
		aggregateLeftovers(plan.LeftoverBeforeStreams, leftoverBeforeMap)
		aggregateLeftovers(plan.LeftoverAfterStreams, leftoverAfterMap)
	}

	leftoverPlan := s.planLeftovers(collectLeftoverGroups(leftoverBeforeMap), collectLeftoverGroups(leftoverAfterMap))

	return tenantPlans, leftoverPlan, nil
}

// aggregateLeftovers merges leftover stream groups into the aggregation map by labels hash.
func aggregateLeftovers(groups []*LeftoverStreamGroup, dest map[uint64]*LeftoverStreamGroup) {
	for _, group := range groups {
		existing, ok := dest[group.LabelsHash]
		if !ok {
			existing = &LeftoverStreamGroup{LabelsHash: group.LabelsHash}
			dest[group.LabelsHash] = existing
		}
		existing.Streams = append(existing.Streams, group.Streams...)
		existing.TotalUncompressedSize += group.TotalUncompressedSize
	}
}

// collectLeftoverGroups extracts all LeftoverStreamGroups from the map into a slice.
func collectLeftoverGroups(groups map[uint64]*LeftoverStreamGroup) []*LeftoverStreamGroup {
	result := make([]*LeftoverStreamGroup, 0, len(groups))
	for _, group := range groups {
		result = append(result, group)
	}
	return result
}

// buildTenantPlan creates a compaction plan for merging data objects for a single tenant.
// It reads stream metadata from indexes and groups them by stream labels.
//
// For small tenants (total size < target), bin packing is skipped since all
// streams will fit in a single output object anyway.
func (s *Planner) buildTenantPlan(ctx context.Context, tenant string, indexes []IndexInfo, compactionWindowStart, compactionWindowEnd time.Time) (*CompactionPlan, error) {
	if len(indexes) == 0 {
		return nil, fmt.Errorf("no indexes to compact")
	}

	if tenant == "" {
		return nil, fmt.Errorf("tenant is required")
	}

	// Step 1: Collect streams grouped by labels hash
	collectionResult, err := s.collectStreams(ctx, tenant, indexes, compactionWindowStart, compactionWindowEnd)
	if err != nil {
		return nil, fmt.Errorf("failed to collect streams: %w", err)
	}

	streamGroups := collectionResult.StreamGroups
	leftoverBeforeStreams := collectionResult.LeftoverBeforeStreams
	leftoverAfterStreams := collectionResult.LeftoverAfterStreams
	totalUncompressedSize := collectionResult.TotalUncompressedSize

	if len(streamGroups) == 0 {
		return nil, fmt.Errorf("no streams found within compaction window for tenant %s", tenant)
	}

	// Step 2: For small tenants, skip bin packing - all streams fit in one object
	if totalUncompressedSize < targetUncompressedSize {
		level.Info(util_log.Logger).Log("msg", "tenant is small, skipping bin packing", "tenant", tenant, "size", totalUncompressedSize)

		// Collect all streams into one output object
		var allStreams []*compactionpb.Stream
		for _, group := range streamGroups {
			allStreams = append(allStreams, group.Streams...)
		}

		return &CompactionPlan{
			OutputObjects: []*compactionpb.SingleTenantObjectSource{{
				Streams:          allStreams,
				NumOutputObjects: 1,
			}},
			TotalUncompressedSize: totalUncompressedSize,
			LeftoverBeforeStreams: leftoverBeforeStreams,
			LeftoverAfterStreams:  leftoverAfterStreams,
		}, nil
	}

	// Step 3: Large tenant - run bin packing
	bins := BinPack(streamGroups)

	// Convert bins to proto OutputObjects
	outputObjects := make([]*compactionpb.SingleTenantObjectSource, len(bins))
	var totalSize int64
	for i, bin := range bins {
		totalSize += bin.Size
		var allStreams []*compactionpb.Stream
		for _, group := range bin.Groups {
			allStreams = append(allStreams, group.Streams...)
		}
		outputObjects[i] = &compactionpb.SingleTenantObjectSource{
			Streams:          allStreams,
			NumOutputObjects: calculateNumOutputObjects(bin.Size),
		}
	}

	return &CompactionPlan{
		OutputObjects:         outputObjects,
		TotalUncompressedSize: totalSize,
		LeftoverBeforeStreams: leftoverBeforeStreams,
		LeftoverAfterStreams:  leftoverAfterStreams,
	}, nil
}

// collectStreams reads stream metadata from all indexes and groups them by labels hash.
// Only streams for the specified tenant within the compaction window are included.
func (s *Planner) collectStreams(ctx context.Context, tenant string, indexes []IndexInfo, windowStart, windowEnd time.Time) (*StreamCollectionResult, error) {
	// Map from labels_hash -> stream group
	streamGroupMap := make(map[uint64]*StreamGroup)

	// Maps for leftover streams grouped by labels hash
	leftoverBeforeMap := make(map[uint64]*LeftoverStreamGroup)
	leftoverAfterMap := make(map[uint64]*LeftoverStreamGroup)

	var mu sync.Mutex
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(maxParallelIndexReads)

	for _, index := range indexes {
		indexPath := index.Path
		g.Go(func() error {
			// Read streams using the injected reader
			result, err := s.indexReader.ReadStreams(ctx, indexPath, tenant, windowStart, windowEnd)
			if err != nil {
				return fmt.Errorf("failed to read streams from index %s: %w", indexPath, err)
			}

			mu.Lock()
			defer mu.Unlock()

			// Group leftover before streams by labels hash
			for _, info := range result.LeftoverBeforeStreams {
				group, ok := leftoverBeforeMap[info.LabelsHash]
				if !ok {
					group = &LeftoverStreamGroup{LabelsHash: info.LabelsHash}
					leftoverBeforeMap[info.LabelsHash] = group
				}
				group.Streams = append(group.Streams, info)
				group.TotalUncompressedSize += info.UncompressedSize
			}

			// Group leftover after streams by labels hash
			for _, info := range result.LeftoverAfterStreams {
				group, ok := leftoverAfterMap[info.LabelsHash]
				if !ok {
					group = &LeftoverStreamGroup{LabelsHash: info.LabelsHash}
					leftoverAfterMap[info.LabelsHash] = group
				}
				group.Streams = append(group.Streams, info)
				group.TotalUncompressedSize += info.UncompressedSize
			}

			// Group streams by labels hash
			for _, info := range result.Streams {
				group, ok := streamGroupMap[info.LabelsHash]
				if !ok {
					group = &StreamGroup{LabelsHash: info.LabelsHash}
					streamGroupMap[info.LabelsHash] = group
				}
				group.Streams = append(group.Streams, &info.Stream)
				group.TotalUncompressedSize += info.UncompressedSize
			}

			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	// Convert stream group map to slice
	var totalUncompressedSize int64
	streamGroups := make([]*StreamGroup, 0, len(streamGroupMap))
	for _, group := range streamGroupMap {
		if len(group.Streams) > 0 {
			streamGroups = append(streamGroups, group)
			totalUncompressedSize += group.TotalUncompressedSize
		}
	}

	// Convert leftover maps to slices
	leftoverBeforeStreams := make([]*LeftoverStreamGroup, 0, len(leftoverBeforeMap))
	for _, group := range leftoverBeforeMap {
		if len(group.Streams) > 0 {
			leftoverBeforeStreams = append(leftoverBeforeStreams, group)
		}
	}
	leftoverAfterStreams := make([]*LeftoverStreamGroup, 0, len(leftoverAfterMap))
	for _, group := range leftoverAfterMap {
		if len(group.Streams) > 0 {
			leftoverAfterStreams = append(leftoverAfterStreams, group)
		}
	}

	return &StreamCollectionResult{
		StreamGroups:          streamGroups,
		TotalUncompressedSize: totalUncompressedSize,
		LeftoverBeforeStreams: leftoverBeforeStreams,
		LeftoverAfterStreams:  leftoverAfterStreams,
	}, nil
}

// readStreamsFromIndexObject reads stream metadata directly from an index object's streams section
// for a specific tenant. This avoids reading the expensive pointers section since streams
// already contain aggregated metadata (labels, time range, size, row count) for planning.
// Also collects leftover stream info (data before/after the compaction window).
func readStreamsFromIndexObject(ctx context.Context, obj *dataobj.Object, indexPath string, tenant string, windowStart, windowEnd time.Time) (*IndexStreamResult, error) {
	result := &IndexStreamResult{}
	labelSymbolizer := symbolizer.New(128, 100_000)

	for _, section := range obj.Sections().Filter(streams.CheckSection) {
		// Filter by tenant - each section belongs to a specific tenant
		if section.Tenant != tenant {
			continue
		}

		sec, err := streams.Open(ctx, section)
		if err != nil {
			return nil, fmt.Errorf("failed to open streams section: %w", err)
		}

		for streamVal := range streams.IterSection(ctx, sec, streams.WithSymbolizer(labelSymbolizer), streams.WithReuseLabelsBuffer()) {
			stream, err := streamVal.Value()
			if err != nil {
				return nil, fmt.Errorf("failed to read stream: %w", err)
			}

			labelsHash := labels.StableHash(stream.Labels)
			originalDuration := stream.MaxTimestamp.Sub(stream.MinTimestamp)

			// Collect leftover stream info for data BEFORE the window
			if stream.MinTimestamp.Before(windowStart) {
				beforeEnd := stream.MaxTimestamp
				if beforeEnd.After(windowStart) {
					beforeEnd = windowStart
				}
				beforeDuration := beforeEnd.Sub(stream.MinTimestamp)

				result.LeftoverBeforeStreams = append(result.LeftoverBeforeStreams, LeftoverStreamInfo{
					TenantStream: compactionpb.TenantStream{
						Tenant: tenant,
						Stream: &compactionpb.Stream{
							StreamID: stream.ID,
							Index:    indexPath,
						},
					},
					LabelsHash:       labelsHash,
					UncompressedSize: prorateSize(stream.UncompressedSize, beforeDuration, originalDuration),
				})
			}

			// Collect leftover stream info for data AFTER the window
			if stream.MaxTimestamp.After(windowEnd) {
				afterStart := stream.MinTimestamp
				if afterStart.Before(windowEnd) {
					afterStart = windowEnd
				}
				afterDuration := stream.MaxTimestamp.Sub(afterStart)

				result.LeftoverAfterStreams = append(result.LeftoverAfterStreams, LeftoverStreamInfo{
					TenantStream: compactionpb.TenantStream{
						Tenant: tenant,
						Stream: &compactionpb.Stream{
							StreamID: stream.ID,
							Index:    indexPath,
						},
					},
					LabelsHash:       labelsHash,
					UncompressedSize: prorateSize(stream.UncompressedSize, afterDuration, originalDuration),
				})
			}

			// Filter out streams completely outside the compaction window
			if stream.MaxTimestamp.Before(windowStart) || stream.MinTimestamp.After(windowEnd) {
				continue
			}

			// Calculate the size for the portion within the compaction window
			minTime := stream.MinTimestamp
			maxTime := stream.MaxTimestamp
			if minTime.Before(windowStart) {
				minTime = windowStart
			}
			if maxTime.After(windowEnd) {
				maxTime = windowEnd
			}
			clampedDuration := maxTime.Sub(minTime)

			result.Streams = append(result.Streams, StreamInfo{
				Stream: compactionpb.Stream{
					StreamID: stream.ID,
					Index:    indexPath,
				},
				LabelsHash:       labelsHash,
				UncompressedSize: prorateSize(stream.UncompressedSize, clampedDuration, originalDuration),
			})
		}
	}

	return result, nil
}

// planLeftovers creates a plan for leftover data outside the compaction window.
// Uses stream-based bin-packing separately for data before and after the window.
func (s *Planner) planLeftovers(beforeStreams, afterStreams []*LeftoverStreamGroup) *LeftoverPlan {
	if len(beforeStreams) == 0 && len(afterStreams) == 0 {
		return nil
	}

	// Bin-pack and convert to proto format
	beforeBins := BinPack(beforeStreams)
	afterBins := BinPack(afterStreams)

	beforeObjects, beforeSize := convertLeftoverBinsToMultiTenantObjectSource(beforeBins)
	afterObjects, afterSize := convertLeftoverBinsToMultiTenantObjectSource(afterBins)

	return &LeftoverPlan{
		BeforeWindow:     beforeObjects,
		BeforeWindowSize: beforeSize,
		AfterWindow:      afterObjects,
		AfterWindowSize:  afterSize,
	}
}

// listToCs lists all ToC files in storage.
func (s *Planner) listToCs(ctx context.Context) ([]string, error) {
	var tocs []string

	err := s.bucket.Iter(ctx, metastore.TocPrefix, func(name string) error {
		if !strings.HasSuffix(name, ".toc") {
			return nil
		}
		tocs = append(tocs, name)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return tocs, nil
}

// BinPack performs best-fit decreasing bin packing with overflow and merging.
//
// Algorithm:
//  1. Sort items by size (largest first) for better packing
//  2. For each item, find the best-fit bin (smallest remaining capacity that fits)
//  3. If no bin fits within targetUncompressedSize, create a new bin if we are below the estimated bin count
//  4. If no bin fits within targetUncompressedSize and we have reached the estimated bin count, try overflowing bins with upto targetUncompressedSize * maxOutputMultiple
//  5. If still no fit, create a new bin
//  6. Merge under-filled bins (below minFillPercent) in a post-processing step
func BinPack[G Sizer](groups []G) []BinPackResult[G] {
	if len(groups) == 0 {
		return nil
	}

	// Sort by size descending for better packing
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].GetSize() > groups[j].GetSize()
	})

	var totalUncompressedSize int64
	for _, group := range groups {
		totalUncompressedSize += group.GetSize()
	}
	estimatedBinCount := int((totalUncompressedSize + targetUncompressedSize - 1) / targetUncompressedSize)
	if estimatedBinCount < 1 {
		estimatedBinCount = 1
	}

	maxOverflowSize := targetUncompressedSize * maxOutputMultiple
	var bins []BinPackResult[G]

	for _, group := range groups {
		groupSize := group.GetSize()

		// Find best-fit bin within target size
		bestIdx := findBestFitBin(bins, groupSize, targetUncompressedSize)

		if bestIdx >= 0 {
			bins[bestIdx].Groups = append(bins[bestIdx].Groups, group)
			bins[bestIdx].Size += groupSize
			continue
		}

		// No bin has capacity within target size
		if len(bins) < estimatedBinCount {
			// Create a new bin since we haven't reached the estimated bin count
			bins = append(bins, BinPackResult[G]{
				Groups: []G{group},
				Size:   groupSize,
			})
			continue
		}

		// No bin has capacity within target size, and we have already created the estimated number of bins, try overflowing the bins
		bestIdx = findBestFitBin(bins, groupSize, maxOverflowSize)
		if bestIdx >= 0 {
			bins[bestIdx].Groups = append(bins[bestIdx].Groups, group)
			bins[bestIdx].Size += groupSize
			continue
		}

		// None of the existing bins has capacity even after overflow, create a new bin
		bins = append(bins, BinPackResult[G]{
			Groups: []G{group},
			Size:   groupSize,
		})
	}

	// Post-processing: merge under-filled bins
	return mergeUnderfilledBins(bins)
}

// findBestFitBin finds the bin with smallest remaining capacity that can fit the given size.
// Returns -1 if no bin can fit the item within maxSize.
func findBestFitBin[G Sizer](bins []BinPackResult[G], itemSize, maxSize int64) int {
	bestIdx := -1
	bestRemaining := maxSize + 1

	for i, bin := range bins {
		if bin.Size > maxSize {
			// Bin already exceeds maxSize and will be split into multiple output objects.
			// Try to fill the partially filled object up to the next targetUncompressedSize boundary.

			// Skip if already at a perfect multiple (output objects are fully filled).
			partialSize := bin.Size % targetUncompressedSize
			if partialSize == 0 {
				continue
			}

			extraSpace := targetUncompressedSize - partialSize
			remaining := extraSpace - itemSize

			// Only consider adding this item to the partially filled object if it does not exceed the remaining space.
			if remaining < 0 {
				continue
			}

			if remaining < bestRemaining {
				bestRemaining = remaining
				bestIdx = i
			}
		} else {
			newSize := bin.Size + itemSize
			if newSize <= maxSize {
				remaining := maxSize - newSize
				if remaining < bestRemaining {
					bestRemaining = remaining
					bestIdx = i
				}
			}
		}
	}

	return bestIdx
}

// mergeUnderfilledBins merges bins that are below the minimum fill threshold.
// Objects below 70% of target size are merged with other objects. This allows
// the builder to create better-filled actual objects.
//
// Example: Let us assume our target size is 1GB. Given 3 objects of 600MB each:
//   - Without merging underfilled bins:
//   - We will end up with 2 bins, one with two 600MB objects and another with one 600MB object.
//   - No matter how much we try, the output objects from first bin would be overfilled or underfilled.
//   - The output object from second bin would be below the 70% of target size.
//   - With merging underfilled bins:
//   - We will end up with a single bin with three 600MB objects to merge.
//   - We will then create 2 output objects from the single bin, each above the 70% of target size.
func mergeUnderfilledBins[G Sizer](bins []BinPackResult[G]) []BinPackResult[G] {
	if len(bins) <= 1 {
		return bins
	}

	minDesiredSize := targetUncompressedSize * minFillPercent / 100
	maxAllowedSize := targetUncompressedSize * maxOutputMultiple

	// Sort by size (smallest first) to process under-filled bins first
	sort.Slice(bins, func(i, j int) bool {
		return bins[i].Size < bins[j].Size
	})

	// Use write index to compact the slice as we merge
	writeIdx := 0
	for readIdx := 0; readIdx < len(bins); readIdx++ {
		bin := &bins[readIdx]

		// If this bin is well-filled, keep it as is
		if bin.Size >= minDesiredSize {
			bins[writeIdx] = *bin
			writeIdx++
			continue
		}

		// Try to merge with another bin (look for best fit)
		bestMergeIdx := -1
		bestFillRemainder := int64(0)

		for i := readIdx + 1; i < len(bins); i++ {
			candidate := &bins[i]
			combinedSize := bin.Size + candidate.Size

			// Only merge if combined size is within max allowed
			if combinedSize <= maxAllowedSize {
				// Calculate how well-filled the last object would be after builder splits
				remainder := combinedSize % targetUncompressedSize
				if remainder == 0 {
					remainder = targetUncompressedSize // Perfect fit = 100%
				}

				// Prefer merges that result in better fill ratio
				if remainder > bestFillRemainder {
					bestMergeIdx = i
					bestFillRemainder = remainder
				}
			}
		}

		if bestMergeIdx >= 0 {
			// Merge the bins
			candidate := &bins[bestMergeIdx]
			bin.Groups = append(bin.Groups, candidate.Groups...)
			bin.Size += candidate.Size

			// Remove the merged candidate
			bins[bestMergeIdx] = bins[len(bins)-1]
			bins = bins[:len(bins)-1]

			// Re-sort remaining unprocessed bins
			if bestMergeIdx < len(bins) {
				remaining := bins[readIdx+1:]
				sort.Slice(remaining, func(i, j int) bool {
					return remaining[i].Size < remaining[j].Size
				})
			}

			// Check if merged bin is still under-filled
			if bin.Size < minDesiredSize {
				readIdx-- // Process this bin again
				continue
			}
		}

		bins[writeIdx] = *bin
		writeIdx++
	}

	return bins[:writeIdx]
}

// countObjects returns the total number of output objects that will be created.
func countObjects(outputObjects []*compactionpb.SingleTenantObjectSource) int32 {
	var total int32
	for _, obj := range outputObjects {
		total += obj.NumOutputObjects
	}
	return total
}

// prorateSize calculates a prorated size based on the ratio of a partial duration
// to the total duration. If totalDuration is zero, returns the full size.
func prorateSize(totalSize int64, partialDuration, totalDuration time.Duration) int64 {
	if totalDuration <= 0 {
		return totalSize
	}
	ratio := float64(partialDuration) / float64(totalDuration)
	return int64(float64(totalSize) * ratio)
}

// calculateNumOutputObjects returns the number of output objects needed for a given size.
func calculateNumOutputObjects(size int64) int32 {
	numObjects := size / targetUncompressedSize
	if size%targetUncompressedSize > 0 {
		numObjects++
	}
	return int32(numObjects)
}

// convertLeftoverBinsToMultiTenantObjectSource converts bin-pack results to proto MultiTenantObjectSource.
func convertLeftoverBinsToMultiTenantObjectSource(bins []BinPackResult[*LeftoverStreamGroup]) ([]*compactionpb.MultiTenantObjectSource, int64) {
	if len(bins) == 0 {
		return nil, 0
	}

	objects := make([]*compactionpb.MultiTenantObjectSource, len(bins))
	var totalSize int64

	for i, bin := range bins {
		totalSize += bin.Size

		var tenantStreams []*compactionpb.TenantStream
		for _, group := range bin.Groups {
			for j := range group.Streams {
				stream := &group.Streams[j]
				tenantStreams = append(tenantStreams, &stream.TenantStream)
			}
		}

		objects[i] = &compactionpb.MultiTenantObjectSource{
			TenantStreams:    tenantStreams,
			NumOutputObjects: calculateNumOutputObjects(bin.Size),
		}
	}

	return objects, totalSize
}
