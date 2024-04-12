package index

import (
	"fmt"
	"math"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
)

func TestShardMatch(t *testing.T) {
	for _, tc := range []struct {
		shard ShardAnnotation
		fp    uint64
		exp   bool
	}{
		{
			shard: NewShard(0, 2),
			fp:    0,
			exp:   true,
		},
		{
			shard: NewShard(0, 2),
			fp:    5287603155525329,
			exp:   true,
		},
		{
			shard: NewShard(0, 2),
			fp:    1 << 63,
			exp:   false,
		},
		{
			shard: NewShard(1, 2),
			fp:    0,
			exp:   false,
		},
		{
			shard: NewShard(1, 2),
			fp:    1 << 63,
			exp:   true,
		},
		{
			shard: NewShard(2, 4),
			fp:    0,
			exp:   false,
		},
		{
			shard: NewShard(2, 4),
			fp:    1 << 63,
			exp:   true,
		},
		{
			shard: NewShard(2, 4),
			fp:    3 << 62,
			exp:   false,
		},
		{
			shard: NewShard(0, 1),
			fp:    5287603155525329,
			exp:   true,
		},
	} {
		t.Run(fmt.Sprint(tc.shard, tc.fp), func(t *testing.T) {
			require.Equal(t, tc.exp, tc.shard.Match(model.Fingerprint(tc.fp)))
		})
	}
}

func TestShardBounds(t *testing.T) {
	for _, tc := range []struct {
		shard         ShardAnnotation
		from, through uint64
	}{
		{
			shard:   NewShard(0, 1),
			from:    0,
			through: math.MaxUint64,
		},
		{
			shard:   NewShard(0, 2),
			from:    0,
			through: 1 << 63,
		},
		{
			shard:   NewShard(1, 2),
			from:    1 << 63,
			through: math.MaxUint64,
		},
		{
			shard:   NewShard(1, 4),
			from:    1 << 62,
			through: 2 << 62,
		},
		{
			shard:   NewShard(2, 4),
			from:    2 << 62,
			through: 3 << 62,
		},
		{
			shard:   NewShard(3, 4),
			from:    3 << 62,
			through: math.MaxUint64,
		},
	} {
		t.Run(tc.shard.String(), func(t *testing.T) {
			from, through := tc.shard.GetFromThrough()
			require.Equal(t, model.Fingerprint(tc.from), from)
			require.Equal(t, model.Fingerprint(tc.through), through)
		})
	}
}

func TestShardValidate(t *testing.T) {
	for _, tc := range []struct {
		desc   string
		factor uint32
		err    bool
	}{
		{
			factor: 0,
			err:    false,
		},
		{
			factor: 1,
			err:    true,
		},
		{
			factor: 2,
			err:    false,
		},
	} {
		t.Run(fmt.Sprint(tc.factor), func(t *testing.T) {
			err := NewShard(0, tc.factor).Validate()
			if tc.err {
				require.NotNil(t, err)
			} else {
				require.Nil(t, err)
			}
		})
	}
}
