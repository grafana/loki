package executor

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/go-kit/log/level"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	dataobjindex "github.com/grafana/loki/v3/pkg/dataobj/index"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/dataobj/sortmerge"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/xcap"
)

func (c *Context) executeLogMerge(node *physical.LogMerge) Pipeline {
	return newLazyPipeline(func(ctx context.Context, _ []Pipeline) Pipeline {
		if err := c.doLogObjectMerge(ctx, node); err != nil {
			return errorPipeline(ctx, err)
		}
		return emptyPipeline()
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

func (c *Context) doLogObjectMerge(ctx context.Context, node *physical.LogMerge) error {
	start := time.Now()
	if c.bucket == nil {
		return errors.New("no object store bucket configured")
	}

	exists, err := c.outputExists(ctx, node.OutputIndexPath)
	if err != nil {
		return fmt.Errorf("checking output existence: %w", err)
	}
	if exists {
		level.Info(c.logger).Log("msg", "LogMerge: output already exists, short-circuiting", "path", node.OutputIndexPath)
		c.observeLogMerge(node.Tenant, logMergeObservedStats{Outcome: logMergeOutcomeShortCircuit}, time.Since(start))
		return nil
	}

	sources, err := c.collectLogSources(ctx, node)
	if err != nil {
		return err
	}
	if len(sources) == 0 {
		c.observeLogMerge(node.Tenant, logMergeObservedStats{Outcome: logMergeOutcomeEmpty}, time.Since(start))
		return fmt.Errorf("LogMerge: no source log sections for tenant %q", node.Tenant)
	}

	table, err := buildGlobalStreamTable(sources, node.SortSchema)
	if err != nil {
		return err
	}

	indexBuilder, err := indexobj.NewBuilder(c.indexobjCfg, c.scratchStore)
	if err != nil {
		return fmt.Errorf("creating index builder: %w", err)
	}
	calc := dataobjindex.NewCalculator(indexBuilder)

	sections, remaps := sectionsWithRemaps(sources, table)
	merged, err := sortmerge.IteratorWithStreamRemap(ctx, sections, remaps, table.sortKeys, node.SortSchema)
	if err != nil {
		return fmt.Errorf("starting k-way log merge: %w", err)
	}

	// Consume the globally-sorted stream and build compacted object
	w := c.newLogObjectWriter(node, table, calc)
	for res := range merged {
		if err := ctx.Err(); err != nil {
			return err
		}
		rec, err := res.Value()
		if err != nil {
			return err
		}
		if err := w.add(ctx, rec); err != nil {
			return err
		}
	}
	stats, err := w.finish(ctx)
	if err != nil {
		return err
	}
	if stats.OutputObjects == 0 {
		c.observeLogMerge(node.Tenant, logMergeObservedStats{Outcome: logMergeOutcomeEmpty}, time.Since(start))
		return fmt.Errorf("LogMerge: produced no compacted objects for tenant %q", node.Tenant)
	}

	idxObj, idxCloser, err := calc.Flush()
	if err != nil {
		return fmt.Errorf("flushing index: %w", err)
	}

	idxBytes, err := c.uploadObject(ctx, c.bucket, node.OutputIndexPath, idxObj)
	if err != nil {
		return errors.Join(fmt.Errorf("uploading index %q: %w", node.OutputIndexPath, err), idxCloser.Close())
	}
	if err := idxCloser.Close(); err != nil {
		return fmt.Errorf("closing index %q: %w", node.OutputIndexPath, err)
	}
	if region := xcap.RegionFromContext(ctx); region != nil {
		region.Record(statLogMergeIndexBytes.Observe(idxBytes))
	}

	stats.SourceObjects = len(sources)
	for _, s := range sources {
		stats.InputSections += len(s.logsSections)
	}
	stats.Outcome = logMergeOutcomeSuccess

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
	return nil
}

const (
	logMergeOutcomeSuccess      = "success"
	logMergeOutcomeShortCircuit = "short_circuit"
	logMergeOutcomeEmpty        = "empty"
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

// logMergeOutputPath derives the deterministic object-storage key for the i-th
// compacted log object from the node's OutputIndexPath. Compacted logs are data
// objects, so they live in the objects/ namespace of the unprefixed data bucket
// alongside ingested source objects — not under the indexes/ namespace where the
// index object itself is written. Deriving the key from OutputIndexPath keeps it
// deterministic and unique per merge task.
func logMergeOutputPath(outputIndexPath string, i int) string {
	objectPath := "objects/" + strings.TrimPrefix(outputIndexPath, "indexes/")
	return fmt.Sprintf("%s.compacted-log.%d", objectPath, i)
}

type logSource struct {
	path         string
	logsSections []*dataobj.Section
	streams      map[int64]streams.Stream
}

// collectLogSources opens every unique source object referenced by node.Runs and
// returns the tenant's logs sections plus its localStreamID->stream map. Objects are deduplicated by path
func (c *Context) collectLogSources(ctx context.Context, node *physical.LogMerge) ([]*logSource, error) {
	// Deduplicate object paths across all runs
	seen := make(map[string]struct{})
	var paths []string
	for _, run := range node.Runs {
		if run == nil {
			continue
		}
		for _, sec := range run.Sections {
			if sec == nil {
				continue
			}
			if _, ok := seen[sec.ObjectPath]; ok {
				continue
			}
			seen[sec.ObjectPath] = struct{}{}
			paths = append(paths, sec.ObjectPath)
		}
	}
	srcBucket := c.dataObjectBucket()

	// Gather log and streams sections
	sources := make([]*logSource, 0, len(paths))
	for _, path := range paths {
		obj, err := dataobj.FromBucket(ctx, srcBucket, path, 0)
		if err != nil {
			return nil, fmt.Errorf("opening object %q: %w", path, err)
		}

		var (
			logsSections   []*dataobj.Section
			streamSections []*dataobj.Section
		)
		for _, sec := range obj.Sections() {
			if sec.Tenant != node.Tenant {
				continue
			}
			switch {
			case logs.CheckSection(sec):
				logsSections = append(logsSections, sec)
			case streams.CheckSection(sec):
				streamSections = append(streamSections, sec)
			}
		}

		if len(logsSections) == 0 {
			continue
		}

		if len(streamSections) == 0 {
			return nil, fmt.Errorf("object %q has logs sections but no streams section for tenant %q", path, node.Tenant)
		}
		if len(streamSections) > 1 {
			return nil, fmt.Errorf("object %q has %d streams sections for tenant %q, expected exactly one", path, len(streamSections), node.Tenant)
		}

		srcStreams, err := resolveStreams(ctx, streamSections[0])
		if err != nil {
			return nil, fmt.Errorf("resolving streams for object %q: %w", path, err)
		}

		sources = append(sources, &logSource{
			path:         path,
			logsSections: logsSections,
			streams:      srcStreams,
		})
	}

	return sources, nil
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
	streams        []streams.Stream  // index = global ID; source stream with aggregates
	streamIDRemaps []map[int64]int64 // per source object (by index): sourceStreamID -> globalID
}

// buildGlobalStreamTable computes the global stream assignment from all sources.
func buildGlobalStreamTable(sources []*logSource, sortSchema []string) (*globalStreamTable, error) {
	type entry struct {
		sourceIdx      int
		sourceStreamID int64
		sortKey        string
		stream         streams.Stream
	}

	var allEntries []entry
	for sourceIdx, src := range sources {
		for sourceStreamID, s := range src.streams {
			key, err := logsobj.ComputeSortKey(s.Labels, sortSchema)
			if err != nil {
				return nil, fmt.Errorf("computing sort key for object %q: %w", src.path, err)
			}
			allEntries = append(allEntries, entry{
				sourceIdx:      sourceIdx,
				sourceStreamID: sourceStreamID,
				sortKey:        key,
				stream:         s,
			})
		}
	}

	// Order by (sortKey, sourceIdx, sourceStreamID) so global IDs are sort-key-major
	// and each source section stays monotonic under the merge comparator.
	slices.SortFunc(allEntries, func(a, b entry) int {
		if r := cmp.Compare(a.sortKey, b.sortKey); r != 0 {
			return r
		}
		if r := cmp.Compare(a.sourceIdx, b.sourceIdx); r != 0 {
			return r
		}
		return cmp.Compare(a.sourceStreamID, b.sourceStreamID)
	})

	table := &globalStreamTable{
		sortKeys:       make([]string, len(allEntries)+1),
		streams:        make([]streams.Stream, len(allEntries)+1),
		streamIDRemaps: make([]map[int64]int64, len(sources)),
	}
	for i := range table.streamIDRemaps {
		table.streamIDRemaps[i] = make(map[int64]int64)
	}
	for i, e := range allEntries {
		gid := int64(i + 1)
		table.sortKeys[gid] = e.sortKey
		s := e.stream
		s.ID = gid
		table.streams[gid] = s
		table.streamIDRemaps[e.sourceIdx][e.sourceStreamID] = gid
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

	logsMetrics    *logs.Metrics
	streamsMetrics *streams.Metrics
	targetObject   int
	targetSection  int

	builder       *dataobj.Builder
	lb            *logs.Builder
	sb            *streams.Builder
	objSize       int
	objStreams    int
	objRecords    int
	curGlobalID   int64
	curObjLocalID int64

	stats logMergeStats
}

func (c *Context) newLogObjectWriter(node *physical.LogMerge, table *globalStreamTable, calc *dataobjindex.Calculator) *logObjectWriter {
	w := &logObjectWriter{
		c:              c,
		node:           node,
		table:          table,
		calc:           calc,
		logsMetrics:    logs.NewMetrics(),
		streamsMetrics: streams.NewMetrics(),
		targetObject:   int(c.indexobjCfg.TargetObjectSize),
		targetSection:  int(c.indexobjCfg.TargetSectionSize),
	}
	w.startObject()
	return w
}

func (w *logObjectWriter) startObject() {
	w.builder = dataobj.NewBuilder(w.c.scratchStore)
	w.lb = logs.NewBuilder(w.logsMetrics, w.c.logsBuilderOptions(w.node.SortSchema))
	w.lb.SetTenant(w.node.Tenant)
	w.sb = streams.NewBuilder(w.streamsMetrics, int(w.c.indexobjCfg.TargetPageSize), w.c.indexobjCfg.MaxPageRows)
	w.sb.SetTenant(w.node.Tenant)
	w.objSize, w.objStreams, w.objRecords = 0, 0, 0
	w.curGlobalID, w.curObjLocalID = 0, 0
}

// add appends one merged record (carrying a global stream ID), rolling to a new
// output object at stream boundaries once the current object reaches its target
// size, and re-basing stream IDs to 1..M within each object.
func (w *logObjectWriter) add(ctx context.Context, rec logs.Record) error {
	if rec.StreamID != w.curGlobalID {

		if w.objRecords > 0 && w.targetObject > 0 && w.objSize >= w.targetObject {
			if err := w.finalizeAndUpload(ctx); err != nil {
				return err
			}
			w.startObject()
		}
		w.curGlobalID = rec.StreamID
		w.curObjLocalID++
		s := w.table.streams[rec.StreamID]
		s.ID = w.curObjLocalID
		w.sb.AppendValue(s)
		w.objStreams++
	}

	rec.StreamID = w.curObjLocalID
	w.lb.Append(rec)
	w.objRecords++
	w.objSize += logRecordSize(rec)

	if w.lb.UncompressedSize() > w.targetSection {
		if err := w.builder.Append(w.lb); err != nil {
			return fmt.Errorf("appending logs section: %w", err)
		}
		w.lb.Reset()
		w.lb.SetTenant(w.node.Tenant)
	}
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

// finalizeAndUpload appends the pending sections, flushes them into one compacted
// log object, and uploads it to a deterministic key.
func (w *logObjectWriter) finalizeAndUpload(ctx context.Context) error {
	if w.lb.UncompressedSize() > 0 {
		if err := w.builder.Append(w.lb); err != nil {
			return fmt.Errorf("appending logs section: %w", err)
		}
	}
	if err := w.builder.Append(w.sb); err != nil {
		return fmt.Errorf("appending streams section: %w", err)
	}

	obj, closer, err := w.builder.Flush()
	if err != nil {
		return fmt.Errorf("flushing object: %w", err)
	}

	path := logMergeOutputPath(w.node.OutputIndexPath, w.stats.OutputObjects)

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
		"streams", w.objStreams,
		"records", w.objRecords,
		"bytes", size,
	)
	w.stats.OutputObjects++
	w.stats.OutputStreams += w.objStreams
	w.stats.OutputRecords += w.objRecords
	w.stats.OutputBytesCompressed += size
	w.stats.OutputBytesUncompressed += int64(w.objSize)
	return nil
}

// logsBuilderOptions builds the logs section options for a schema-sorted output,
// sized from the executor's shared builder config.
func (c *Context) logsBuilderOptions(sortSchema []string) logs.BuilderOptions {
	return logs.BuilderOptions{
		PageSizeHint:     int(c.indexobjCfg.TargetPageSize),
		PageMaxRowCount:  c.indexobjCfg.MaxPageRows,
		BufferSize:       int(c.indexobjCfg.BufferSize),
		StripeMergeLimit: c.indexobjCfg.SectionStripeMergeLimit,
		AppendStrategy:   logs.AppendOrdered,
		SortOrder:        logs.SortSchemaASC,
		SchemaLabels:     sortSchema,
	}
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
