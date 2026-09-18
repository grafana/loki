package querier

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/dataobj/dataobjmetrics"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/logql"
	logqllog "github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/xcap"
)

const (
	// maxParallelObjectResolves bounds how many objects the planner resolves (opens + reads the streams
	// section) concurrently.
	maxParallelObjectResolves = 128

	// planBufferSize is the task-channel buffer for plan. It is large so the background planner runs well
	// ahead of the reader and effectively never blocks on it. The channel holds one task per logs section;
	// a corpus rarely has more than a few hundred sections, and a dataObjReadTask is small, so the memory
	// bound is negligible.
	planBufferSize = 1024
)

// streamID identifies a stream within a data object. The object's builder assigns it and it is local to
// the object — distinct from the tenant-wide stream fingerprint (a uint64 StableHash). The metastore,
// logs, and streams sections all carry it as a plain int64; the reader uses this type for clarity.
type streamID int64

// streamIDsToInt64 converts stream IDs to the plain []int64 that the metastore and section readers use.
func streamIDsToInt64(ids []streamID) []int64 {
	out := make([]int64, len(ids))
	for i, id := range ids {
		out[i] = int64(id)
	}
	return out
}

// dataObjReadTask is the plan for reading one logs section: the shard-filtered streams to read, their
// labels and fingerprints, the column projection, and the row predicates.
type dataObjReadTask struct {
	// object is the object-storage path of the data object that holds the section.
	object string

	// section is the logs-relative index of the section within the object.
	section int

	// streamIDs are the shard-filtered stream IDs to read from the section.
	streamIDs []streamID

	// labels maps each stream ID to its raw stream labels.
	labels map[streamID]labels.Labels

	// fingerprints maps each stream ID to its label StableHash.
	fingerprints map[streamID]uint64

	// projectedColumns are the column types to read; ColumnTypeMetadata here means "all metadata".
	projectedColumns []logs.ColumnType

	// projectedMetadata are the specific metadata columns to project by name; empty means none, unless
	// projectedColumns contains ColumnTypeMetadata.
	projectedMetadata []string

	// predicates are the row predicates to push to the reader, besides the time range.
	predicates []logs.RowPredicate

	// start and end are the sample time range the reader filters to.
	start, end time.Time
}

// rowPredicates returns the predicates the reader applies to the section: the half-open time window
// [start, end) followed by the planned metadata predicates.
func (t dataObjReadTask) rowPredicates() []logs.RowPredicate {
	predicates := make([]logs.RowPredicate, 0, 1+len(t.predicates))
	predicates = append(predicates, logs.TimeRangeRowPredicate{
		StartTime:    t.start,
		EndTime:      t.end,
		IncludeStart: true,
		IncludeEnd:   false,
	})
	predicates = append(predicates, t.predicates...)
	return predicates
}

// recordBatch turns a section read's records into forwardable log records. It attaches each stream's
// fingerprint and labels, and copies each line because the reader reuses its decode buffer across
// reads.
//
// It returns an error if a record carries a stream ID the task did not plan to read. MatchStreams
// restricts the read to the task's streams, so an unknown ID means that invariant broke; failing the
// read stops the query rather than under-counting silently.
func (t dataObjReadTask) recordBatch(recs []logs.Record) ([]dataObjLogRecord, error) {
	out := make([]dataObjLogRecord, 0, len(recs))
	for i := range recs {
		rec := &recs[i]
		id := streamID(rec.StreamID)
		fp, ok := t.fingerprints[id]
		if !ok {
			return nil, fmt.Errorf("data object %q section %d returned unexpected stream ID %d", t.object, t.section, id)
		}
		out = append(out, dataObjLogRecord{
			fingerprint:  fp,
			streamLabels: t.labels[id],
			timestamp:    rec.Timestamp.UnixNano(),
			line:         append([]byte(nil), rec.Line...),
			metadata:     rec.Metadata,
		})
	}
	return out, nil
}

// readQuery is the query-level input shared by every section read in a plan: the sample expression to
// analyse, the shard filter, and the sample time range.
type readQuery struct {
	expr       syntax.SampleExpr
	shard      *logql.Shard
	start, end time.Time

	// shardBucket, when non-nil, prunes streams to its bucket range before their labels are decoded. It
	// is set only when the feature is enabled and the shard restricts streams.
	shardBucket *shardBucketFilter
}

// dataObjReadPlanner turns a metric query into the section-read tasks the reader runs. It owns the metastore lookup, reads
// each object's streams to compute fingerprints and apply the shard filter, and analyses the query
// expression to decide the column projection and which metadata filters to push down.
type dataObjReadPlanner struct {
	resolver                         dataObjSectionsResolver
	cache                            *dataObjCache
	shardBucketFilterEnabled         bool
	sectionShardBucketPruningEnabled bool
}

