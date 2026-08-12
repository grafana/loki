package executor

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/go-kit/log/level"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	dataobjindex "github.com/grafana/loki/v3/pkg/dataobj/index"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/dataobj/sortmerge"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
)

func (c *Context) executeLogMerge(node *physical.LogMerge) Pipeline {
	return newLazyPipeline(func(ctx context.Context, _ []Pipeline) Pipeline {
		arts, err := c.doLogObjectMerge(ctx, node)
		if err != nil {
			return errorPipeline(ctx, err)
		}
		return NewBufferedPipeline(v2.BuildResultRecord(memory.DefaultAllocator, arts))
	}, nil)
}

// dataObjectBucket returns the bucket for reading source log objects and writing
// compacted log objects. Both live at the unprefixed dataobj root (the objects/
// namespace), not under the index-storage prefix that c.bucket carries, so it
// prefers dataBucket and falls back to bucket when dataBucket is unset (e.g.
// query-only workers or tests that share a single bucket).
func (c *Context) dataObjectBucket() objstore.Bucket {
	if c.dataBucket != nil {
		return c.dataBucket
	}
	return c.bucket
}

func (c *Context) doLogObjectMerge(ctx context.Context, node *physical.LogMerge) ([]v2.ResultArtifact, error) {
	start := time.Now()
	if c.bucket == nil {
		return nil, errors.New("no object store bucket configured")
	}

	sources, err := c.collectLogSources(ctx, node)
	if err != nil {
		return nil, err
	}
	if len(sources) == 0 {
		c.observeLogMerge(node.Tenant, logMergeObservedStats{Outcome: logMergeOutcomeEmpty}, time.Since(start))
		return nil, fmt.Errorf("LogMerge: no source log sections for tenant %q", node.Tenant)
	}
	if node.SortOnly {
		return c.doLogObjectSort(ctx, node, sources, start)
	}
	if err := validateLogSourceLayouts(ctx, sources, node); err != nil {
		return nil, err
	}

	table, err := buildGlobalStreamTable(sources, node.SortSchema)
	if err != nil {
		return nil, err
	}

	indexBuilder, err := indexobj.NewBuilder(c.indexobjCfg, c.scratchStore)
	if err != nil {
		return nil, fmt.Errorf("creating index builder: %w", err)
	}
	calc := dataobjindex.NewCalculator(indexBuilder)

	sections, remaps := sectionsWithRemaps(sources, table)
	merged, err := sortmerge.IteratorWithStreamRemap(ctx, sections, remaps, table.shards, table.sortKeys, node.SortSchema)
	if err != nil {
		return nil, fmt.Errorf("starting k-way log merge: %w", err)
	}

	// Consume the globally-sorted stream and build compacted object
	w, err := c.newLogObjectWriter(node, table, calc)
	if err != nil {
		return nil, err
	}
	lastSortKey := "unknown"
	for res := range merged {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		rec, err := res.Value()
		if err != nil {
			return nil, err
		}
		if rec.SortKey != lastSortKey {
			lastSortKey = rec.SortKey
		}
		if err := w.add(ctx, rec); err != nil {
			return nil, err
		}
	}
	stats, err := w.finish(ctx)
	if err != nil {
		return nil, err
	}
	if stats.OutputObjects == 0 {
		c.observeLogMerge(node.Tenant, logMergeObservedStats{Outcome: logMergeOutcomeEmpty}, time.Since(start))
		return nil, fmt.Errorf("LogMerge: produced no compacted objects for tenant %q", node.Tenant)
	}

	idxObj, idxCloser, _, err := calc.Flush()
	if err != nil {
		return nil, fmt.Errorf("flushing index: %w", err)
	}

	idxPathReader, err := idxObj.Reader(ctx)
	if err != nil {
		return nil, errors.Join(err, idxCloser.Close())
	}
	idxPath, hashErr := v2.CompactedIndexPath(node.Tenant, idxPathReader)
	if cerr := idxPathReader.Close(); cerr != nil && hashErr == nil {
		hashErr = cerr
	}
	if hashErr != nil {
		return nil, errors.Join(hashErr, idxCloser.Close())
	}

	if _, upErr := c.uploadObject(ctx, c.bucket, idxPath, idxObj); upErr != nil {
		return nil, errors.Join(fmt.Errorf("uploading index %q: %w", idxPath, upErr), idxCloser.Close())
	}

	if err := idxCloser.Close(); err != nil {
		return nil, fmt.Errorf("closing index %q: %w", idxPath, err)
	}

	stats.Outcome = logMergeOutcomeSuccess
	stats.SourceObjects = len(sources)
	for _, s := range sources {
		stats.InputSections += len(s.logsSections)
	}

	level.Info(c.logger).Log(
		"msg", "LogMerge: built compacted log object(s)",
		"tenant", node.Tenant,
		"source_objects", stats.SourceObjects,
		"input_sections", stats.InputSections,
		"output_objects", stats.OutputObjects,
		"output_streams", stats.OutputStreams,
		"output_records", stats.OutputRecords,
		"output_bytes", stats.OutputBytesCompressed,
		"output_bytes_uncompressed", stats.OutputBytesUncompressed,
		"sort_schema", strings.Join(node.SortSchema, ","),
		"duration", time.Since(start),
	)

	c.observeLogMerge(node.Tenant, stats.logMergeObservedStats, time.Since(start))
	return []v2.ResultArtifact{{Path: idxPath}}, nil
}

