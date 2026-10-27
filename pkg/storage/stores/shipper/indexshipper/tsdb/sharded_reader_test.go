package tsdb

import (
	"context"
	"sync"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

type shardReaderBounds struct{ Index }

func (shardReaderBounds) Bounds() (model.Time, model.Time) { return 0, 100 }

func shardedFile(x, of uint32) Index {
	return &TSDBFile{Index: shardReaderBounds{}, shard: index.NewShard(x, of)}
}

func TestMultiIndexSelectsFingerprintFiles(t *testing.T) {
	mixed := []Index{shardedFile(0, 2), shardedFile(1, 2), &TSDBFile{Index: shardReaderBounds{}}}
	splitOnly := []Index{shardedFile(0, 2), shardedFile(1, 2)}
	quarters := []Index{shardedFile(0, 4), shardedFile(1, 4), shardedFile(2, 4), shardedFile(3, 4)}
	for _, tc := range []struct {
		name   string
		files  []Index
		filter index.FingerprintFilter
		want   []int
	}{
		{"without filter reads every file", mixed, nil, []int{0, 1, 2}},
		{"first half includes historical unsplit file", mixed, index.NewShard(0, 2), []int{0, 2}},
		{"last quarter includes historical unsplit file", mixed, index.NewShard(3, 4), []int{1, 2}},
		{"fully split table reads only the matching shard", splitOnly, index.NewShard(0, 2), []int{0}},
		{"coarse filter reads every overlapping finer shard", quarters, index.NewShard(1, 2), []int{2, 3}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var got []Index
			err := NewMultiIndex(IndexSlice(tc.files)).forMatchingIndices(context.Background(), 1, 2, tc.filter, func(_ context.Context, idx Index) error {
				mu.Lock()
				defer mu.Unlock()
				got = append(got, idx)
				return nil
			})
			require.NoError(t, err)
			want := make([]Index, 0, len(tc.want))
			for _, i := range tc.want {
				want = append(want, tc.files[i])
			}
			require.ElementsMatch(t, want, got)
		})
	}
}
