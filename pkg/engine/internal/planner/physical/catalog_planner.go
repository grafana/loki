package physical

import (
	"context"
	"fmt"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/engine/internal/util/dag"
	"github.com/oklog/ulid/v2"
)

type MetastorePlanner struct {
	metastore metastore.Metastore
}

func NewMetastorePlanner(metastore metastore.Metastore) MetastorePlanner {
	return MetastorePlanner{
		metastore: metastore,
	}
}

func (p MetastorePlanner) Plan(ctx context.Context, req CatalogRequest) (*Plan, error) {
	plan := &Plan{
		graph: dag.Graph[Node]{},
	}

	indexPaths, err := p.metastore.GetIndexes(ctx, req.From, req.Through)
	if err != nil {
		return nil, fmt.Errorf("metastore plan failed: %w", err)
	}

	parallelize := &Parallelize{
		NodeID: ulid.Make(),
	}
	plan.graph.Add(parallelize)

	// TODO(ivkalita): I'm adding a nested parallelize node for workflow builder to actually split the ScanSet to
	//                 multiple tasks. Obviously, it's a hack, we need to support it properly.
	noop := &Parallelize{
		NodeID: ulid.Make(),
	}
	plan.graph.Add(noop)

	scanSet := &ScanSet{
		NodeID: ulid.Make(),
	}
	plan.graph.Add(scanSet)

	for _, indexPath := range indexPaths {
		scanSet.Targets = append(scanSet.Targets, &ScanTarget{
			Type: ScanTypePointers,
			PointersScan: &PointersScan{
				NodeID:   ulid.Make(),
				Location: DataObjLocation(indexPath),

				Selector: req.Selector,

				Start: req.From,
				End:   req.Through,

				MaxTimeRange: TimeRange{
					Start: req.From,
					End:   req.Through,
				},
			},
		})
	}

	if err := plan.graph.AddEdge(dag.Edge[Node]{Parent: parallelize, Child: noop}); err != nil {
		return nil, err
	}

	if err := plan.graph.AddEdge(dag.Edge[Node]{Parent: noop, Child: scanSet}); err != nil {
		return nil, err
	}

	return plan, nil
}