const (
	logMergeOutcomeSuccess = "success"
	logMergeOutcomeEmpty   = "empty"
)

// LogMergeObservedStats is the per-task compaction summary reported to
// LogMergeObserver and xcap statistics.
type LogMergeObservedStats struct {
	Outcome                 string
	SourceObjects           int
	InputSections           int
	OutputObjects           int
	OutputStreams           int
	OutputRecords           int
	OutputBytesCompressed   int64
	OutputBytesUncompressed int64
	OutputObjectPaths       []string
}

// logMergeObservedStats is the internal alias used while assembling stats.
type logMergeObservedStats = LogMergeObservedStats

// logMergeStats summarizes a completed LogMerge for the reference log line.
type logMergeStats struct {
	logMergeObservedStats
}

func (c *Context) observeLogMerge(tenant string, stats logMergeObservedStats, duration time.Duration) {
	if c.logMergeObserver != nil {
		c.logMergeObserver.ObserveLogMerge(tenant, stats, duration)
	}
}

type logSource struct {
	path              string
	logsSections      []*dataobj.Section
	totalLogsSections int
	streams           map[int64]streams.Stream
}

// collectLogSources opens every unique source object referenced by node.Runs and
// returns only the referenced logs sections plus the object's localStreamID->stream map.
func (c *Context) collectLogSources(ctx context.Context, node *physical.LogMerge) ([]*logSource, error) {
	type sourceRef struct {
		path           string
		sectionIndexes []int64
	}
	type sectionKey struct {
		path  string
		index int64
	}

	var refs []sourceRef
	refsByPath := make(map[string]int)
	seenSections := make(map[sectionKey]struct{})
	for _, run := range node.Runs {
		if run == nil {
			continue
		}
		for _, sec := range run.Sections {
			if sec == nil {
				continue
			}
			key := sectionKey{path: sec.ObjectPath, index: sec.SectionIndex}
			if _, ok := seenSections[key]; ok {
				return nil, fmt.Errorf("duplicate log section reference %q section %d", sec.ObjectPath, sec.SectionIndex)
			}
			seenSections[key] = struct{}{}

			refIdx, ok := refsByPath[sec.ObjectPath]
			if !ok {
				refIdx = len(refs)
				refsByPath[sec.ObjectPath] = refIdx
				refs = append(refs, sourceRef{path: sec.ObjectPath})
			}
			refs[refIdx].sectionIndexes = append(refs[refIdx].sectionIndexes, sec.SectionIndex)
		}
	}
	srcBucket := c.dataObjectBucket()

	// Gather log and streams sections
	sources := make([]*logSource, 0, len(refs))
	for _, ref := range refs {
		obj, err := dataobj.FromBucket(ctx, srcBucket, ref.path, 0)
		if err != nil {
			return nil, fmt.Errorf("opening object %q: %w", ref.path, err)
		}

		var allLogs []*dataobj.Section
		for _, sec := range obj.Sections().Filter(logs.CheckSection) {
			if sec.Tenant == node.Tenant {
				allLogs = append(allLogs, sec)
			}
		}

		logsSections := make([]*dataobj.Section, 0, len(ref.sectionIndexes))
		for _, sectionIndex := range ref.sectionIndexes {
			if sectionIndex < 0 || sectionIndex >= int64(len(allLogs)) {
				return nil, fmt.Errorf("object %q log section index %d out of range [0,%d)", ref.path, sectionIndex, len(allLogs))
			}
			section := allLogs[sectionIndex]
			logsSections = append(logsSections, section)
		}

		var streamSections []*dataobj.Section
		for _, sec := range obj.Sections() {
			if sec.Tenant != node.Tenant {
				continue
			}
			if streams.CheckSection(sec) {
				streamSections = append(streamSections, sec)
			}
		}

		if len(streamSections) == 0 {
			return nil, fmt.Errorf("object %q has logs sections but no streams section for tenant %q", ref.path, node.Tenant)
		}
		if len(streamSections) > 1 {
			return nil, fmt.Errorf("object %q has %d streams sections for tenant %q, expected exactly one", ref.path, len(streamSections), node.Tenant)
		}

		srcStreams, err := resolveStreams(ctx, streamSections[0])
		if err != nil {
			return nil, fmt.Errorf("resolving streams for object %q: %w", ref.path, err)
		}

		sources = append(sources, &logSource{
			path:              ref.path,
			logsSections:      logsSections,
			totalLogsSections: len(allLogs),
			streams:           srcStreams,
		})
	}

	return sources, nil
}

