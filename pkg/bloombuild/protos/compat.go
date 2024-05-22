package protos

import (
	"fmt"

	"github.com/google/uuid"

	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
)

type GapWithBlocks struct {
	Bounds v1.FingerprintBounds
	Blocks []bloomshipper.BlockRef
}

type Task struct {
	ID string

	Table           string
	Tenant          string
	OwnershipBounds v1.FingerprintBounds
	TSDB            tsdb.SingleTenantTSDBIdentifier
	Gaps            []GapWithBlocks
}

func NewTask(table, tenant string, bounds v1.FingerprintBounds, tsdb tsdb.SingleTenantTSDBIdentifier, gaps []GapWithBlocks) *Task {
	return &Task{
		ID: uuid.NewString(),

		Table:           table,
		Tenant:          tenant,
		OwnershipBounds: bounds,
		TSDB:            tsdb,
		Gaps:            gaps,
	}
}

// TODO: Use it in the builder to parse the task
func FromProtoTask(task ProtoTask) (*Task, error) {
	tsdbRef, ok := tsdb.ParseSingleTenantTSDBPath(task.Tsdb)
	if !ok {
		return nil, fmt.Errorf("failed to parse tsdb path %s", task.Tsdb)
	}

	gaps := make([]GapWithBlocks, 0, len(task.Gaps))
	for _, gap := range task.Gaps {
		bounds := v1.FingerprintBounds{
			Min: gap.Bounds.Min,
			Max: gap.Bounds.Max,
		}
		blocks := make([]bloomshipper.BlockRef, 0, len(gap.BlockRef))
		for _, block := range gap.BlockRef {
			b, err := bloomshipper.BlockRefFromKey(block)
			if err != nil {
				return nil, fmt.Errorf("failed to parse block ref %s: %w", block, err)
			}

			blocks = append(blocks, b)
		}
		gaps = append(gaps, GapWithBlocks{
			Bounds: bounds,
			Blocks: blocks,
		})
	}

	return &Task{
		ID:     task.Id,
		Table:  task.Table,
		Tenant: task.Tenant,
		OwnershipBounds: v1.FingerprintBounds{
			Min: task.Bounds.Min,
			Max: task.Bounds.Max,
		},
		TSDB: tsdbRef,
		Gaps: gaps,
	}, nil
}

func (t *Task) ToProtoTask() *ProtoTask {
	protoGaps := make([]*ProtoGapWithBlocks, 0, len(t.Gaps))
	for _, gap := range t.Gaps {
		blockRefs := make([]string, 0, len(gap.Blocks))
		for _, block := range gap.Blocks {
			blockRefs = append(blockRefs, block.String())
		}

		protoGaps = append(protoGaps, &ProtoGapWithBlocks{
			Bounds: ProtoFingerprintBounds{
				Min: gap.Bounds.Min,
				Max: gap.Bounds.Max,
			},
			BlockRef: blockRefs,
		})
	}

	return &ProtoTask{
		Id:     t.ID,
		Table:  t.Table,
		Tenant: t.Tenant,
		Bounds: ProtoFingerprintBounds{
			Min: t.OwnershipBounds.Min,
			Max: t.OwnershipBounds.Max,
		},
		Tsdb: t.TSDB.Path(),
		Gaps: protoGaps,
	}
}
