package executor

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/grafana/loki/v3/pkg/dataobj"
	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
)

// executeIndexFilter copies the rows of the node's log objects from the
// source index into a new index run, and emits a result record that reports
// the index objects of the run.
func (c *Context) executeIndexFilter(node *physical.IndexFilter) Pipeline {
	return newLazyPipeline(func(ctx context.Context) Pipeline {
		artifacts, err := c.doIndexFilter(ctx, node)
		if err != nil {
			return errorPipeline(ctx, err)
		}
		rec, err := v2.EncodeResultArtifacts(memory.DefaultAllocator, artifacts)
		if err != nil {
			return errorPipeline(ctx, err)
		}
		return NewBufferedPipeline(rec)
	})
}

func (c *Context) doIndexFilter(ctx context.Context, node *physical.IndexFilter) ([]v2.ResultArtifact, error) {
	if c.bucket == nil {
		return nil, errors.New("no object store bucket configured")
	}
	if node.Tenant == "" || len(node.SourceIndexPaths) == 0 || slices.Contains(node.SourceIndexPaths, "") {
		return nil, fmt.Errorf("IndexFilter: malformed plan: tenant %q, source index %q", node.Tenant, node.SourceIndexPaths)
	}
	if len(node.ObjectPaths) == 0 {
		return nil, errors.New("IndexFilter: no object paths to keep")
	}
	source := strings.Join(node.SourceIndexPaths, ",")

	sources, layout, err := c.openFilterSources(ctx, node)
	if err != nil {
		return nil, err
	}
	out, err := c.newIndexOutput(node.Tenant, layout)
	if err != nil {
		return nil, err
	}
	defer out.reset()

	keepSet := make(map[string]struct{}, len(node.ObjectPaths))
	for _, path := range node.ObjectPaths {
		keepSet[path] = struct{}{}
	}
	keep := func(found map[string]struct{}) func(string) bool {
		return func(path string) bool {
			if _, ok := keepSet[path]; !ok {
				return false
			}
			found[path] = struct{}{}
			return true
		}
	}

	foundPostings := make(map[string]struct{}, len(keepSet))
	if err := c.mergePostingsInto(ctx, filterRuns(node, sources), postings.KeySpan{}, keep(foundPostings), out); err != nil {
		return nil, fmt.Errorf("filtering postings of source index %q: %w", source, err)
	}
	foundStats := make(map[string]struct{}, len(keepSet))
	if err := c.mergeStatsInto(ctx, sources, keep(foundStats), out); err != nil {
		return nil, fmt.Errorf("filtering stats of source index %q: %w", source, err)
	}

	// A kept object without postings rows or without stats rows means the
	// planner and the source index disagree. Fail rather than publish an
	// index that cannot find the object, or cannot plan it.
	for _, path := range node.ObjectPaths {
		if _, ok := foundPostings[path]; !ok {
			return nil, fmt.Errorf("source index %q has no postings rows for object %q", source, path)
		}
		if _, ok := foundStats[path]; !ok {
			return nil, fmt.Errorf("source index %q has no stats rows for object %q", source, path)
		}
	}

	artifacts, _, err := out.finish(ctx)
	if err != nil {
		return nil, fmt.Errorf("index filter output for source index %q: %w", source, err)
	}
	return artifacts, nil
}

// filterRuns returns the runs of postings sections that the filter merges.
// Sources in key order form one run, so the merge holds one postings section
// reader at a time. The merge then fails if they are not in key order.
// Otherwise each source is a run of its own, and the merge accepts any order.
func filterRuns(node *physical.IndexFilter, sources []objectSections) [][]sourceSection {
	if !node.SourcesInKeyOrder {
		runs := make([][]sourceSection, len(sources))
		for i, source := range sources {
			runs[i] = sectionsOf(source.path, source.postings)
		}
		return runs
	}
	var run []sourceSection
	for _, source := range sources {
		run = append(run, sectionsOf(source.path, source.postings)...)
	}
	return [][]sourceSection{run}
}

// openFilterSources opens the source index objects of node and returns their
// postings and stats sections of the tenant, and their stream layout.
//
// It returns an error if a source index object holds a section of the tenant
// of another type than postings, stats or streams. It also returns an error if
// the sources record different layouts, or if only some record one.
//
// If no source index object records a layout, the sources hold no stats
// sections of the tenant, and the layout is the zero layout.
func (c *Context) openFilterSources(ctx context.Context, node *physical.IndexFilter) ([]objectSections, streams.Layout, error) {
	var (
		sources []objectSections
		layout  sourceLayout
	)
	for _, path := range node.SourceIndexPaths {
		obj, err := dataobj.FromBucket(ctx, c.bucket, path, indexPrefetchBytes)
		if err != nil {
			return nil, streams.Layout{}, fmt.Errorf("opening source index %q: %w", path, err)
		}

		group := objectSections{path: path}
		for _, sec := range obj.Sections() {
			if sec.Tenant != node.Tenant {
				continue
			}
			switch {
			case postings.CheckSection(sec):
				group.postings = append(group.postings, sec)
			case stats.CheckSection(sec):
				group.stats = append(group.stats, sec)
			case streams.CheckSection(sec):
			default:
				return nil, streams.Layout{}, fmt.Errorf("filtering source index %q: unknown section %q", path, sec.Type)
			}
		}

		if err := layout.add(ctx, path, obj, node.Tenant); err != nil {
			return nil, streams.Layout{}, err
		}
		sources = append(sources, group)
	}
	shared, err := layout.shared()
	if err != nil {
		return nil, streams.Layout{}, err
	}
	return sources, shared, nil
}
