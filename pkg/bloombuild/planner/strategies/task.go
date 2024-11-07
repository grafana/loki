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
	*protos.Task
	// Override the protos.Task.Gaps field with gaps that use model.Fingerprint instead of v1.Series.
	Gaps []Gap
}

func NewTask(
	table config.DayTable,
	tenant string,
	bounds v1.FingerprintBounds,
	tsdb tsdb.SingleTenantTSDBIdentifier,
	gaps []Gap,
) *Task {
	return &Task{
		Task: protos.NewTask(table, tenant, bounds, tsdb, nil),
		Gaps: gaps,
	}
}

// ToProtoTask converts a Task to a ProtoTask.
// It will use the opened TSDB to get the chunks for the series in the gaps.
func (t *Task) ToProtoTask(ctx context.Context, forSeries common.ForSeries) (*protos.ProtoTask, error) {
	// Populate the gaps with the series and chunks.
	protoGaps := make([]protos.Gap, 0, len(t.Gaps))
	for _, gap := range t.Gaps {
		if !slices.IsSorted(gap.Series) {
			slices.Sort(gap.Series)
		}

		series := make([]*v1.Series, 0, len(gap.Series))
		if err := forSeries.ForSeries(
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

					chunks := make(v1.ChunkRefs, 0, len(chks))
					for _, chk := range chks {
						chunks = append(chunks, v1.ChunkRef{
							From:     model.Time(chk.MinTime),
							Through:  model.Time(chk.MaxTime),
							Checksum: chk.Checksum,
						})
					}

					series = append(series, &v1.Series{
						Fingerprint: fp,
						Chunks:      chunks,
					})
					return false
				}
			},
			labels.MustNewMatcher(labels.MatchEqual, "", ""),
		); err != nil {
			return nil, fmt.Errorf("failed to load series from TSDB for gap (%s): %w", gap.Bounds.String(), err)
		}

		protoGaps = append(protoGaps, protos.Gap{
			Bounds: gap.Bounds,
			Series: series,
			Blocks: gap.Blocks,
		})
	}

	// Copy inner task and set gaps
	task := *t.Task
	task.Gaps = protoGaps
	return task.ToProtoTask(), nil
}
