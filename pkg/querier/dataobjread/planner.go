package dataobjread

import (
	"context"
	"fmt"
	"runtime/debug"

	"github.com/go-kit/log/level"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	util_server "github.com/grafana/loki/v3/pkg/util/server"
	"github.com/grafana/loki/v3/pkg/xcap"
)

const (
	// maxParallelObjectResolves caps how many planObject goroutines run at once. Each one reads the
	// streams of one object and sends its tasks.
	maxParallelObjectResolves = 128
)

// Planner turns a metric query into the section-read tasks the reader runs. It takes the sections
// from the metastore, reads their streams to compute stream hashes, and applies the per-stream
// filters.
type Planner struct {
	metastore metastore.Metastore
	objects   *OpenObjects

	// filterer rejects streams a request may not read. It is nil when nothing filters.
	filterer chunk.Filterer
}

func NewPlanner(ms metastore.Metastore, objects *OpenObjects, filterer chunk.Filterer) *Planner {
	return &Planner{metastore: ms, objects: objects, filterer: filterer}
}

// Plan streams the query's read tasks through the returned iterator.
//
// A background goroutine plans the sections of each index object as soon as the metastore has read
// it. The reader can start on one object's logs while the metastore still reads later index
// objects.
//
// The iterator records an error from the metastore or from planning. Err returns it.
func (p *Planner) Plan(ctx context.Context, query QueryParams) *TaskIterator {
	ctx, cancel := context.WithCancel(ctx)
	tasks := make(chan ReadTask, planBufferSize)
	it := newTaskIterator(tasks, cancel)

	go func() {
		defer close(it.done)
		defer close(tasks)

		// Fail the query rather than the process. Every goroutine this package starts recovers:
		// none of them is covered by the server's request-level recovery, and the metastore lookup
		// on this goroutine decodes stored objects, so one malformed object would otherwise take
		// the querier down and abort every other in-flight query.
		defer func() {
			if panicked := recover(); panicked != nil {
				util_server.RecordPanic()

				level.Error(util_log.Logger).Log(
					"msg", "panic planning data object reads",
					"panic", panicked,
					"stack", string(debug.Stack()),
				)
				it.setErr(fmt.Errorf("planning data object reads: %v", panicked))
			}
		}()

		// The object opens and streams reads get a root region of their own. Without one their
		// requests and bytes run outside any region and go uncounted.
		planCtx, _ := xcap.StartRegion(ctx, regionStreamsReader)
		if err := p.planSections(ctx, planCtx, query, tasks); err != nil {
			it.setErr(err)
		}
	}()

	return it
}

// planSections plans the sections the metastore yields, as it yields them.
//
// The metastore yields one batch per index object: the sections that index object lists and no
// earlier batch did. planSections groups a batch by data object and plans the data objects
// concurrently. It sends the tasks of a data object as soon as that object is planned.
//
// ctx reaches the metastore and planCtx reaches the planning goroutines, so their requests count
// in different regions.
func (p *Planner) planSections(ctx, planCtx context.Context, query QueryParams, out chan<- ReadTask) error {
	planCtx, cancelPlans := context.WithCancel(planCtx)
	group, planCtx := errgroup.WithContext(planCtx)
	group.SetLimit(maxParallelObjectResolves)

	// Stop the planning goroutines and wait for them however this function ends, including a
	// panic. The caller closes the out channel after this function returns. A goroutine that still
	// sends would panic on the closed channel.
	defer func() {
		cancelPlans()
		_ = group.Wait()
	}()

	// The metastore yields each section once. A repeat is a metastore bug. The planner then fails
	// the query, because planning a section twice emits its rows twice.
	planned := make(map[metastore.SectionKey]struct{})

	for batch, streamErr := range p.metastore.SectionsStream(ctx, metastore.SectionsRequest{
		Start:           query.Start,
		End:             query.End,
		Matchers:        query.Matchers,
		Predicates:      query.Projection.sectionPredicates(),
		StreamHashRange: query.Shard.hashRange(),
	}) {
		// Planning failed or the query context ended. Returning from the loop cancels the metastore
		// reads and waits for them to end.
		if planCtx.Err() != nil {
			return context.Cause(planCtx)
		}

		if streamErr != nil {
			return fmt.Errorf("resolving data object sections: %w", streamErr)
		}

		for _, descriptor := range batch {
			if _, repeated := planned[descriptor.SectionKey]; repeated {
				return fmt.Errorf(
					"data object %q logs section %d was listed twice, so reading it would count its rows twice",
					descriptor.ObjectPath, descriptor.SectionIdx,
				)
			}
			planned[descriptor.SectionKey] = struct{}{}
		}

		for path, descriptors := range batch.ByObject() {
			group.Go(func() (err error) {
				// Decoding an object can panic on a column whose type does not match its schema.
				// This runs on a goroutine an errgroup deliberately does not recover, so without
				// this the panic takes the querier process down instead of one query.
				defer func() {
					if panicked := recover(); panicked != nil {
						util_server.RecordPanic()

						level.Error(util_log.Logger).Log(
							"msg", "panic planning reads of a data object",
							"object", path,
							"panic", panicked,
							"stack", string(debug.Stack()),
						)
						err = fmt.Errorf("planning reads of data object %q: %v", path, panicked)
					}
				}()

				tasks, err := p.planObject(planCtx, path, descriptors, query)
				if err != nil {
					return err
				}

				for _, task := range tasks {
					select {
					case out <- task:
					case <-planCtx.Done():
						return planCtx.Err()
					}
				}
				return nil
			})
		}
	}
	return group.Wait()
}