func newDataObjReadPlanner(resolver dataObjSectionsResolver, cache *dataObjCache, shardBucketFilterEnabled, sectionShardBucketPruningEnabled bool) *dataObjReadPlanner {
	return &dataObjReadPlanner{
		resolver:                         resolver,
		cache:                            cache,
		shardBucketFilterEnabled:         shardBucketFilterEnabled,
		sectionShardBucketPruningEnabled: sectionShardBucketPruningEnabled,
	}
}

// plan resolves the query's sections and streams the resulting read tasks through a
// dataObjTaskIterator. Resolution (the metastore lookup, then each object's streams read) runs in a
// background goroutine, so the reader can start reading one object's logs while later objects are still
// being resolved. A resolution error is recorded on the iterator and surfaced through its Err.
func (p *dataObjReadPlanner) plan(ctx context.Context, start, end time.Time, matchers []*labels.Matcher, shard *logql.Shard, expr syntax.SampleExpr) *dataObjTaskIterator {
	ctx, cancel := context.WithCancel(ctx)
	ch := make(chan dataObjReadTask, planBufferSize)
	it := newDataObjTaskIterator(ch, cancel)

	go func() {
		defer close(it.done)
		defer close(ch)

		// The shard maps to a contiguous bucket range once; both prunes below reuse it.
		sb, hasBucket := shardBucketRange(shard)

		// The resolver self-scopes its object-storage reads (metastore) or its index-gateway calls; the
		// streams reads (and the object-open head prefetch they trigger) get their own region here, so
		// each phase's fetched bytes are attributed to the right component.
		//
		// When enabled, narrow resolution to the shard's bucket range: the postings scan returns a superset
		// of this shard's streams (coarse, per-section), and the streams-read fingerprint recheck below still
		// enforces exactness. Only the metastore (postings) resolver honors it; the index-gateway ignores it.
		var bucketRange *metastore.ShardBucketRange
		if p.sectionShardBucketPruningEnabled && hasBucket {
			bucketRange = &metastore.ShardBucketRange{From: uint32(sb.from), To: uint32(sb.to)}
		}

		resolveStart := time.Now()
		sections, err := p.resolver.resolveSections(ctx, start, end, matchers, bucketRange)
		stats.FromContext(ctx).RecordDataobjSectionsResolutionTime(time.Since(resolveStart))
		if err != nil {
			it.setErr(fmt.Errorf("resolving data object sections: %w", err))
			return
		}

		query := readQuery{expr: expr, shard: shard, start: start, end: end}
		if p.shardBucketFilterEnabled && hasBucket {
			query.shardBucket = &sb
		}

		streamsCtx, _ := xcap.StartRegion(ctx, dataobjmetrics.ComponentStreamsReader)
		if err := p.planObjectsRead(streamsCtx, sections, query, ch); err != nil {
			it.setErr(err)
		}
	}()

	return it
}

