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
)

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

	sources, err := c.collectLogSources(ctx, node)
	if err != nil {
		return nil, err
	}
	if len(sources) == 0 {
		c.observeLogMerge(node.Tenant, logMergeObservedStats{Outcome: logMergeOutcomeEmpty}, time.Since(start))
		return nil, fmt.Errorf("LogMerge: no source log sections for tenant %q", node.Tenant)
	}

	ok, mismatch, err := sourcesMatchSortLayout(ctx, sources, node.SortSchema)
	if err != nil {
		return nil, err
	}
	if !ok {
		level.Warn(c.logger).Log(
			"msg", "LogMerge: skipping task; source object sort layout does not match target",
			"tenant", node.Tenant,
			"path", mismatch,
			"sort_schema", strings.Join(node.SortSchema, ","),
		)
		c.observeLogMerge(node.Tenant, logMergeObservedStats{Outcome: logMergeOutcomeEmpty}, time.Since(start))
		return nil, nil
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
	merged, err := sortmerge.MixedObjectIterator(ctx, sections, remaps, node.SortSchema)
	if err != nil {
		return nil, fmt.Errorf("starting k-way log merge: %w", err)
	}

	// Consume the globally-sorted stream and build compacted object
	w, err := c.newLogObjectWriter(node, table, calc)
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

// sourcesMatchSortLayout reports whether every logs section in sources has the
// target layout. mismatch is the first object path that does not match.
func sourcesMatchSortLayout(ctx context.Context, sources []*logSource, sortSchema []string) (ok bool, mismatch string, err error) {
	want := logs.SortLayout{
		SchemaLabels: sortSchema,
		StreamOrder:  logs.StreamOrderStableHashV1,
		ShardCount:   streams.ShardFactor,
	}
	for _, src := range sources {
		for _, sec := range src.logsSections {
			opened, err := logs.Open(ctx, sec)
			if err != nil {
				return false, src.path, fmt.Errorf("opening logs section in %q: %w", src.path, err)
			}
			got := opened.SortLayout()
			if !sortLayoutEqual(got, want) {
				return false, src.path, nil
			}
		}
	}
	return true, "", nil
}

func sortLayoutEqual(got, want logs.SortLayout) bool {
	return slices.Equal(got.SchemaLabels, want.SchemaLabels) &&
		got.StreamOrder == want.StreamOrder &&
		got.ShardCount == want.ShardCount
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
// StreamOrderKey ID space. Same labels in two objects share one ID.
func buildGlobalStreamTable(sources []*logSource, sortSchema []string) (*logsobj.MultiSourceRankedStreams, error) {
	maps := make([]map[int64]streams.Stream, 0, len(sources))
	for _, src := range sources {
		maps = append(maps, src.streams)
	}
	return logsobj.RankMixedStreams(sortSchema, maps...)
}

// sectionsWithRemaps flattens the sources' logs sections
func sectionsWithRemaps(sources []*logSource, table *logsobj.MultiSourceRankedStreams) ([]*dataobj.Section, []map[int64]int64) {
	var (
		sections []*dataobj.Section
		remaps   []map[int64]int64
	)
	for sourceIdx, src := range sources {
		for _, sec := range src.logsSections {
			sections = append(sections, sec)
			remaps = append(remaps, table.Remap(sourceIdx))
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
		builderMetrics: logsobj.NewBuilderMetrics(),
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
func (w *logObjectWriter) finalizeAndUpload(ctx context.Context) error {
	obj, closer, err := w.logsBuilder.Flush()
	if err != nil {
		return fmt.Errorf("flushing logs builder: %w", err)
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
