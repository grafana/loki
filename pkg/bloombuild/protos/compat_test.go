package protos

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/bloombuild/planner/plannertest"
	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
)

func TestTaskToProtoTask(t *testing.T) {
	// Hack to set exportTSInSecs to true
	idx := plannertest.TsdbID(1234)
	idx, _ = tsdb.ParseSingleTenantTSDBPath(idx.Name())

	in := NewTask(plannertest.TestTable, "fake", v1.NewBounds(0, 100), idx, []Gap{
		{
			Bounds: v1.NewBounds(0, 25),
			Series: plannertest.GenSeriesWithStep(v1.NewBounds(0, 10), 2),
			Blocks: []bloomshipper.BlockRef{
				plannertest.GenBlockRef(0, 2),
				plannertest.GenBlockRef(4, 10),
			},
		},
		{
			Bounds: v1.NewBounds(30, 50),
			Series: plannertest.GenSeriesWithStep(v1.NewBounds(30, 40), 2),
			Blocks: []bloomshipper.BlockRef{
				plannertest.GenBlockRef(30, 50),
			},
		},
		{
			Bounds: v1.NewBounds(60, 100),
			Series: plannertest.GenSeriesWithStep(v1.NewBounds(60, 70), 5),
			Blocks: []bloomshipper.BlockRef{
				plannertest.GenBlockRef(60, 70),
				plannertest.GenBlockRef(71, 90),
				plannertest.GenBlockRef(91, 100),
			},
		},
	})

	out, err := FromProtoTask(in.ToProtoTask())
	require.NoError(t, err)

	require.Equal(t, in, out)
}