func validateLogSourceLayouts(ctx context.Context, sources []*logSource, node *physical.LogMerge) error {
	if node.StreamOrder == compactionv2pb.STREAM_ORDER_UNSPECIFIED {
		return nil
	}
	for _, source := range sources {
		for _, section := range source.logsSections {
			logsSection, err := logs.Open(ctx, section)
			if err != nil {
				return fmt.Errorf("opening logs section from %q: %w", source.path, err)
			}
			layout := logsSection.SortLayout()
			if !slices.Equal(layout.SchemaLabels, node.SortSchema) ||
				layout.StreamOrder != logs.StreamOrderStableHashV1 ||
				layout.ShardCount != node.ShardCount {
				return fmt.Errorf(
					"stale LogMerge plan: object %q has layout schema=%v stream_order=%d shard_count=%d, expected schema=%v stream_order=%s shard_count=%d",
					source.path, layout.SchemaLabels, layout.StreamOrder, layout.ShardCount,
					node.SortSchema, node.StreamOrder, node.ShardCount,
				)
			}
		}
	}
	return nil
}

func (c *Context) doLogObjectSort(ctx context.Context, node *physical.LogMerge, sources []*logSource, start time.Time) ([]v2.ResultArtifact, error) {
	if len(sources) != 1 {
		return nil, fmt.Errorf("sort-only LogMerge requires exactly one physical object, got %d", len(sources))
	}
	if len(sources[0].logsSections) != sources[0].totalLogsSections {
		return nil, fmt.Errorf(
			"sort-only LogMerge requires all tenant LOG sections from object %q, got %d of %d",
			sources[0].path, len(sources[0].logsSections), sources[0].totalLogsSections,
		)
	}
	if node.StreamOrder != compactionv2pb.STREAM_ORDER_STABLE_HASH_V1 || node.ShardCount != streams.ShardFactor {
		return nil, fmt.Errorf("sort-only LogMerge requires stable-hash-v1 with shard_count=%d", streams.ShardFactor)
	}

	table, err := buildGlobalStreamTable(sources, node.SortSchema)
	if err != nil {
		return nil, err
	}

	objectBuilder := dataobj.NewBuilder(c.scratchStore)
	streamsBuilder := streams.NewBuilder(streams.NewMetrics(), int(c.indexobjCfg.TargetPageSize), c.indexobjCfg.MaxPageRows)
	streamsBuilder.SetTenant(node.Tenant)
	for _, stream := range table.streams[1:] {
		streamsBuilder.AppendValue(stream)
	}
	if err := objectBuilder.Append(streamsBuilder); err != nil {
		return nil, fmt.Errorf("building sorted streams section: %w", err)
	}

	newLogsBuilder := func() *logs.Builder {
		builder := logs.NewBuilder(logs.NewMetrics(), logs.BuilderOptions{
			PageSizeHint:     int(c.indexobjCfg.TargetPageSize),
			PageMaxRowCount:  c.indexobjCfg.MaxPageRows,
			BufferSize:       int(c.indexobjCfg.BufferSize),
			StripeMergeLimit: c.indexobjCfg.SectionStripeMergeLimit,
			AppendStrategy:   logs.AppendUnordered,
			SortOrder:        logs.SortSchemaASC,
			SchemaLabels:     node.SortSchema,
			StreamOrder:      logs.StreamOrderStableHashV1,
			ShardCount:       streams.ShardFactor,
			SchemaSortKeys:   table.sortKeys,
			SchemaShards:     table.shards,
		})
		builder.SetTenant(node.Tenant)
		return builder
	}

	logsBuilder := newLogsBuilder()
	var records int
	for _, section := range sources[0].logsSections {
		opened, err := logs.Open(ctx, section)
		if err != nil {
			return nil, fmt.Errorf("opening logs section from %q: %w", sources[0].path, err)
		}
		for result := range logs.IterSection(ctx, opened) {
			record, err := result.Value()
			if err != nil {
				return nil, err
			}
			globalID, ok := table.streamIDRemaps[0][record.StreamID]
			if !ok {
				return nil, fmt.Errorf("object %q record references unknown stream ID %d", sources[0].path, record.StreamID)
			}
			record.StreamID = globalID
			record.SortKey = table.sortKeys[globalID]
			record.ShardHash = int64(table.shards[globalID])
			record.Line = slices.Clone(record.Line)
			record.Metadata = record.Metadata.Copy()
			logsBuilder.Append(record)
			records++
		}
	}
	if records == 0 {
		return nil, fmt.Errorf("sort-only LogMerge found no records for tenant %q", node.Tenant)
	}
	if logsBuilder.UncompressedSize() > 0 {
		if err := objectBuilder.Append(logsBuilder); err != nil {
			return nil, fmt.Errorf("building sorted logs section: %w", err)
		}
	}

	obj, closer, err := objectBuilder.Flush()
	if err != nil {
		return nil, fmt.Errorf("flushing sorted object: %w", err)
	}
	pathReader, err := obj.Reader(ctx)
	if err != nil {
		return nil, errors.Join(err, closer.Close())
	}
	path, pathErr := v2.CompactedLogObjectPath(node.Tenant, pathReader)
	if closeErr := pathReader.Close(); closeErr != nil && pathErr == nil {
		pathErr = closeErr
	}
	if pathErr != nil {
		return nil, errors.Join(pathErr, closer.Close())
	}
	size, err := c.uploadObject(ctx, c.dataObjectBucket(), path, obj)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("uploading sorted object %q: %w", path, err), closer.Close())
	}

	indexBuilder, err := indexobj.NewBuilder(c.indexobjCfg, c.scratchStore)
	if err != nil {
		return nil, errors.Join(err, closer.Close())
	}
	calc := dataobjindex.NewCalculator(indexBuilder)
	if err := calc.Calculate(ctx, c.logger, obj, path); err != nil {
		return nil, errors.Join(fmt.Errorf("indexing sorted object %q: %w", path, err), closer.Close())
	}
	if err := closer.Close(); err != nil {
		return nil, fmt.Errorf("closing sorted object %q: %w", path, err)
	}

	idxObj, idxCloser, _, err := calc.Flush()
	if err != nil {
		return nil, fmt.Errorf("flushing sorted-object index: %w", err)
	}
	idxReader, err := idxObj.Reader(ctx)
	if err != nil {
		return nil, errors.Join(err, idxCloser.Close())
	}
	idxPath, idxPathErr := v2.CompactedIndexPath(node.Tenant, idxReader)
	if closeErr := idxReader.Close(); closeErr != nil && idxPathErr == nil {
		idxPathErr = closeErr
	}
	if idxPathErr != nil {
		return nil, errors.Join(idxPathErr, idxCloser.Close())
	}
	if _, err := c.uploadObject(ctx, c.bucket, idxPath, idxObj); err != nil {
		return nil, errors.Join(fmt.Errorf("uploading sorted-object index %q: %w", idxPath, err), idxCloser.Close())
	}
	if err := idxCloser.Close(); err != nil {
		return nil, fmt.Errorf("closing sorted-object index %q: %w", idxPath, err)
	}

	level.Info(c.logger).Log(
		"msg", "LogMerge: uploaded sort-only object",
		"tenant", node.Tenant,
		"source_object", sources[0].path,
		"path", path,
		"records", records,
		"bytes", size,
		"duration", time.Since(start),
	)
	return []v2.ResultArtifact{{Path: idxPath}}, nil
}

