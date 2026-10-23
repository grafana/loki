package tsdb

import (
	"context"
	"math"
	"sync"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

type shardReaderBounds struct{ Index }

func (shardReaderBounds) Bounds() (model.Time, model.Time) { return 0, 100 }

func TestMultiIndexSelectsFingerprintFiles(t *testing.T) {
	files := []Index{
		&TSDBFile{Index: shardReaderBounds{}, shard: index.NewShard(0, 2), sharded: true},
		&TSDBFile{Index: shardReaderBounds{}, shard: index.NewShard(1, 2), sharded: true},
		&TSDBFile{Index: shardReaderBounds{}},
	}
	for _, tc := range []struct {
		name   string
		filter index.FingerprintFilter
		want   []Index
	}{
		{"without filter reads every file", nil, files},
		{"maximum fingerprint remains readable", boundedFilter{math.MaxUint64, math.MaxUint64}, []Index{files[1], files[2]}},
		{"first half includes historical unsplit file", index.NewShard(0, 2), []Index{files[0], files[2]}},
		{"last quarter includes historical unsplit file", index.NewShard(3, 4), []Index{files[1], files[2]}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var got []Index
			err := NewMultiIndex(IndexSlice(files)).forMatchingIndices(context.Background(), 1, 2, tc.filter, func(_ context.Context, idx Index) error {
				mu.Lock()
				defer mu.Unlock()
				got = append(got, idx)
				return nil
			})
			require.NoError(t, err)
			require.ElementsMatch(t, tc.want, got)
		})
	}
}
