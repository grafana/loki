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
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	dataobjindex "github.com/grafana/loki/v3/pkg/dataobj/index"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/dataobj/sortmerge"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/util"
)

var errNoSourceObjects = errors.New("no source objects found")

func (c *Context) executeLogMerge(node *physical.LogMerge) Pipeline {
	return newLazyPipeline(func(ctx context.Context, _ []Pipeline) Pipeline {
		arts, err := c.doLogObjectMerge(ctx, node)
		if err != nil {
			return errorPipeline(ctx, err)
		}
		if len(arts) == 0 {
			return emptyPipeline()
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

	inputs, err := c.prepareLogMergeInputs(ctx, node)
	if err != nil {
		if errors.Is(err, errNothingToDo) {
			c.observeLogMerge(node.Tenant, logMergeObservedStats{Outcome: logMergeOutcomeEmpty}, time.Since(start))
		}

		return nil, err
	}

	indexBuilder, err := indexobj.NewBuilder(c.indexobjCfg, c.scratchStore)
	if err != nil {
		return nil, fmt.Errorf("creating index builder: %w", err)
	}
	calc := dataobjindex.NewCalculator(indexBuilder)

	merged := sortmerge.MixedRunIterator(ctx, inputs.runs, node.SortSchema)

	// Consume the globally-sorted stream and build compacted object
	w, err := c.newLogObjectWriter(node, inputs.table, calc)
	if err != nil {
		return nil, err
	}
	for res := range merged {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		rec, err := res.Value()
		if err != nil {
			return nil, err
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

	idxPath, err := c.flushAndUploadIndex(ctx, calc, func(ctx context.Context, obj *dataobj.Object) (path string, outputErr error) {
		reader, err := obj.Reader(ctx)
		if err != nil {
			return "", err
		}
		defer util.CloseAndHandleError(reader, &outputErr)
		return v2.CompactedIndexPath(node.Tenant, reader)
	})
	if err != nil {
		return nil, err
	}

	stats.Outcome = logMergeOutcomeSuccess
	stats.SourceObjects = len(inputs.sources)
	for _, run := range inputs.runs {
		stats.InputSections += len(run)
	}

	level.Info(c.logger).Log(
		"msg", "LogMerge: built compacted log object(s)",
		"tenant", node.Tenant,
		"source_objects", stats.SourceObjects,
		"input_sections", stats.InputSections,
		"output_objects", stats.OutputObjects,
		"output_bytes", stats.OutputBytesCompressed,
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
	Outcome               string
	SourceObjects         int
	InputSections         int
	OutputObjects         int
	OutputBytesCompressed int64
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
	path               string
	logSectionsByIndex []*dataobj.Section // Logs-only index across tenants; unselected sections are nil.
	streams            map[int64]streams.Stream
	remap              map[int64]int64
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

		srcStreams, err := resolveStreams(ctx, streamSections[0])
		if err != nil {
			return nil, fmt.Errorf("resolving streams for object %q: %w", path, err)
		}

		sources = append(sources, &logSource{
			path:               path,
			streams:            srcStreams,
			logSectionsByIndex: logSections,
		})
	}

	return sources, nil
}

// sourcesMatchSortLayout checks whether every logs section in sources has the
// target layout. mismatch is the first object path that does not match.
func sourcesMatchSortLayout(ctx context.Context, sources []*logSource, sortSchema []string) (string, error) {
	want := logsobj.TargetSortLayout(sortSchema)
	for _, src := range sources {
		for _, sec := range src.logSectionsByIndex {
			if sec == nil {
				// Not all sections are selected for merge. Unselected sections are nil
				continue
			}
			opened, err := logs.Open(ctx, sec)
			if err != nil {
				return src.path, fmt.Errorf("opening logs section in %q: %w", src.path, err)
			}
			got := opened.SortLayout()
			if !logsobj.EqualSortLayout(got, want) {
				return src.path, nil
			}
		}
	}
	return "", nil
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

// buildGlobalStreamTable ranks unique label sets across sources into one
// ID space ranked by SortKey. Same labels in two objects share one ID.
func buildGlobalStreamTable(sources []*logSource, sortSchema []string) (*logsobj.MultiSourceRankedStreams, error) {
	maps := make([]map[int64]streams.Stream, 0, len(sources))
	for _, src := range sources {
		maps = append(maps, src.streams)
	}
	return logsobj.RankMixedStreams(sortSchema, maps...)
}

// logMergeInputs owns source identity, run order, and the global stream namespace.
type logMergeInputs struct {
	sources  []*logSource
	runs     []sortmerge.Run
	table    *logsobj.MultiSourceRankedStreams
	mismatch string
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
	inputs.mismatch, err = sourcesMatchSortLayout(ctx, sources, node.SortSchema)
	if err != nil {
		return nil, err
	}

	if inputs.mismatch != "" {
		level.Warn(c.logger).Log(
			"msg", "LogMerge: skipping task; source object sort layout does not match target",
			"tenant", node.Tenant,
			"path", inputs.mismatch,
			"sort_schema", strings.Join(node.SortSchema, ","),
		)
		return nil, fmt.Errorf("source object %q sort layout does not match target", inputs.mismatch)
	}

	inputs.table, err = buildGlobalStreamTable(sources, node.SortSchema)
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
			run = append(run, sortmerge.RemappedSection{Section: sec, Remap: source.remap})
		}
		if len(run) > 0 {
			inputs.runs = append(inputs.runs, run)
		}
	}
	return inputs, nil
}

// logObjectWriter consumes the globally-sorted merged record stream and builds
// one or more compacted log objects, split at TargetObjectSize (never splitting a
// stream across objects)
type logObjectWriter struct {
	c     *Context
	node  *physical.LogMerge
	table *logsobj.MultiSourceRankedStreams
	calc  *dataobjindex.Calculator

	builderMetrics *logsobj.BuilderMetrics

	logsBuilder   *logsobj.Builder
	lastSchemaKey string
	lastShard     uint32
	haveLast      bool

	stats logMergeStats
}

type fixedSortSchema []string

func (s fixedSortSchema) SortSchemaLabels(string) []string { return s }

func (c *Context) newLogObjectWriter(node *physical.LogMerge, table *logsobj.MultiSourceRankedStreams, calc *dataobjindex.Calculator) (*logObjectWriter, error) {
	w := &logObjectWriter{
		c:              c,
		node:           node,
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
	cfg := logsobj.BuilderConfig{
		BuilderBaseConfig:    w.c.logsobjCfg,
		AppendOrderedEnabled: true,
	}
	overrides := fixedSortSchema(w.node.SortSchema)

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
	obj, closer, err := w.logsBuilder.Flush()
	if err != nil {
		return fmt.Errorf("flushing logs builder: %w", err)
	}
	defer closer.Close()

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