// resolveStreams decodes a streams section into a map from local stream ID to its
// stream (labels + aggregates). Labels are deep-copied so they remain valid after
// the underlying reader buffers are reused.
func resolveStreams(ctx context.Context, section *dataobj.Section) (map[int64]streams.Stream, error) {
	sec, err := streams.Open(ctx, section)
	if err != nil {
		return nil, fmt.Errorf("opening streams section: %w", err)
	}

	out := make(map[int64]streams.Stream)
	for res := range streams.IterSection(ctx, sec) {
		stream, err := res.Value()
		if err != nil {
			return nil, err
		}
		stream.Labels = stream.Labels.Copy()
		out[stream.ID] = stream
	}
	return out, nil
}

// globalStreamTable holds the disjoint global stream assignment for a merge
type globalStreamTable struct {
	sortKeys       []string          // index = global ID (1..N); [0] unused
	shards         []uint32          // index = global ID; shard precedes schema in the physical order
	streams        []streams.Stream  // index = global ID; source stream with aggregates
	streamIDRemaps []map[int64]int64 // per source object (by index): sourceStreamID -> globalID
}

// buildGlobalStreamTable computes the global stream assignment from all sources.
func buildGlobalStreamTable(sources []*logSource, sortSchema []string) (*globalStreamTable, error) {
	type entry struct {
		key    logsobj.StreamOrderKey
		stream streams.Stream
	}

	byLabels := make(map[string]entry)
	for _, src := range sources {
		for _, s := range src.streams {
			key, err := logsobj.NewStreamOrderKey(s.Labels, sortSchema)
			if err != nil {
				return nil, fmt.Errorf("computing sort key for object %q: %w", src.path, err)
			}
			labelKey := s.Labels.String()
			if _, exists := byLabels[labelKey]; !exists {
				byLabels[labelKey] = entry{key: key, stream: s}
			}
		}
	}

	allEntries := make([]entry, 0, len(byLabels))
	for _, e := range byLabels {
		allEntries = append(allEntries, e)
	}
	slices.SortFunc(allEntries, func(a, b entry) int {
		return logsobj.CompareStreamOrderKey(a.key, b.key)
	})

	table := &globalStreamTable{
		sortKeys:       make([]string, len(allEntries)+1),
		shards:         make([]uint32, len(allEntries)+1),
		streams:        make([]streams.Stream, len(allEntries)+1),
		streamIDRemaps: make([]map[int64]int64, len(sources)),
	}
	globalIDs := make(map[string]int64, len(allEntries))
	for i, e := range allEntries {
		gid := int64(i + 1)
		table.sortKeys[gid] = e.key.SchemaKey
		table.shards[gid] = e.key.Shard
		s := e.stream
		s.ID = gid
		s.ShardHash = int64(e.key.Shard)
		table.streams[gid] = s
		globalIDs[s.Labels.String()] = gid
	}
	for sourceIdx, src := range sources {
		table.streamIDRemaps[sourceIdx] = make(map[int64]int64, len(src.streams))
		for sourceStreamID, s := range src.streams {
			table.streamIDRemaps[sourceIdx][sourceStreamID] = globalIDs[s.Labels.String()]
		}
	}
	return table, nil
}

