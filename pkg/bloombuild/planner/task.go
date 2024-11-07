package planner

import (
	"context"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/bloombuild/common"
	"github.com/grafana/loki/v3/pkg/bloombuild/planner/strategies"
	"github.com/grafana/loki/v3/pkg/bloombuild/protos"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

type QueueTask struct {
	*strategies.Task

	// We use forSeries in ToProtoTask to get the chunks for the series in the gaps.
	forSeries common.ClosableForSeries

	resultsChannel chan *protos.TaskResult

	// Tracking
	timesEnqueued atomic.Int64
	queueTime     time.Time
	ctx           context.Context
}

func NewQueueTask(
	ctx context.Context,
	queueTime time.Time,
	task *strategies.Task,
	forSeries common.ClosableForSeries,
	resultsChannel chan *protos.TaskResult,
) *QueueTask {
	return &QueueTask{
		Task:           task,
		resultsChannel: resultsChannel,
		ctx:            ctx,
		queueTime:      queueTime,
		forSeries:      forSeries,
	}
}

// ToProtoTask converts a Task to a ProtoTask.
// It will use the opened TSDB to get the chunks for the series in the gaps.
func (t *QueueTask) ToProtoTask(ctx context.Context) (*protos.ProtoTask, error) {
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
		Tsdb: t.TSDB.Path(),
		Gaps: protoGaps,
	}, nil
}
