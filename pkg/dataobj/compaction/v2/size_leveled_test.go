package compactionv2

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
)

const gib uint64 = 1 << 30

type sizedRun uint64

func (r sizedRun) Sections() []*compactionv2pb.SectionRef { return nil }
func (r sizedRun) Size() uint64                           { return uint64(r) }

func TestNewSizeLeveledStrategy(t *testing.T) {
	t.Run("returns an error when base is zero", func(t *testing.T) {
		_, err := NewSizeLeveledStrategy(0, 8)
		require.Error(t, err)
	})

	t.Run("returns an error when ratio is below 2", func(t *testing.T) {
		_, err := NewSizeLeveledStrategy(16*gib, 1)
		require.Error(t, err)
	})

	t.Run("accepts the default base and ratio", func(t *testing.T) {
		_, err := NewSizeLeveledStrategy(DefaultSizeLevelBase, DefaultSizeLevelRatio)
		require.NoError(t, err)
	})
}

func TestSizeLeveledStrategyLevel(t *testing.T) {
	s, err := NewSizeLeveledStrategy(16*gib, 8)
	require.NoError(t, err)

	tests := []struct {
		name string
		size uint64
		want int
	}{
		{name: "an empty run is in level 0", size: 0, want: 0},
		{name: "a run just below base is in level 0", size: 16*gib - 1, want: 0},
		{name: "a run equal to base is in level 1", size: 16 * gib, want: 1},
		{name: "a run just below base times ratio is in level 1", size: 128*gib - 1, want: 1},
		{name: "a run equal to base times ratio is in level 2", size: 128 * gib, want: 2},
		{name: "a run of 1PiB is in level 6", size: 1 << 50, want: 6},
		{name: "the largest possible run does not overflow and is in the top level", size: math.MaxUint64, want: 10},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, s.Level(test.size))
		})
	}
}

func TestSizeLeveledStrategyGroupByLevels(t *testing.T) {
	s, err := NewSizeLeveledStrategy(DefaultSizeLevelBase, DefaultSizeLevelRatio) // 16GB, 8x ratio
	require.NoError(t, err)

	t.Run("returns no levels when there are no runs", func(t *testing.T) {
		require.Empty(t, s.GroupByLevels(nil))
	})

	t.Run("returns levels up to the highest level that holds a run", func(t *testing.T) {
		levels := s.GroupByLevels([]Run{sizedRun(200 * gib)})
		require.Len(t, levels, 3)
		require.Nil(t, levels[0])
		require.Nil(t, levels[1])
		require.Equal(t, []Run{sizedRun(200 * gib)}, levels[2])
	})

	t.Run("places the output of merging by the DefaultSizeLevelRatio runs one level up", func(t *testing.T) {
		const fresh = 6 * gib
		levels := s.GroupByLevels([]Run{sizedRun(fresh), sizedRun(8 * fresh), sizedRun(64 * fresh)})
		require.Equal(t, []Run{sizedRun(fresh)}, levels[0])
		require.Equal(t, []Run{sizedRun(8 * fresh)}, levels[1])
		require.Equal(t, []Run{sizedRun(64 * fresh)}, levels[2])
	})

	t.Run("keeps input order within a level", func(t *testing.T) {
		levels := s.GroupByLevels([]Run{sizedRun(3 * gib), sizedRun(200 * gib), sizedRun(1 * gib)})
		require.Equal(t, []Run{sizedRun(3 * gib), sizedRun(1 * gib)}, levels[0])
		require.Equal(t, []Run{sizedRun(200 * gib)}, levels[2])
	})
}