// planObjectsRead groups the resolved sections by object and plans each object's read concurrently
// (opening an object and reading its streams section is I/O bound), sending each object's tasks to out
// as soon as that object is planned. It returns the first resolution error, or ctx.Err() if cancelled.
func (p *dataObjReadPlanner) planObjectsRead(ctx context.Context, sections []*metastore.DataobjSectionDescriptor, query readQuery, out chan<- dataObjReadTask) error {
	byObject := map[string][]*metastore.DataobjSectionDescriptor{}
	for _, d := range sections {
		byObject[d.ObjectPath] = append(byObject[d.ObjectPath], d)
	}

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(maxParallelObjectResolves)
	for path, descs := range byObject {
		g.Go(func() error {
			tasks, err := p.planObjectRead(ctx, path, descs, query)
			if err != nil {
				return err
			}
			for _, t := range tasks {
				select {
				case out <- t:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return nil
		})
	}
	return g.Wait()
}

// planObjectRead reads one object's streams once, then plans the read of each of its logs sections.
func (p *dataObjReadPlanner) planObjectRead(ctx context.Context, path string, descs []*metastore.DataobjSectionDescriptor, query readQuery) ([]dataObjReadTask, error) {
	obj, err := p.cache.get(ctx, path)
	if err != nil {
		return nil, err
	}

	want := map[streamID]struct{}{}
	for _, d := range descs {
		for _, id := range d.StreamIDs {
			want[streamID(id)] = struct{}{}
		}
	}
	idLabels, filtered, err := obj.streamLabels(ctx, want, query)
	if err != nil {
		return nil, err
	}

	var tasks []dataObjReadTask
	for _, d := range descs {
		task, ok, err := planSectionRead(d, idLabels, query, filtered)
		if err != nil {
			return nil, err
		}
		if ok {
			tasks = append(tasks, task)
		}
	}
	return tasks, nil
}

// planSectionRead plans the read of a single logs section into one task: it applies the shard filter to
// the section's streams, then analyses the query against just those streams' labels to decide the column
// projection and the pushed-down predicates. It returns ok=false when the shard filter leaves no streams.
//
// It returns an error when the metastore lists a stream ID that the object's streams section does not
// hold: that is a broken invariant, and dropping the stream would silently under-count the query. This
// mirrors recordBatch, which fails on an unexpected stream ID rather than under-counting.
func planSectionRead(desc *metastore.DataobjSectionDescriptor, idLabels map[streamID]labels.Labels, query readQuery, shardBucketFiltered bool) (dataObjReadTask, bool, error) {
	var (
		streamIDs        []streamID
		labelsByID       = map[streamID]labels.Labels{}
		fingerprintsByID = map[streamID]uint64{}
	)

	for _, rawID := range desc.StreamIDs {
		id := streamID(rawID)
		lbls, ok := idLabels[id]
		if !ok {
			if shardBucketFiltered {
				// On the shard-filtered read a listed ID absent from the result is treated as out-of-shard:
				// the bucket predicate dropped it. It could instead be genuinely missing from the section
				// (metastore/object disagreement), but the single pruned read cannot tell the two apart, so
				// that corruption is not detected here — a trade-off for dropping the extra existence scan.
				continue
			}
			return dataObjReadTask{}, false, fmt.Errorf("data object %q logs section %d: stream ID %d listed by the metastore is missing from the object's streams section", desc.ObjectPath, desc.SectionIdx, id)
		}

		fp := labels.StableHash(lbls)
		// The bucket predicate resolves the shard exactly only for a power-of-two shard of 2..<streams.ShardFactor>;
		// otherwise it over-fetches, so keep the fingerprint recheck. fp is still needed below for the stream hash.
		if query.shard != nil && !(shardBucketFiltered && query.shardBucket != nil && query.shardBucket.exact) && !query.shard.Match(model.Fingerprint(fp)) {
			continue
		}
		if _, dup := fingerprintsByID[id]; dup {
			continue
		}
		streamIDs = append(streamIDs, id)
		labelsByID[id] = lbls
		fingerprintsByID[id] = fp
	}

	if len(streamIDs) == 0 {
		return dataObjReadTask{}, false, nil
	}

	// Analyse against this section's stream labels only, so the metadata pushdown gate is as narrow as
	// possible: a key that is a stream label in another section can still be pushed here when it is
	// structured metadata for this section's streams.
	projectedColumns, projectedMetadata, predicates := planProjectionsAndPredicates(query.expr, streamLabelNames(labelsByID))
	return dataObjReadTask{
		object:            desc.ObjectPath,
		section:           int(desc.SectionIdx),
		streamIDs:         streamIDs,
		labels:            labelsByID,
		fingerprints:      fingerprintsByID,
		projectedColumns:  projectedColumns,
		projectedMetadata: projectedMetadata,
		predicates:        predicates,
		start:             query.start,
		end:               query.end,
	}, true, nil
}

// streamLabelNames returns the set of stream-label names across the given streams.
func streamLabelNames(byID map[streamID]labels.Labels) map[string]struct{} {
	names := map[string]struct{}{}
	for _, lbls := range byID {
		lbls.Range(func(l labels.Label) { names[l.Name] = struct{}{} })
	}
	return names
}

// planProjectionsAndPredicates decides the column projection, the structured-metadata columns to project, and the
// metadata equality predicates to push down.
//
// It reads stream_id and timestamp always, and the message only when the value or pipeline needs the
// line. The structured-metadata projection follows what the sample extractor does with the query, which
// the reader must reproduce because the extractor reduces labels identically on the chunk and data-object
// paths:
//
//   - The extractor reads the metadata a pipeline filter or an unwrap references, and emits the metadata
//     that survives into the output series labels. Those keys are projected by name.
//   - When the output series can carry arbitrary metadata — a bare range aggregation (full label set), a
//     `without` grouping, or a parser/formatter that derives labels from the line — every metadata column
//     is projected, since the set cannot be enumerated ahead of time.
//   - Otherwise (a `sum` or a `by` grouping that reduces to a known label set) only the referenced and
//     grouped keys are projected; unreferenced metadata is dropped by the extractor on both paths, so it
//     is never read.
//
// A metadata filter is also pushed to the reader (see pushableMetadataMatchers and
// metadataMatcherPredicates): an equality prunes pages, and any other matcher still filters rows so the
// message and other secondary columns are read only for the survivors.
func planProjectionsAndPredicates(expr syntax.SampleExpr, streamLabelNames map[string]struct{}) (projectedColumns []logs.ColumnType, projectedMetadata []string, predicates []logs.RowPredicate) {
	var (
		isComplex  bool
		needLine   bool
		anyWithout bool
		pushable   []*labels.Matcher       // metadata matchers to push to the reader
		refKeys    = map[string]struct{}{} // metadata the extractor reads (filters, unwrap) or emits (grouping)
	)
	addRef := func(name string) {
		if name != "" {
			refKeys[name] = struct{}{}
		}
	}
	handleGrouping := func(g *syntax.Grouping) {
		if g == nil {
			return
		}
		if g.Without {
			anyWithout = true
			return
		}
		for _, name := range g.Groups {
			addRef(name)
		}
	}

	expr.Walk(func(e syntax.Expr) bool {
		switch typedE := e.(type) {
		case *syntax.RangeAggregationExpr:
			// bytes_over_time() and bytes_rate() need the message to compute the line length.
			if typedE.Operation == syntax.OpRangeTypeBytes || typedE.Operation == syntax.OpRangeTypeBytesRate {
				needLine = true
			}
			if typedE.Left != nil && typedE.Left.Unwrap != nil {
				addRef(typedE.Left.Unwrap.Identifier)
			}
			handleGrouping(typedE.Grouping)
		case *syntax.VectorAggregationExpr:
			handleGrouping(typedE.Grouping)
		}
		return true
	})

	// Pipeline stages.
	if sel, err := expr.Selector(); err != nil {
		isComplex = true
	} else if pe, ok := sel.(*syntax.PipelineExpr); ok {
		for _, st := range pe.MultiStages {
			switch s := st.(type) {
			case *syntax.LineFilterExpr:
				needLine = true
			case *syntax.LabelFilterExpr:
				for _, name := range s.LabelFilterer.RequiredLabelNames() {
					addRef(name)
				}
				pushable = append(pushable, pushableMetadataMatchers(s.LabelFilterer, streamLabelNames)...)
			default:
				isComplex = true // parser / formatter / keep / drop / line_format / decolorize
			}
		}
	}

	// The output carries arbitrary metadata unless the top-level aggregation reduces to a known label set.
	allMetadata := isComplex || anyWithout || !reducesOutputLabels(expr)

	projectedColumns = []logs.ColumnType{logs.ColumnTypeStreamID, logs.ColumnTypeTimestamp}
	if allMetadata {
		projectedColumns = append(projectedColumns, logs.ColumnTypeMetadata)
	}
	if isComplex || needLine {
		projectedColumns = append(projectedColumns, logs.ColumnTypeMessage)
	}

	if isComplex {
		return projectedColumns, nil, nil // the pipeline builds labels from the line; push nothing down
	}
	if !allMetadata {
		projectedMetadata = make([]string, 0, len(refKeys))
		for k := range refKeys {
			projectedMetadata = append(projectedMetadata, k)
		}
		sort.Strings(projectedMetadata)
	}
	return projectedColumns, projectedMetadata, metadataMatcherPredicates(pushable)
}

// reducesOutputLabels reports whether the top-level aggregation of expr reduces the output to a bounded
// label set (so unreferenced metadata cannot surface). A `sum` dismisses every label outside its `by`
// grouping; a range aggregation with a `by` grouping reduces to it. A bare range aggregation keeps the
// full label set. `without` groupings are handled by the caller. The engine only sends the vector wrapper
// for `sum`, so other vector aggregations arrive as a bare range aggregation and read all metadata.
func reducesOutputLabels(expr syntax.SampleExpr) bool {
	switch e := expr.(type) {
	case *syntax.VectorAggregationExpr:
		return e.Operation == syntax.OpTypeSum
	case *syntax.RangeAggregationExpr:
		return e.Grouping != nil && !e.Grouping.Without
	default:
		return false
	}
}

// pushableMetadataMatchers returns the string-label matchers in a label filter that are safe to push to
// the reader as metadata predicates.
func pushableMetadataMatchers(f logqllog.LabelFilterer, streamLabelNames map[string]struct{}) []*labels.Matcher {
	canPush := func(m *labels.Matcher) []*labels.Matcher {
		if m == nil {
			return nil
		}

		// A pushed predicate reads only the metadata column, so it must not drop a row the extractor
		// keeps. streamLabelNames is the union of every read stream's labels: a key not in it is metadata
		// for every read row (the column holds the whole value), so the matcher applies to the column
		// alone. A key in it may be a stream label the predicate can't see, so it is left to the extractor.
		// A section that lacks the column is handled by the reader, which reduces the matcher against the
		// empty value, so any matcher (including negation) is safe once the key is not a stream label.
		if _, isStreamLabel := streamLabelNames[m.Name]; isStreamLabel {
			return nil
		}
		return []*labels.Matcher{m}
	}

	switch flt := f.(type) {
	case *logqllog.LineFilterLabelFilter:
		return canPush(flt.Matcher)
	case *logqllog.StringLabelFilter:
		return canPush(flt.Matcher)
	case *logqllog.BinaryLabelFilter:
		if flt.And {
			return append(pushableMetadataMatchers(flt.Left, streamLabelNames), pushableMetadataMatchers(flt.Right, streamLabelNames)...)
		}
	}
	return nil
}

// metadataMatcherPredicates turns metadata matchers into reader predicates.
func metadataMatcherPredicates(matchers []*labels.Matcher) []logs.RowPredicate {
	var predicates []logs.RowPredicate
	for _, m := range matchers {
		if m.Type == labels.MatchEqual {
			// An equality becomes a MetadataMatcherRowPredicate, which the reader can prune pages with
			// via column min/max stats.
			predicates = append(predicates, logs.MetadataMatcherRowPredicate{Key: m.Name, Value: m.Value})
		} else {
			// Any other matcher becomes a MetadataFilterRowPredicate: it cannot prune pages, but it still
			// makes the column a primary column, so the reader fills the message and other secondary columns
			// only for the rows that pass — skipping their reads for the rest.
			predicates = append(predicates, logs.MetadataFilterRowPredicate{Key: m.Name, Keep: func(_, value string) bool { return m.Matches(value) }})
		}
	}
	return predicates
}

// dataObjTaskIterator streams read tasks from the planner to the reader. The planner fills it from a
// background goroutine as it resolves objects; the reader consumes tasks as they arrive. Err returns any
// resolution error once Next has returned false, so a planning failure reaches the reader the same way a
// scan failure does.
//
// Next and At are for the single consumer goroutine only. Err, setErr, and Abort are safe for concurrent
// use: the reader may Abort while the planner still runs.
type dataObjTaskIterator struct {
	tasks <-chan dataObjReadTask
	curr  dataObjReadTask

	// cancel stops the background planner; done is closed once that goroutine has exited. Abort uses them
	// to stop the planner and wait for it, so the shared cache is released only after the planner lets go.
	cancel context.CancelFunc
	done   chan struct{}

	errMu sync.Mutex
	err   error
}

// newDataObjTaskIterator returns an iterator over tasks whose background planner is stopped by cancel. It
// panics if tasks is nil: that is a programming error, and Next would otherwise block forever on it.
func newDataObjTaskIterator(tasks <-chan dataObjReadTask, cancel context.CancelFunc) *dataObjTaskIterator {
	if tasks == nil {
		panic("newDataObjTaskIterator: tasks channel must not be nil")
	}
	return &dataObjTaskIterator{
		tasks:  tasks,
		cancel: cancel,
		done:   make(chan struct{}),
	}
}

func (it *dataObjTaskIterator) Next() bool {
	// A resolution error is terminal: stop at once rather than yield the tasks the planner queued (into
	// the buffer) before it failed, mirroring how the log reader stops on a scan error. This avoids
	// reading sections for a query that is already doomed to fail.
	if it.Err() != nil {
		return false
	}
	task, ok := <-it.tasks
	if !ok {
		return false
	}
	it.curr = task
	return true
}

func (it *dataObjTaskIterator) At() dataObjReadTask { return it.curr }

func (it *dataObjTaskIterator) Err() error {
	it.errMu.Lock()
	defer it.errMu.Unlock()
	return it.err
}

func (it *dataObjTaskIterator) setErr(err error) {
	it.errMu.Lock()
	defer it.errMu.Unlock()
	if it.err == nil {
		it.err = err
	}
}

// Abort records err (if non-nil), stops the background planner, and waits for it to exit. After Abort
// returns, the planner no longer touches the shared cache, so the caller may release it. Abort is
// idempotent and safe to call after a normal drain (cancel is a no-op and done is already closed).
func (it *dataObjTaskIterator) Abort(err error) {
	if err != nil {
		it.setErr(err)
	}
	it.cancel()
	<-it.done
}
