package physical

import (
	"context"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
)

type MetastorePlanner struct {
	metastore metastore.Metastore

	// PointersScansPerInnerMerge is the number of PointersScan targets per inner Merge node.
	// When 0, the plan is a single Merge → Parallelize → ScanSet with all targets.
	// When > 0, the plan is a two-level tree: Top Merge → K Inner Merges, each Inner Merge → Parallelize → ScanSet(chunk).
	PointersScansPerInnerMerge int
}

func NewMetastorePlanner(metastore metastore.Metastore, pointersScansPerInnerMerge int) MetastorePlanner {
	return MetastorePlanner{
		metastore:                  metastore,
		PointersScansPerInnerMerge: pointersScansPerInnerMerge,
	}
}

func (p MetastorePlanner) Plan(ctx context.Context, selector Expression, predicates []Expression, start time.Time, end time.Time) (*Plan, error) {
	plan := &Plan{
		graph: dag.Graph[Node]{},
	}

	indexesResp, err := p.metastore.GetIndexes(ctx, metastore.GetIndexesRequest{
		Start: start,
		End:   end,
	})
	if err != nil {
		return nil, fmt.Errorf("metastore plan failed: %w", err)
	}

	paths := indexesResp.IndexesPaths
	if p.PointersScansPerInnerMerge <= 0 || len(paths) == 0 {
		return p.planWithSingleMerge(plan, paths, selector, predicates, start, end)
	}
	return p.planWithMultiMerges(plan, paths, selector, predicates, start, end)
}

// planWithSingleMerge builds Merge → Parallelize → ScanSet with all targets.
func (p MetastorePlanner) planWithSingleMerge(plan *Plan, paths []string, selector Expression, predicates []Expression, start, end time.Time) (*Plan, error) {
	merge := &Merge{NodeID: ulid.Make()}
	plan.graph.Add(merge)

	parallelize := &Parallelize{NodeID: ulid.Make()}
	plan.graph.Add(parallelize)

	scanSet := &ScanSet{NodeID: ulid.Make()}
	plan.graph.Add(scanSet)

	for _, indexPath := range paths {
		scanSet.Targets = append(scanSet.Targets, p.pointersTarget(indexPath, selector, predicates, start, end))
	}

	if err := plan.graph.AddEdge(dag.Edge[Node]{Parent: merge, Child: parallelize}); err != nil {
		return nil, err
	}
	if err := plan.graph.AddEdge(dag.Edge[Node]{Parent: parallelize, Child: scanSet}); err != nil {
		return nil, err
	}
	return plan, nil
}

// planWithMultiMerges builds Top Merge → K Inner Merges, each Inner Merge → Parallelize → ScanSet(chunk).
func (p MetastorePlanner) planWithMultiMerges(plan *Plan, paths []string, selector Expression, predicates []Expression, start, end time.Time) (*Plan, error) {
	topMerge := &Merge{NodeID: ulid.Make()}
	plan.graph.Add(topMerge)

	batchSize := p.PointersScansPerInnerMerge
	for i := 0; i < len(paths); i += batchSize {
		endIdx := i + batchSize
		if endIdx > len(paths) {
			endIdx = len(paths)
		}
		batch := paths[i:endIdx]

		innerMerge := &Merge{NodeID: ulid.Make()}
		plan.graph.Add(innerMerge)

		parallelize := &Parallelize{NodeID: ulid.Make()}
		plan.graph.Add(parallelize)

		scanSet := &ScanSet{NodeID: ulid.Make()}
		plan.graph.Add(scanSet)

		for _, indexPath := range batch {
			scanSet.Targets = append(scanSet.Targets, p.pointersTarget(indexPath, selector, predicates, start, end))
		}

		if err := plan.graph.AddEdge(dag.Edge[Node]{Parent: innerMerge, Child: parallelize}); err != nil {
			return nil, err
		}
		if err := plan.graph.AddEdge(dag.Edge[Node]{Parent: parallelize, Child: scanSet}); err != nil {
			return nil, err
		}
		if err := plan.graph.AddEdge(dag.Edge[Node]{Parent: topMerge, Child: innerMerge}); err != nil {
			return nil, err
		}
	}

	return plan, nil
}

func (p MetastorePlanner) pointersTarget(indexPath string, selector Expression, predicates []Expression, start, end time.Time) *ScanTarget {
	return &ScanTarget{
		Type: ScanTypePointers,
		Pointers: &PointersScan{
			NodeID:     ulid.Make(),
			Location:   DataObjLocation(indexPath),
			Selector:   selector,
			Predicates: predicates,
			Start:      start,
			End:        end,
		},
	}
}
