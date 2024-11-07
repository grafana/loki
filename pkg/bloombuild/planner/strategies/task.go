package strategies

import (
	"context"
	"fmt"
	"math"
	"slices"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/bloombuild/common"
	"github.com/grafana/loki/v3/pkg/bloombuild/protos"
	"github.com/grafana/loki/v3/pkg/logproto"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

type Gap struct {
	Bounds v1.FingerprintBounds
	Series []model.Fingerprint
	Blocks []bloomshipper.BlockRef
}

// Task represents a task that is enqueued in the planner.
type Task struct {
	ID              string
	Table           config.DayTable
	Tenant          string
	OwnershipBounds v1.FingerprintBounds
	tsdbIdentifier  tsdb.SingleTenantTSDBIdentifier
	forSeries       common.ClosableForSeries
	Gaps            []Gap
}

func NewTask(
	table config.DayTable,
	tenant string,
	bounds v1.FingerprintBounds,
	tsdb tsdb.SingleTenantTSDBIdentifier,
	forSeries common.ClosableForSeries,
	gaps []Gap,
) *Task {
	return &Task{
		ID:              fmt.Sprintf("%s-%s-%s-%d", table.Addr(), tenant, bounds.String(), len(gaps)),
		Table:           table,
		Tenant:          tenant,
		OwnershipBounds: bounds,
		tsdbIdentifier:  tsdb,
		forSeries:       forSeries,
		Gaps:            gaps,
	}
}

// TODO: move to planner.Task and pass forSeries comming from the planner.
// ToProtoTask converts a Task to a ProtoTask.
// It will use the opened TSDB to get the chunks for the series in the gaps.
func (t *Task) ToProtoTask(ctx context.Context) (*protos.ProtoTask, error) {
	if t == nil {
		return nil, nil
	}

	protoGaps := make([]*protos.ProtoGapWithBlocks, 0, len(t.Gaps))
	for _, gap := range t.Gaps {
		blockRefs := make([]string, 0, len(gap.Blocks))
		for _, block := range gap.Blocks {
			blockRefs = append(blockRefs, block.String())
		}

		if !slices.IsSorted(gap.Series) {
			slices.Sort(gap.Series)
		}

		series := make([]*protos.ProtoSeries, 0, len(gap.Series))
		if err := t.forSeries.ForSeries(
			ctx,
			t.Tenant,
			gap.Bounds,
			0, math.MaxInt64,
			func(_ labels.Labels, fp model.Fingerprint, chks []index.ChunkMeta) (stop bool) {
				select {
				case <-ctx.Done():
					return true
				default:
					// Skip this series if it's not in the gap.
					// Series are sorted, so we can break early.
					if _, found := slices.BinarySearch(gap.Series, fp); !found {
						return false
					}

					chunks := make([]*logproto.ShortRef, 0, len(chks))
					for _, chk := range chks {
						chunks = append(chunks, &logproto.ShortRef{
							From:     model.Time(chk.MinTime),
							Through:  model.Time(chk.MaxTime),
							Checksum: chk.Checksum,
						})
					}

					series = append(series, &protos.ProtoSeries{
						Fingerprint: uint64(fp),
						Chunks:      chunks,
					})
					return false
				}
			},
			labels.MustNewMatcher(labels.MatchEqual, "", ""),
		); err != nil {
			return nil, fmt.Errorf("failed to load series from TSDB for gap (%s): %w", gap.Bounds.String(), err)
		}

		protoGaps = append(protoGaps, &protos.ProtoGapWithBlocks{
			Bounds: protos.ProtoFingerprintBounds{
				Min: gap.Bounds.Min,
				Max: gap.Bounds.Max,
			},
			Series:   series,
			BlockRef: blockRefs,
		})
	}

	return &protos.ProtoTask{
		Id: t.ID,
		Table: protos.DayTable{
			DayTimestampMS: int64(t.Table.Time),
			Prefix:         t.Table.Prefix,
		},
		Tenant: t.Tenant,
		Bounds: protos.ProtoFingerprintBounds{
			Min: t.OwnershipBounds.Min,
			Max: t.OwnershipBounds.Max,
		},
		Tsdb: t.tsdbIdentifier.Path(),
		Gaps: protoGaps,
	}, nil
}
