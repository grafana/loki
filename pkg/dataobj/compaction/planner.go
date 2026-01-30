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
	// Streams contains all the stream entries for this stream (from different indexes).
	Streams []*compactionpb.Stream
	// TotalUncompressedSize is the sum of uncompressed sizes across all streams.
	TotalUncompressedSize int64
}

// GetSize implements Sizer interface for bin-packing.
func (g *StreamGroup) GetSize() int64 { return g.TotalUncompressedSize }

// StreamInfo represents aggregated stream information from an index object.
type StreamInfo struct {
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
	Streams               []*compactionpb.TenantStream
	TotalUncompressedSize int64
}

// GetSize implements Sizer interface for bin-packing.
func (g *LeftoverStreamGroup) GetSize() int64 { return g.TotalUncompressedSize }

// LeftoverStreamInfo represents a stream's leftover data outside the compaction window.
type LeftoverStreamInfo struct {
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

	// Build plans and collect leftovers
	tenantPlans := make(map[string]*CompactionPlan)
	var allLeftoverBefore, allLeftoverAfter []*LeftoverStreamGroup

	for tenant, tenantIndexes := range indexesByTenant {
		level.Info(util_log.Logger).Log("msg", "Building plan for tenant", "tenant", tenant, "num_indexes", len(tenantIndexes))

		plan, err := s.buildTenantPlan(ctx, tenant, tenantIndexes, windowStart, windowEnd)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to build plan for tenant %s: %w", tenant, err)
		}
		tenantPlans[tenant] = plan

		// Collect leftover streams from all tenants
		allLeftoverBefore = append(allLeftoverBefore, plan.LeftoverBeforeStreams...)
		allLeftoverAfter = append(allLeftoverAfter, plan.LeftoverAfterStreams...)
	}

	leftoverPlan := s.planLeftovers(allLeftoverBefore, allLeftoverAfter)

	return tenantPlans, leftoverPlan, nil
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
					group = &LeftoverStreamGroup{}
					leftoverBeforeMap[info.LabelsHash] = group
				}
				group.Streams = append(group.Streams, &info.TenantStream)
				group.TotalUncompressedSize += info.UncompressedSize
			}

			// Group leftover after streams by labels hash
			for _, info := range result.LeftoverAfterStreams {
				group, ok := leftoverAfterMap[info.LabelsHash]
				if !ok {
					group = &LeftoverStreamGroup{}
					leftoverAfterMap[info.LabelsHash] = group
				}
				group.Streams = append(group.Streams, &info.TenantStream)
				group.TotalUncompressedSize += info.UncompressedSize
			}

			// Group streams by labels hash
			for _, info := range result.Streams {
				group, ok := streamGroupMap[info.LabelsHash]
				if !ok {
					group = &StreamGroup{}
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
							ID:    stream.ID,
							Index: indexPath,
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
							ID:    stream.ID,
							Index: indexPath,
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
					ID:    stream.ID,
					Index: indexPath,
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
			tenantStreams = append(tenantStreams, group.Streams...)
		}

		objects[i] = &compactionpb.MultiTenantObjectSource{
			TenantStreams:    tenantStreams,
			NumOutputObjects: calculateNumOutputObjects(bin.Size),
		}
	}

	return objects, totalSize
}
