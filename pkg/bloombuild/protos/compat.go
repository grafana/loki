package protos

import (
	"fmt"

	"github.com/go-kit/log"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/logproto"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
)

type Gap struct {
	Bounds v1.FingerprintBounds
	Series []*v1.Series
	Blocks []bloomshipper.BlockRef
}

type Task struct {
	ID string

	Table           config.DayTable
	Tenant          string
	OwnershipBounds v1.FingerprintBounds
	TSDB            tsdb.SingleTenantTSDBIdentifier
	Gaps            []Gap
}

func NewTask(
	table config.DayTable,
	tenant string,
	bounds v1.FingerprintBounds,
	tsdb tsdb.SingleTenantTSDBIdentifier,
	gaps []Gap,
) *Task {
	return &Task{
		ID: fmt.Sprintf("%s-%s-%s-%d", table.Addr(), tenant, bounds.String(), len(gaps)),

		Table:           table,
		Tenant:          tenant,
		OwnershipBounds: bounds,
		TSDB:            tsdb,
		Gaps:            gaps,
	}
}

func FromProtoTask(task *ProtoTask) (*Task, error) {
	if task == nil {
		return nil, nil
	}

	tsdbRef, ok := tsdb.ParseSingleTenantTSDBPath(task.Tsdb)
	if !ok {
		return nil, fmt.Errorf("failed to parse tsdb path %s", task.Tsdb)
	}

	gaps := make([]Gap, 0, len(task.Gaps))
	for _, gap := range task.Gaps {
		bounds := v1.FingerprintBounds{
			Min: gap.Bounds.Min,
			Max: gap.Bounds.Max,
		}

		series := make([]*v1.Series, 0, len(gap.Series))
		for _, s := range gap.Series {
			chunks := make(v1.ChunkRefs, 0, len(s.Chunks))
			for _, c := range s.Chunks {
				chunks = append(chunks, v1.ChunkRef(*c))
			}
			series = append(series, &v1.Series{
				Fingerprint: model.Fingerprint(s.Fingerprint),
				Chunks:      chunks,
			})
		}

		blocks := make([]bloomshipper.BlockRef, 0, len(gap.BlockRef))
		for _, block := range gap.BlockRef {
			b, err := bloomshipper.BlockRefFromKey(block)
			if err != nil {
				return nil, fmt.Errorf("failed to parse block ref %s: %w", block, err)
			}

			blocks = append(blocks, b)
		}
		gaps = append(gaps, Gap{
			Bounds: bounds,
			Series: series,
			Blocks: blocks,
		})
	}

	return &Task{
		ID:     task.Id,
		Table:  config.NewDayTable(config.NewDayTime(model.Time(task.Table.DayTimestampMS)), task.Table.Prefix),
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
	if t == nil {
		return nil
	}

	protoGaps := make([]*ProtoGapWithBlocks, 0, len(t.Gaps))
	for _, gap := range t.Gaps {
		blockRefs := make([]string, 0, len(gap.Blocks))
		for _, block := range gap.Blocks {
			blockRefs = append(blockRefs, block.String())
		}

		series := make([]*ProtoSeries, 0, len(gap.Series))
		for _, s := range gap.Series {
			chunks := make([]*logproto.ShortRef, 0, len(s.Chunks))
			for _, c := range s.Chunks {
				chunk := logproto.ShortRef(c)
				chunks = append(chunks, &chunk)
			}

			series = append(series, &ProtoSeries{
				Fingerprint: uint64(s.Fingerprint),
				Chunks:      chunks,
			})
		}

		protoGaps = append(protoGaps, &ProtoGapWithBlocks{
			Bounds: ProtoFingerprintBounds{
				Min: gap.Bounds.Min,
				Max: gap.Bounds.Max,
			},
			Series:   series,
			BlockRef: blockRefs,
		})
	}

	return &ProtoTask{
		Id: t.ID,
		Table: DayTable{
			DayTimestampMS: int64(t.Table.Time),
			Prefix:         t.Table.Prefix,
		},
		Tenant: t.Tenant,
		Bounds: ProtoFingerprintBounds{
			Min: t.OwnershipBounds.Min,
			Max: t.OwnershipBounds.Max,
		},
		Tsdb: t.TSDB.Path(),
		Gaps: protoGaps,
	}
}

func (t *Task) GetLogger(logger log.Logger) log.Logger {
	return log.With(logger,
		"task", t.ID,
		"tenant", t.Tenant,
		"table", t.Table.String(),
		"tsdb", t.TSDB.Name(),
	)
}

type TaskResult struct {
	TaskID       string
	Error        error
	CreatedMetas []bloomshipper.Meta
}

func FromProtoTaskResult(result *ProtoTaskResult) (*TaskResult, error) {
	if result == nil {
		return nil, nil
	}

	if result.Error != "" {
		return &TaskResult{
			TaskID: result.TaskID,
			Error:  errors.New(result.Error),
		}, nil
	}

	metas := make([]bloomshipper.Meta, 0, len(result.CreatedMetas))
	for _, meta := range result.CreatedMetas {
		metaRef, err := bloomshipper.MetaRefFromKey(meta.MetaRef)
		if err != nil {
			return nil, fmt.Errorf("failed to parse meta ref %v: %w", meta, err)
		}

		sources := make([]tsdb.SingleTenantTSDBIdentifier, 0, len(meta.SourcesTSDBs))
		for _, source := range meta.SourcesTSDBs {
			tsdbRef, ok := tsdb.ParseSingleTenantTSDBPath(source)
			if !ok {
				return nil, fmt.Errorf("failed to parse tsdb path %s", source)
			}
			sources = append(sources, tsdbRef)
		}

		blockRefs := make([]bloomshipper.BlockRef, 0, len(meta.BlockRefs))
		for _, block := range meta.BlockRefs {
			b, err := bloomshipper.BlockRefFromKey(block)
			if err != nil {
				return nil, fmt.Errorf("failed to parse block ref %s: %w", block, err)
			}

			blockRefs = append(blockRefs, b)
		}

		metas = append(metas, bloomshipper.Meta{
			MetaRef: metaRef,
			Sources: sources,
			Blocks:  blockRefs,
		})
	}

	return &TaskResult{
		TaskID:       result.TaskID,
		CreatedMetas: metas,
	}, nil
}

func (r *TaskResult) ToProtoTaskResult() *ProtoTaskResult {
	if r == nil {
		return nil
	}

	if r.Error != nil {
		return &ProtoTaskResult{
			TaskID: r.TaskID,
			Error:  r.Error.Error(),
		}
	}

	protoMetas := make([]*ProtoMeta, 0, len(r.CreatedMetas))
	for _, meta := range r.CreatedMetas {
		metaRefs := make([]string, 0, len(meta.Sources))
		for _, source := range meta.Sources {
			metaRefs = append(metaRefs, source.Path())
		}

		blockRefs := make([]string, 0, len(meta.Blocks))
		for _, block := range meta.Blocks {
			blockRefs = append(blockRefs, block.String())
		}

		protoMetas = append(protoMetas, &ProtoMeta{
			MetaRef:      meta.String(),
			SourcesTSDBs: metaRefs,
			BlockRefs:    blockRefs,
		})
	}

	return &ProtoTaskResult{
		TaskID:       r.TaskID,
		CreatedMetas: protoMetas,
	}
}

func FromProtoDayTableToDayTable(proto DayTable) config.DayTable {
	return config.NewDayTable(config.NewDayTime(model.Time(proto.DayTimestampMS)), proto.Prefix)
}
