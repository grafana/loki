package executor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/go-kit/log/level"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
	dataobjindex "github.com/grafana/loki/v3/pkg/dataobj/index"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/dataobj/sortmerge"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/util"
)

var errNoSourceObjects = errors.New("no source objects found")

func (c *Context) executeLogMerge(node *physical.LogMerge) Pipeline {
	return newLazyPipeline(func(ctx context.Context) Pipeline {
		artifact, err := c.doLogObjectMerge(ctx, node)
		if err != nil {
			return errorPipeline(ctx, err)
		}
		if artifact == nil {
			return errorPipeline(ctx, errors.New("LogMerge: missing result artifact"))
		}
		rec, err := artifact.ToRecordBatch(memory.DefaultAllocator)
		if err != nil {
			return errorPipeline(ctx, err)
		}
		return NewBufferedPipeline(rec)
	})
}

// dataObjectBucket returns the bucket for reading source log objects and writing
// compacted log objects. Both live at the unprefixed dataobj root (the objects/
// namespace), not under the index-storage prefix that c.bucket carries, so it
// prefers dataBucket and falls back to bucket when dataBucket is unset (e.g.
// tests that share a single bucket).
func (c *Context) dataObjectBucket() objstore.Bucket {
	if c.dataBucket != nil {
		return c.dataBucket
	}
	return c.bucket
}

func (c *Context) doLogObjectMerge(ctx context.Context, node *physical.LogMerge) (*v2.ResultArtifact, error) {
	start := time.Now()
	if c.bucket == nil {
		return nil, errors.New("no object store bucket configured")
	}

	inputs, err := c.prepareLogMergeInputs(ctx, node)
	if err != nil {
		if errors.Is(err, errNoSourceObjects) {
			c.logMergeObserver.ObserveLogMerge(node.Tenant, logMergeObservedStats{Outcome: logMergeOutcomeEmpty}, time.Since(start))
		}

		return nil, err
	}

	indexBuilder, err := indexobj.NewBuilder(node.Tenant, c.indexobjCfg, c.scratchStore, indexobj.NewBuilderMetrics(nil))
	if err != nil {
		return nil, fmt.Errorf("creating index builder: %w", err)
	}
	calc := dataobjindex.NewCalculator(indexBuilder, dataobjindex.NewCalculatorMetrics(nil))

	merged := sortmerge.MixedRunIterator(ctx, inputs.runs)

	// Consume the globally-sorted stream and build compacted object
	w, err := c.newLogObjectWriter(node, inputs.layout, inputs.table, calc)
	if err != nil {
		return nil, err
	}
	var (
		inputBytesTotal int64
		dups            duplicateCounter
		inputBytesBatch int64
		count           int
		keptRecords     int64
	)
	batchRecords := 1000
	observeBatch := func() {
		c.logMergeObserver.ObserveLogMergeInputBytes(inputBytesBatch)
		inputBytesTotal += inputBytesBatch
	}
	for res := range merged {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		rec, err := res.Value()
		if err != nil {
			return nil, err
		}
		size := rec.UncompressedSize()
		inputBytesBatch += size
		count++
		if count%batchRecords == 0 {
			observeBatch()
			inputBytesBatch = 0
			count = 0
		}
		dups.observe(rec)
		if err := w.add(ctx, rec); err != nil {
			return nil, err
		}
		keptRecords++
	}
	// Count any remaining records that haven't been added to the metric yet
	observeBatch()
	stats, err := w.finish(ctx)
	if err != nil {
		return nil, err
	}
	if stats.OutputObjects == 0 {
		c.logMergeObserver.ObserveLogMerge(node.Tenant, logMergeObservedStats{Outcome: logMergeOutcomeEmpty}, time.Since(start))
		return nil, fmt.Errorf("LogMerge: produced no compacted objects for tenant %q", node.Tenant)
	}

	idxPath, err := c.flushAndUploadIndex(ctx, node.Tenant, calc)
	if err != nil {
		return nil, err
	}

	stats.Outcome = logMergeOutcomeSuccess
	stats.SourceObjects = len(inputs.sources)
	for _, run := range inputs.runs {
		stats.InputSections += len(run)
	}
	stats.InputBytes = inputBytesTotal
	stats.DuplicateRecords = dups.duplicates

	duration := time.Since(start)
	var inputBytesPerSecond int64
	if duration > 0 {
		inputBytesPerSecond = int64(float64(stats.InputBytes) / duration.Seconds())
	}
	level.Info(c.logger).Log(
		"msg", "LogMerge: built compacted log object(s)",
		"tenant", node.Tenant,
		"source_objects", stats.SourceObjects,
		"input_sections", stats.InputSections,
		"input_bytes", stats.InputBytes,
		"input_bytes_per_second", inputBytesPerSecond,
		"duplicate_records", stats.DuplicateRecords,
		"output_objects", stats.OutputObjects,
		"output_bytes", stats.OutputBytesCompressed,
		"sort_schema", strings.Join(inputs.layout.SchemaLabels(), ","),
		"duration", duration,
	)

	c.logMergeObserver.ObserveLogMerge(node.Tenant, stats.logMergeObservedStats, duration)

	artifact := &v2.ResultArtifact{Path: idxPath}
	// Coordinators that predate key ranges send no range and cannot decode
	// log counts, so only a task with a range reports them.
	if node.KeyRange != nil {
		artifact.LogCounts = &v2.LogMergeCounts{KeptRecords: keptRecords, Sections: inputs.sectionRecords}
	}
	return artifact, nil
}