// sectionsWithRemaps flattens the sources' logs sections
func sectionsWithRemaps(sources []*logSource, table *globalStreamTable) ([]*dataobj.Section, []map[int64]int64) {
	var (
		sections []*dataobj.Section
		remaps   []map[int64]int64
	)
	for sourceIdx, src := range sources {
		for _, sec := range src.logsSections {
			sections = append(sections, sec)
			remaps = append(remaps, table.streamIDRemaps[sourceIdx])
		}
	}
	return sections, remaps
}

// logObjectWriter consumes the globally-sorted merged record stream and builds
// one or more compacted log objects, split at TargetObjectSize (never splitting a
// stream across objects)
type logObjectWriter struct {
	c     *Context
	node  *physical.LogMerge
	table *globalStreamTable
	calc  *dataobjindex.Calculator

	builder *logsobj.Builder
	sorter  *logsobj.Builder

	objSize        int
	objRecords     int
	currentSortKey string
	hasSortKey     bool

	stats logMergeStats
}

type fixedSortSchema []string

func (s fixedSortSchema) SortSchemaLabels(string) []string { return s }

func (c *Context) newLogObjectWriter(node *physical.LogMerge, table *globalStreamTable, calc *dataobjindex.Calculator) (*logObjectWriter, error) {
	builderCfg := c.indexobjCfg

	cfg := logsobj.BuilderConfig{
		BuilderBaseConfig:    builderCfg,
		AppendOrderedEnabled: true,
		DataobjUseSortSchema: true,
		SchemaStreamOrder:    logs.StreamOrderStableHashV1,
	}
	overrides := fixedSortSchema(node.SortSchema)
	builder, err := logsobj.NewBuilder(cfg, c.scratchStore, logsobj.NewBuilderMetrics(), c.logger, overrides)
	if err != nil {
		return nil, fmt.Errorf("creating compacted logs builder: %w", err)
	}
	sorter, err := logsobj.NewBuilder(cfg, c.scratchStore, logsobj.NewBuilderMetrics(), c.logger, overrides)
	if err != nil {
		return nil, fmt.Errorf("creating compacted logs sorter: %w", err)
	}
	return &logObjectWriter{
		c:       c,
		node:    node,
		table:   table,
		calc:    calc,
		builder: builder,
		sorter:  sorter,
	}, nil
}

