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
}

func NewMetastorePlanner(metastore metastore.Metastore) MetastorePlanner {
	return MetastorePlanner{
		metastore: metastore,
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

	merge := &Merge{
		NodeID: ulid.Make(),
	}
	plan.graph.Add(merge)

	parallelize := &Parallelize{
		NodeID: ulid.Make(),
	}
	plan.graph.Add(parallelize)

	scanSet := &ScanSet{
		NodeID: ulid.Make(),
	}
	plan.graph.Add(scanSet)

	for _, indexPath := range indexesResp.IndexesPaths {
		scanSet.Targets = append(scanSet.Targets, &ScanTarget{
			Type: ScanTypePointers,
			Pointers: &PointersScan{
				NodeID:   ulid.Make(),
				Location: DataObjLocation(indexPath),

				Selector:   selector,
				Predicates: predicates,

				Start: start,
				End:   end,

				MaxTimeRange: TimeRange{
					Start: start,
					End:   end,
				},
			},
		})
	}

	if err := plan.graph.AddEdge(dag.Edge[Node]{Parent: merge, Child: parallelize}); err != nil {
		return nil, err
	}

	if err := plan.graph.AddEdge(dag.Edge[Node]{Parent: parallelize, Child: scanSet}); err != nil {
		return nil, err
	}

	return plan, nil
}