const (
	logMergeOutcomeSuccess = "success"
	logMergeOutcomeEmpty   = "empty"
)

// LogMergeObservedStats is the per-task compaction summary reported to
// LogMergeObserver and xcap statistics.
type LogMergeObservedStats struct {
	Outcome               string
	SourceObjects         int
	InputSections         int
	InputBytes            int64 // Log line bytes plus structured metadata value bytes.
	DuplicateRecords      int   // Records that repeat an earlier record's stream, timestamp, line, and metadata.
	OutputObjects         int
	OutputBytesCompressed int64
}

// logMergeObservedStats is the internal alias used while assembling stats.
type logMergeObservedStats = LogMergeObservedStats

// logMergeStats summarizes a completed LogMerge for the reference log line.
type logMergeStats struct {
	logMergeObservedStats
}

type logSource struct {
	path               string
	logSectionsByIndex []*dataobj.Section // Logs-only index across tenants; unselected sections are nil.
	streams            map[int64]streams.Stream
	layout             streams.Layout
	remap              map[int64]int64

	// droppedStreamIDs holds the IDs of the streams outside the key range of
	// the merge. Reads of the object skip their records.
	droppedStreamIDs []int64
}

// collectLogSources opens every unique source object referenced by node.Runs and
// returns only the logs sections the task was assigned (by SectionIndex) plus the
// tenant's localStreamID->stream map. Objects are deduplicated by path.
func (c *Context) collectLogSources(ctx context.Context, node *physical.LogMerge) ([]*logSource, error) {
	// Per object, the set of logs SectionIndex values this task must merge. The
	// paths slice preserves first-seen order for deterministic output.
	wanted := make(map[string]map[int64]struct{})
	var paths []string
	for _, run := range node.Runs {
		for _, sec := range run.Sections {
			if sec == nil {
				continue
			}
			set, ok := wanted[sec.ObjectPath]
			if !ok {
				set = make(map[int64]struct{})
				wanted[sec.ObjectPath] = set
				paths = append(paths, sec.ObjectPath)
			}
			set[sec.SectionIndex] = struct{}{}
		}
	}
	srcBucket := c.dataObjectBucket()
	keyRange, err := sortPrefixRange(node.KeyRange)
	if err != nil {
		return nil, fmt.Errorf("invalid LogMerge task: %w", err)
	}

	// Gather the assigned log sections and the tenant's streams section.
	sources := make([]*logSource, 0, len(paths))
	for _, path := range paths {
		want := wanted[path]
		obj, err := dataobj.FromBucket(ctx, srcBucket, path, 1<<20) // 1MB
		if err != nil {
			return nil, fmt.Errorf("opening object %q: %w", path, err)
		}

		// Use a slice rather than a map iterated in order, sized for all log sections in the object.
		// Slots for unselected sections or sections belonging to other tenants will remain nil
		logSections := make([]*dataobj.Section, obj.Sections().Count(logs.CheckSection))
		found := 0
		// Filter indexes count logs sections across all tenants, matching the
		// index calculator. Unselected sections need no layout or data reads.
		for i, sec := range obj.Sections().Filter(logs.CheckSection) {
			if _, ok := want[int64(i)]; !ok {
				continue
			}
			if sec.Tenant != node.Tenant {
				return nil, fmt.Errorf("object %q logs section %d belongs to tenant %q, expected %q", path, i, sec.Tenant, node.Tenant)
			}
			logSections[i] = sec
			found++
		}
		if found != len(want) {
			return nil, fmt.Errorf("object %q: found %d of %d requested logs sections for tenant %q (stale plan or index/object mismatch)", path, found, len(want), node.Tenant)
		}

		var streamSections []*dataobj.Section
		for _, sec := range obj.Sections().Filter(streams.CheckSection) {
			if sec.Tenant != node.Tenant {
				continue
			}
			streamSections = append(streamSections, sec)
		}

		if len(streamSections) == 0 {
			return nil, fmt.Errorf("object %q has logs sections but no streams section for tenant %q", path, node.Tenant)
		}
		if len(streamSections) > 1 {
			return nil, fmt.Errorf("object %q has %d streams sections for tenant %q, expected exactly one", path, len(streamSections), node.Tenant)
		}

		layout, srcStreams, err := resolveStreams(ctx, streamSections[0], logSections)
		if err != nil {
			return nil, fmt.Errorf("resolving streams for object %q: %w", path, err)
		}
		dropped, err := dropStreamsOutsideRange(srcStreams, layout, keyRange)
		if err != nil {
			return nil, fmt.Errorf("filtering streams for object %q: %w", path, err)
		}

		sources = append(sources, &logSource{
			path:               path,
			streams:            srcStreams,
			layout:             layout,
			logSectionsByIndex: logSections,
			droppedStreamIDs:   dropped,
		})
	}

	return sources, nil
}