// add appends one merged record (carrying a global stream ID), rolling to a new
// output object at sort-key boundaries once the logsobj builder reaches its
// target size. Keeping a complete sort-key group together ensures duplicate
// logical streams cannot be split across output objects.
func (w *logObjectWriter) add(ctx context.Context, rec logs.Record) error {
	if w.objRecords > 0 && w.hasSortKey && rec.SortKey != w.currentSortKey && w.builder.IsFull() {
		if err := w.finalizeAndUpload(ctx); err != nil {
			return err
		}
	}

	if rec.StreamID <= 0 || rec.StreamID >= int64(len(w.table.streams)) {
		return fmt.Errorf("merged record references invalid global stream ID %d", rec.StreamID)
	}
	stream := w.table.streams[rec.StreamID]
	if err := w.builder.AppendRecord(w.node.Tenant, stream.Labels, rec, rec.Timestamp); err != nil {
		return fmt.Errorf("appending compacted log record: %w", err)
	}

	w.currentSortKey = rec.SortKey
	w.hasSortKey = true
	w.objRecords++
	w.objSize += logRecordSize(rec)
	return nil
}

// finish flushes and uploads the last in-progress object (if any) and returns the
// accumulated stats.
func (w *logObjectWriter) finish(ctx context.Context) (logMergeStats, error) {
	if w.objRecords > 0 {
		if err := w.finalizeAndUpload(ctx); err != nil {
			return w.stats, err
		}
	}
	return w.stats, nil
}