// planObject reads the streams of the given sections in one read. It then plans one task for each
// logs section that keeps a stream after filtering.
//
// descriptors must all belong to the object at path. Their stream IDs are only meaningful there,
// because each object's builder assigns its own. Another index object can list the other sections
// of the same data object. planObject then runs again for that object.
func (p *Planner) planObject(ctx context.Context, path string, descriptors metastore.DataobjSectionDescriptors, query QueryParams) ([]ReadTask, error) {
	object, err := p.objects.get(ctx, path)
	if err != nil {
		return nil, err
	}

	// The shard bucket range depends on the shard layout of this object, which can differ from the
	// layout of the other objects of the query.
	buckets, err := object.shardBucketRange(ctx, query.Shard)
	if err != nil {
		return nil, err
	}

	decoded, err := object.streamLabels(ctx, descriptors.StreamIDs(), buckets)
	if err != nil {
		return nil, err
	}

	streams := newObjectStreams(decoded, func(streamLabels labels.Labels, streamHash uint64) bool {
		return p.admits(streamLabels, streamHash, query.Shard, buckets)
	})

	tasks := make([]ReadTask, 0, len(descriptors))
	for _, descriptor := range descriptors {
		task, ok, err := p.planSection(descriptor, streams, query, buckets != nil)
		if err != nil {
			return nil, err
		}
		if ok {
			tasks = append(tasks, task)
		}
	}
	return tasks, nil
}

// planSection plans the read of one logs section. ok is false when no stream of it survives
// filtering, so there is nothing to read.
//
// A stream the metastore lists but the streams read did not return means one of two things,
// which prunedShardBuckets tells apart. prunedShardBuckets reports whether the streams read pushed
// a shard bucket range down.
//
// A read without a shard bucket range returns every listed stream, so a missing one is a broken
// invariant between the index and the object. That fails the query: dropping the stream would
// under-count it silently.
//
// A read with a shard bucket range pushes the shard bucket predicate down, so a missing stream is
// out of shard. One pruned read cannot tell that from a genuinely missing stream, and the extra
// read it would take to find out costs more than the invariant is worth here.
func (p *Planner) planSection(descriptor *metastore.DataobjSectionDescriptor, streams *objectStreams, query QueryParams, prunedShardBuckets bool) (ReadTask, bool, error) {
	if len(descriptor.StreamIDs) == 0 {
		return ReadTask{}, false, fmt.Errorf(
			"data object %q logs section %d: the resolver listed no stream for the section",
			descriptor.ObjectPath, descriptor.SectionIdx,
		)
	}

	var (
		streamIDs  = make([]int64, 0, len(descriptor.StreamIDs))
		labelNames = map[string]struct{}{}
		seen       = map[int64]struct{}{}
	)

	for _, id := range descriptor.StreamIDs {
		if !streams.decoded(id) {
			if prunedShardBuckets {
				continue
			}
			return ReadTask{}, false, fmt.Errorf(
				"data object %q logs section %d: stream ID %d listed by the metastore is missing from the object's streams section",
				descriptor.ObjectPath, descriptor.SectionIdx, id,
			)
		}
		if !streams.admits(id) {
			continue
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		streamIDs = append(streamIDs, id)
		streams.admittedByID[id].streamLabels.Range(func(label labels.Label) { labelNames[label.Name] = struct{}{} })
	}

	if len(streamIDs) == 0 {
		return ReadTask{}, false, nil
	}

	// Decide pushdown against this section's stream labels alone, so the gate stays as narrow
	// as it can be: a key that is a stream label in another section can still be pushed here.
	//
	// The descriptor's AmbiguousPredicates answers almost the same question, but over every
	// stream the metastore matched rather than the ones that survived filtering, so using it
	// would suppress pushdown this can keep.
	columns, metadataNames, predicates := query.Projection.forStreams(labelNames)
	return ReadTask{
		objectPath:        descriptor.ObjectPath,
		sectionIdx:        int(descriptor.SectionIdx),
		streamIDs:         streamIDs,
		streams:           streams,
		columns:           columns,
		metadataNames:     metadataNames,
		predicates:        predicates,
		needReturningLine: query.Projection.needReturningLine,
		start:             query.Start,
		end:               query.End,
	}, true, nil
}

// admits reports whether a stream passes the query's per-stream filters. buckets is the shard
// bucket range that the streams read of the object pushed down, or nil if it pushed none.
func (p *Planner) admits(streamLabels labels.Labels, streamHash uint64, shard *QueryShard, buckets *shardBucketRange) bool {
	if p.filterer != nil && p.filterer.ShouldFilter(streamLabels) {
		return false
	}

	// The shard bucket range resolves a shard exactly only for a power-of-two shard of at most the
	// shard bucket count of the object. Any other shard maps to a range that covers streams outside
	// it, or to no range, so the stream hash still has to be checked.
	if shard != nil && !buckets.isExact() {
		if !shard.assignment.Match(model.Fingerprint(streamHash)) {
			return false
		}
	}

	return true
}