// sortPrefixRange returns the stream sort prefixes in r. It returns an error
// if r misses a bound, if its start is not below its end, or if its end is
// above [streams.SortPrefixLimit].
//
// A nil r holds every prefix. Coordinators that predate key ranges send no
// range, and their tasks merge every stream.
func sortPrefixRange(r *compactionv2pb.KeyRange) (v2.KeyRange[streams.SortPrefix], error) {
	if r == nil {
		return v2.KeyRange[streams.SortPrefix]{Start: streams.MinSortPrefix(), End: streams.SortPrefixLimit()}, nil
	}
	if r.Start == nil || r.End == nil {
		return v2.KeyRange[streams.SortPrefix]{}, errors.New("key range must have a start and an end")
	}
	out := v2.KeyRange[streams.SortPrefix]{
		Start: streams.NewSortPrefix(r.Start.ShardBucket, r.Start.SchemaValues),
		End:   streams.NewSortPrefix(r.End.ShardBucket, r.End.SchemaValues),
	}
	if out.Start.Compare(out.End) >= 0 {
		return v2.KeyRange[streams.SortPrefix]{}, fmt.Errorf("key range start %v is not below its end %v", out.Start, out.End)
	}
	if out.End.Compare(streams.SortPrefixLimit()) > 0 {
		return v2.KeyRange[streams.SortPrefix]{}, fmt.Errorf("key range end %v is above the sort prefix limit", out.End)
	}
	return out, nil
}

