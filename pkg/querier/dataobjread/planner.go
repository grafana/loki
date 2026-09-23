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
)

const (
	// maxParallelObjectResolves is how many objects the planner resolves at once, where one
	// resolve is an object open plus a streams-section read.
	maxParallelObjectResolves = 128
)

// Planner turns a metric query into the section-read tasks the reader runs. It
// resolves the sections, reads each object's streams to compute stream hashes, and applies the
// per-stream filters.
type Planner struct {
	metastore metastore.Metastore
	objects   *OpenObjects

	// filterer rejects streams a request may not read. It is nil when nothing filters.
	filterer chunk.Filterer
}

func NewPlanner(ms metastore.Metastore, objects *OpenObjects, filterer chunk.Filterer) *Planner {
	return &Planner{metastore: ms, objects: objects, filterer: filterer}
}

// Plan resolves the query's sections and streams the resulting tasks through the returned
// iterator.
//
// Resolution and the per-object streams reads run in a background goroutine, so the reader can
// start on one object's logs while later objects still resolve. A resolution error is recorded
// on the iterator and surfaces through its Err.
func (p *Planner) Plan(ctx context.Context, query QueryParams) *TaskIterator {
	ctx, cancel := context.WithCancel(ctx)
	tasks := make(chan ReadTask, planBufferSize)
	it := newTaskIterator(tasks, cancel)

	go func() {
		defer close(it.done)
		defer close(tasks)

		// Fail the query rather than the process. Every goroutine this package starts recovers:
		// none of them is covered by the server's request-level recovery, and resolution decodes
		// stored index objects, so one malformed object would otherwise take the querier down
		// and abort every other in-flight query.
		defer func() {
			if panicked := recover(); panicked != nil {
				level.Error(util_log.Logger).Log(
					"msg", "panic planning data object reads",
					"panic", panicked,
					"stack", string(debug.Stack()),
				)
				it.setErr(fmt.Errorf("planning data object reads: %v", panicked))
			}
		}()

		resolved, err := p.metastore.Sections(ctx, metastore.SectionsRequest{
			Start:      query.Start,
			End:        query.End,
			Matchers:   query.Matchers,
			Predicates: query.Projection.sectionPredicates(),
		})
		if err != nil {
			it.setErr(fmt.Errorf("resolving data object sections: %w", err))
			return
		}

		if err := p.planObjects(ctx, resolved.Sections, query, tasks); err != nil {
			it.setErr(err)
		}
	}()

	return it
}

// planObjects groups the descriptors by object and plans each object concurrently, sending an
// object's tasks as soon as it is planned.
func (p *Planner) planObjects(ctx context.Context, descriptors metastore.DataobjSectionDescriptors, query QueryParams, out chan<- ReadTask) error {
	group, ctx := errgroup.WithContext(ctx)
	group.SetLimit(maxParallelObjectResolves)

	for path, objectDescriptors := range descriptors.ByObject() {
		group.Go(func() (err error) {
			// Decoding an object can panic on a column whose type does not match its schema.
			// This runs on a goroutine an errgroup deliberately does not recover, so without
			// this the panic takes the querier process down instead of one query.
			defer func() {
				if panicked := recover(); panicked != nil {
					level.Error(util_log.Logger).Log(
						"msg", "panic planning reads of a data object",
						"object", path,
						"panic", panicked,
						"stack", string(debug.Stack()),
					)
					err = fmt.Errorf("planning reads of data object %q: %v", path, panicked)
				}
			}()

			tasks, err := p.planObject(ctx, path, objectDescriptors, query)
			if err != nil {
				return err
			}

			for _, task := range tasks {
				select {
				case out <- task:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return nil
		})
	}

	return group.Wait()
}

// planObject reads one object's streams once, then plans a task per logs section of it.
//
// descriptors must all belong to the object at path. Their stream IDs are only meaningful there,
// because each object's builder assigns its own.
func (p *Planner) planObject(ctx context.Context, path string, descriptors metastore.DataobjSectionDescriptors, query QueryParams) ([]ReadTask, error) {
	object, err := p.objects.get(ctx, path)
	if err != nil {
		return nil, err
	}

	// One task per section. The metastore concatenates the descriptors of every index object
	// without merging across them, so two index objects describing one section would plan it
	// twice and emit every one of its rows twice. Check before the streams read below, which
	// costs a round trip to object storage.
	planned := make(map[int64]struct{}, len(descriptors))
	for _, descriptor := range descriptors {
		if _, repeated := planned[descriptor.SectionIdx]; repeated {
			return nil, fmt.Errorf(
				"data object %q logs section %d was listed twice, so reading it would count its rows twice",
				path, descriptor.SectionIdx,
			)
		}
		planned[descriptor.SectionIdx] = struct{}{}
	}

	decoded, err := object.streamLabels(ctx, descriptors.StreamIDs(), query.Shard.bucketRange())
	if err != nil {
		return nil, err
	}

	streams := newObjectStreams(decoded, func(streamLabels labels.Labels, streamHash uint64) bool {
		return p.admits(streamLabels, streamHash, query.Shard)
	})

	tasks := make([]ReadTask, 0, len(descriptors))
	for _, descriptor := range descriptors {
		task, ok, err := p.planSection(descriptor, streams, query)
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
// which the query's shard tells apart.
//
// An unsharded read returns every listed stream, so a missing one is a broken invariant between
// the index and the object. That fails the query: dropping the stream would under-count it
// silently.
//
// A sharded read pushes the bucket predicate down, so a missing stream is out of shard. One
// pruned read cannot tell that from a genuinely missing stream, and the extra read it would take
// to find out costs more than the invariant is worth here.
func (p *Planner) planSection(descriptor *metastore.DataobjSectionDescriptor, streams *objectStreams, query QueryParams) (ReadTask, bool, error) {
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
			if query.Shard.prunes() {
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
		objectPath:    descriptor.ObjectPath,
		sectionIdx:    int(descriptor.SectionIdx),
		streamIDs:     streamIDs,
		streams:       streams,
		columns:       columns,
		metadataNames: metadataNames,
		predicates:    predicates,
		start:         query.Start,
		end:           query.End,
	}, true, nil
}

// admits reports whether a stream passes the query's per-stream filters.
func (p *Planner) admits(streamLabels labels.Labels, streamHash uint64, shard *QueryShard) bool {
	if p.filterer != nil && p.filterer.ShouldFilter(streamLabels) {
		return false
	}

	// The bucket range resolves a shard exactly only for a power-of-two shard of at most
	// streams.ShardFactor. Any other shard maps to a range that covers streams outside it, so
	// the stream hash still has to be checked.
	if shard != nil && !shard.resolvesExactly() {
		if !shard.assignment.Match(model.Fingerprint(streamHash)) {
			return false
		}
	}

	return true
}