// finalizeAndUpload flushes the deduplicating logsobj builder, restores
// object-wide ordering after stream ID reassignment, computes the content-hash
// path, and uploads the compacted object to the data bucket.
func (w *logObjectWriter) finalizeAndUpload(ctx context.Context) error {
	intermediate, intermediateCloser, err := w.builder.Flush()
	if err != nil {
		return fmt.Errorf("flushing object: %w", err)
	}

	obj, closer := intermediate, intermediateCloser
	if w.node.StreamOrder == compactionv2pb.STREAM_ORDER_UNSPECIFIED {
		obj, closer, err = w.sorter.CopyAndSort(ctx, intermediate)
		if err != nil {
			return errors.Join(fmt.Errorf("sorting compacted object: %w", err), intermediateCloser.Close())
		}
		if err := intermediateCloser.Close(); err != nil {
			return errors.Join(fmt.Errorf("closing unsorted compacted object: %w", err), closer.Close())
		}
	}

	pathReader, err := obj.Reader(ctx)
	if err != nil {
		return errors.Join(err, closer.Close())
	}
	path, hashErr := v2.CompactedLogObjectPath(w.node.Tenant, pathReader)
	if cerr := pathReader.Close(); cerr != nil && hashErr == nil {
		hashErr = cerr
	}
	if hashErr != nil {
		return errors.Join(hashErr, closer.Close())
	}

	objStreams, err := countTenantStreams(ctx, obj, w.node.Tenant)
	if err != nil {
		return errors.Join(err, closer.Close())
	}

	size, upErr := w.c.uploadObject(ctx, w.c.dataObjectBucket(), path, obj)
	if upErr != nil {
		return errors.Join(fmt.Errorf("uploading %q: %w", path, upErr), closer.Close())
	}

	// Build the index over the just-written object while it is still in memory.
	if err := w.calc.Calculate(ctx, w.c.logger, obj, path); err != nil {
		return errors.Join(fmt.Errorf("indexing %q: %w", path, err), closer.Close())
	}

	if err := closer.Close(); err != nil {
		return fmt.Errorf("closing compacted object %q: %w", path, err)
	}

	level.Info(w.c.logger).Log(
		"msg", "LogMerge: uploaded compacted log object",
		"tenant", w.node.Tenant,
		"path", path,
		"object_index", w.stats.OutputObjects,
		"streams", objStreams,
		"records", w.objRecords,
		"bytes", size,
	)
	w.stats.OutputObjectPaths = append(w.stats.OutputObjectPaths, path)
	w.stats.OutputObjects++
	w.stats.OutputStreams += objStreams
	w.stats.OutputRecords += w.objRecords
	w.stats.OutputBytesCompressed += size
	w.stats.OutputBytesUncompressed += int64(w.objSize)
	w.objSize = 0
	w.objRecords = 0
	w.currentSortKey = ""
	w.hasSortKey = false
	return nil
}

func countTenantStreams(ctx context.Context, obj *dataobj.Object, tenant string) (int, error) {
	var count int
	for _, section := range obj.Sections().Filter(streams.CheckSection) {
		if section.Tenant != tenant {
			continue
		}
		streamSection, err := streams.Open(ctx, section)
		if err != nil {
			return 0, fmt.Errorf("opening compacted streams section: %w", err)
		}
		count += streamSection.NumRows()
	}
	return count, nil
}

// logRecordSize approximates a record's uncompressed footprint the same way the
// ingest builder does: line length plus structured-metadata value lengths.
func logRecordSize(rec logs.Record) int {
	size := len(rec.Line)
	rec.Metadata.Range(func(l labels.Label) {
		size += len(l.Value)
	})
	return size
}

// uploadObject streams a built object to the given bucket and returns its encoded
// size. The index object goes to the index bucket; compacted log objects go to
// the data bucket.
func (c *Context) uploadObject(ctx context.Context, bucket objstore.Bucket, path string, obj *dataobj.Object) (int64, error) {
	reader, err := obj.Reader(ctx)
	if err != nil {
		return 0, fmt.Errorf("getting object reader: %w", err)
	}
	defer reader.Close()

	if err := bucket.Upload(ctx, path, reader); err != nil {
		return 0, err
	}
	return obj.Size(), nil
}