// dropStreamsOutsideRange deletes the streams outside keyRange from byID and
// returns their IDs.
//
// It derives each sort prefix from the stream labels and layout, as the stats
// section that the planner cuts by does.
func dropStreamsOutsideRange(byID map[int64]streams.Stream, layout streams.Layout, keyRange v2.KeyRange[streams.SortPrefix]) ([]int64, error) {
	schemaLabels, shardLayout := layout.SchemaLabels(), layout.ShardLayout()
	var dropped []int64
	for id, stream := range byID {
		schemaKey, err := logsobj.ComputeSchemaKey(stream.Labels, schemaLabels)
		if err != nil {
			return nil, err
		}
		if !keyRange.Contains(shardLayout.NewSortKey(stream.Labels, schemaKey).Prefix(), streams.SortPrefix.Compare) {
			dropped = append(dropped, id)
			delete(byID, id)
		}
	}
	return dropped, nil
}

// sourcesLayout returns the layout that every source shares. It returns an
// error if two sources have different layouts, because their stream IDs and
// row orders are not comparable.
//
// The output builder writes only the default shard layout, so sourcesLayout
// also returns an error for any other shard layout.
func sourcesLayout(sources []*logSource) (streams.Layout, error) {
	layout := sources[0].layout
	for _, src := range sources[1:] {
		if !src.layout.Equal(layout) {
			return streams.Layout{}, fmt.Errorf("source object %q has a different stream layout than %q", src.path, sources[0].path)
		}
	}
	if layout.ShardLayout() != streams.DefaultShardLayout() {
		return streams.Layout{}, fmt.Errorf("source objects have %d shard buckets, but LogMerge supports only %d", layout.ShardLayout().Buckets(), streams.DefaultShardLayout().Buckets())
	}
	return layout, nil
}

// resolveStreams returns the layout of a streams section and its streams by
// local stream ID. It copies the labels, because the reader reuses its
// buffers.
//
// If the streams section records no layout, resolveStreams takes the layout
// from logsSections, which nil entries skip; see [logs.LegacyLayoutOf].
func resolveStreams(ctx context.Context, section *dataobj.Section, logsSections []*dataobj.Section) (streams.Layout, map[int64]streams.Stream, error) {
	sec, err := streams.Open(ctx, section)
	if err != nil {
		return streams.Layout{}, nil, fmt.Errorf("opening streams section: %w", err)
	}
	layout, ok, err := sec.Layout()
	if err != nil {
		return streams.Layout{}, nil, fmt.Errorf("reading streams layout: %w", err)
	}
	if !ok {
		layout, err = logs.LegacyLayoutOf(ctx, logsSections)
		if err != nil {
			return streams.Layout{}, nil, err
		}
	}

	out := make(map[int64]streams.Stream)
	for res := range streams.IterSection(ctx, sec) {
		stream, err := res.Value()
		if err != nil {
			return streams.Layout{}, nil, err
		}
		stream.Labels = stream.Labels.Copy()
		out[stream.ID] = stream
	}
	return layout, out, nil
}

// buildGlobalStreamTable ranks unique label sets across sources into one
// ID space ranked by SortKey. Same labels in two objects share one ID.
func buildGlobalStreamTable(sources []*logSource, layout streams.Layout) (*logsobj.MultiSourceRankedStreams, error) {
	maps := make([]map[int64]streams.Stream, 0, len(sources))
	for _, src := range sources {
		maps = append(maps, src.streams)
	}
	return logsobj.RankMixedStreams(layout.SchemaLabels(), layout.ShardLayout(), maps...)
}

// logMergeInputs owns source identity, run order, and the global stream namespace.
type logMergeInputs struct {
	sources []*logSource
	layout  streams.Layout
	runs    []sortmerge.Run
	table   *logsobj.MultiSourceRankedStreams

	// sectionRecords holds the record count of every input section, once per
	// section.
	sectionRecords []v2.SectionRecords
}

