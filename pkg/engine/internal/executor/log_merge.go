package executor

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/go-kit/log/level"
	"github.com/prometheus/prometheus/model/labels"
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

func scanLogObjectSortKeys(bucket objstore.Bucket, paths []string) {
	for _, path := range paths {
		obj, err := dataobj.FromBucket(context.Background(), bucket, path, 1024*1024)
		if err != nil {
			continue
		}
		var sortKeys = []string{}
		for _, sec := range obj.Sections().Filter(streams.CheckSection) {
			ls, err := streams.Open(context.Background(), sec)
			if err != nil {
				panic(err)
			}
			lr := streams.NewRowReader(ls)
			lr.Open(context.Background())

			s := make([]streams.Stream, 1024)
			for {
				n, err := lr.Read(context.Background(), s)
				if err != nil && !errors.Is(err, io.EOF) {
					break
				}
				for i := 0; i < n; i++ {
					key, err := logsobj.ComputeSortKey(s[i].Labels, []string{"label:service_name", "label:cluster"})
					if err != nil {
						panic(err)
					}
					sortKeys = append(sortKeys, key)
				}
				if err != nil {
					break
				}
			}
		}

		for idx, sec := range obj.Sections().Filter(logs.CheckSection) {
			ls, err := logs.Open(context.Background(), sec)
			if err != nil {
				panic(err)
			}
			lr := logs.NewRowReader(ls)
			lr.Open(context.Background())

			minSK := ""
			maxSK := ""
			s := make([]logs.Record, 1024)
			for {
				n, err := lr.Read(context.Background(), s)
				if err != nil {
					break
				}
				for i := 0; i < n; i++ {
					sk := sortKeys[s[i].StreamID-1]

					if minSK == "" || sk < minSK {
						minSK = sk
					}
					if maxSK == "" || sk > maxSK {
						maxSK = sk
					}
				}
			}
			fmt.Printf("obj=%s sec=%d minSK=%q maxSK=%q\n", path, idx, strings.Replace(minSK, "\x00", ":", 2), strings.Replace(maxSK, "\x00", ":", 2))
		}
	}
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
	merged, err := sortmerge.IteratorWithStreamRemap(ctx, sections, remaps, table.sortKeys, node.SortSchema)
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

	scanLogObjectSortKeys(c.bucket, stats.OutputObjectPaths)

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
	cfg := logsobj.BuilderConfig{
		BuilderBaseConfig:    c.indexobjCfg,
		DataobjSortOrder:     "stream-asc",
		AppendOrderedEnabled: true,
		DataobjUseSortSchema: true,
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

	obj, closer, err := w.sorter.CopyAndSort(ctx, intermediate)
	if err != nil {
		return errors.Join(fmt.Errorf("sorting compacted object: %w", err), intermediateCloser.Close())
	}
	if err := intermediateCloser.Close(); err != nil {
		return errors.Join(fmt.Errorf("closing unsorted compacted object: %w", err), closer.Close())
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