func (c *Context) prepareLogMergeInputs(ctx context.Context, node *physical.LogMerge) (*logMergeInputs, error) {
	sources, err := c.collectLogSources(ctx, node)
	if err != nil {
		return nil, err
	}
	if len(sources) == 0 {
		level.Warn(c.logger).Log("msg", "LogMerge: skipping task; no source objects found", "tenant", node.Tenant)
		return nil, errNoSourceObjects
	}

	inputs := &logMergeInputs{sources: sources}
	inputs.layout, err = sourcesLayout(sources)
	if err != nil {
		level.Warn(c.logger).Log("msg", "LogMerge: skipping task; source objects do not share one stream layout", "tenant", node.Tenant, "err", err)
		return nil, err
	}

	inputs.table, err = buildGlobalStreamTable(sources, inputs.layout)
	if err != nil {
		return nil, err
	}
	byPath := make(map[string]*logSource, len(sources))
	for i, source := range sources {
		source.remap = inputs.table.Remap(i)
		byPath[source.path] = source
	}
	type sectionID struct {
		path  string
		index int64
	}
	seen := make(map[sectionID]struct{})
	for _, ref := range node.Runs {
		var run sortmerge.Run
		for _, section := range ref.Sections {
			id := sectionID{section.ObjectPath, section.SectionIndex}
			if _, ok := seen[id]; ok {
				continue
			}
			source := byPath[id.path]
			if source == nil || id.index < 0 || id.index >= int64(len(source.logSectionsByIndex)) || source.logSectionsByIndex[id.index] == nil {
				return nil, fmt.Errorf("invalid logs section reference %q#%d", id.path, id.index)
			}
			sec := source.logSectionsByIndex[id.index]
			seen[id] = struct{}{}
			records, err := sectionRecordCount(ctx, sec)
			if err != nil {
				return nil, fmt.Errorf("logs section %q#%d: %w", id.path, id.index, err)
			}
			inputs.sectionRecords = append(inputs.sectionRecords, v2.SectionRecords{ObjectPath: id.path, SectionIndex: id.index, Records: records})

			// Skip the sections of an object with no stream in the key range,
			// so the task downloads none of their pages. A record of a stream
			// that the streams section does not hold still fails a merge: each
			// stream of the object is in the range of another task, and that
			// task reads the section.
			if len(source.streams) == 0 {
				continue
			}
			run = append(run, sortmerge.RemappedSection{Section: sec, Remap: source.remap, DropStreamIDs: source.droppedStreamIDs})
		}
		if len(run) > 0 {
			inputs.runs = append(inputs.runs, run)
		}
	}
	return inputs, nil
}

func sectionRecordCount(ctx context.Context, section *dataobj.Section) (int64, error) {
	sec, err := logs.Open(ctx, section)
	if err != nil {
		return 0, fmt.Errorf("opening logs section: %w", err)
	}
	return sec.RowCount()
}

// logObjectWriter consumes the globally-sorted merged record stream and builds
// one or more compacted log objects, split at TargetObjectSize (never splitting a
// stream across objects)
type logObjectWriter struct {
	c      *Context
	node   *physical.LogMerge
	layout streams.Layout
	table  *logsobj.MultiSourceRankedStreams
	calc   *dataobjindex.Calculator

	builderMetrics *logsobj.BuilderMetrics

	logsBuilder   *logsobj.Builder
	lastSchemaKey string
	lastShard     uint32
	haveLast      bool

	stats logMergeStats
}

// fixedSortSchema gives every tenant the same sort schema, so the output
// builder writes the schema labels of the source layout instead of the tenant
// overrides.
type fixedSortSchema []string

func (s fixedSortSchema) SortSchemaLabels(string) []string { return s }

func (c *Context) newLogObjectWriter(node *physical.LogMerge, layout streams.Layout, table *logsobj.MultiSourceRankedStreams, calc *dataobjindex.Calculator) (*logObjectWriter, error) {
	// Merging an object should not modify an object by dropping duplicates
	c.logsobjCfg.DropDuplicates = false

	w := &logObjectWriter{
		c:              c,
		node:           node,
		layout:         layout,
		table:          table,
		calc:           calc,
		builderMetrics: c.builderMetrics,
	}
	err := w.startNewObject()
	if err != nil {
		return nil, err
	}
	return w, nil
}

func (w *logObjectWriter) startNewObject() error {
	cfg := w.c.logsobjCfg
	overrides := fixedSortSchema(w.layout.SchemaLabels())

	var err error
	w.logsBuilder, err = logsobj.NewBuilder(cfg, w.c.scratchStore, w.builderMetrics, w.c.logger, overrides)
	if err != nil {
		return err
	}

	return nil
}

// add appends one merged record (carrying a global stream ID), rolling to a new
// output object at stream boundaries once the current object reaches its target
// size, and re-basing stream IDs to 1..M within each object.
func (w *logObjectWriter) add(ctx context.Context, rec logs.Record) error {
	gs := w.table.ByID(rec.StreamID)
	if w.logsBuilder.IsFull() && w.haveLast && (gs.SchemaKey != w.lastSchemaKey || gs.ShardBucket != w.lastShard) {
		if err := w.finalizeAndUpload(ctx); err != nil {
			return err
		}
		err := w.startNewObject()
		if err != nil {
			return err
		}
	}
	w.lastSchemaKey = gs.SchemaKey
	w.lastShard = gs.ShardBucket
	w.haveLast = true

	// There's no equivalent for ingestion time during compaction, so use the current time.
	ingestionTime := time.Now()
	err := w.logsBuilder.AppendRecord(w.node.Tenant, gs.Labels, rec, ingestionTime)
	if err != nil {
		return err
	}

	return nil
}

// finish flushes and uploads the last in-progress object (if any) and returns the
// accumulated stats.
func (w *logObjectWriter) finish(ctx context.Context) (logMergeStats, error) {
	if w.logsBuilder.GetEstimatedSize() > 0 {
		if err := w.finalizeAndUpload(ctx); err != nil {
			return w.stats, err
		}
	}
	return w.stats, nil
}

// finalizeAndUpload appends the pending sections, flushes them into one compacted
// log object, computes its content-hash path, and uploads it to the data bucket.
func (w *logObjectWriter) finalizeAndUpload(ctx context.Context) (returnErr error) {
	// The merged records arrive in global stream key order, so the object
	// records the source layout without a sort.
	obj, closer, err := w.logsBuilder.Flush()
	if err != nil {
		return fmt.Errorf("flushing logs builder: %w", err)
	}
	defer util.CloseAndHandleError(closer, &returnErr)

	pathReader, err := obj.Reader(ctx)
	if err != nil {
		return fmt.Errorf("getting object reader: %w", err)
	}
	defer util.CloseAndHandleError(pathReader, &returnErr)

	path, err := v2.CompactedLogObjectPath(w.node.Tenant, pathReader)
	if err != nil {
		return fmt.Errorf("calculating object path: %w", err)
	}

	size, err := w.c.uploadAndIndexObject(ctx, obj, path, w.calc)
	if err != nil {
		return fmt.Errorf("uploading index object: %w", err)
	}

	level.Info(w.c.logger).Log(
		"msg", "LogMerge: uploaded compacted log object",
		"tenant", w.node.Tenant,
		"path", path,
		"bytes", size,
		"object_index", w.stats.OutputObjects,
	)
	w.stats.OutputObjects++
	w.stats.OutputBytesCompressed += size
	return nil
}

// uploadObject streams a built object to the given bucket and returns its encoded
// size. The index object goes to the index bucket; compacted log objects go to
// the data bucket.
func (c *Context) uploadObject(ctx context.Context, bucket objstore.Bucket, path string, obj *dataobj.Object) (size int64, returnErr error) {
	reader, err := obj.Reader(ctx)
	if err != nil {
		return 0, fmt.Errorf("getting object reader: %w", err)
	}
	defer util.CloseAndHandleError(reader, &returnErr)

	if err := bucket.Upload(ctx, path, reader); err != nil {
		return 0, err
	}
	return obj.Size(), nil
}
