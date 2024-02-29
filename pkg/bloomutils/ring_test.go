package bloomutils

import (
	"fmt"
	"math"
	"testing"

	"github.com/grafana/dskit/ring"
	"github.com/stretchr/testify/require"

	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
)

func uint64Range(min, max uint64) Range[uint64] {
	return Range[uint64]{min, max}
}

func TestKeyspacesFromTokenRanges(t *testing.T) {
	for i, tc := range []struct {
		tokenRanges ring.TokenRanges
		exp         []v1.FingerprintBounds
	}{
		{
			tokenRanges: ring.TokenRanges{
				0, math.MaxUint32 / 2,
				math.MaxUint32/2 + 1, math.MaxUint32,
			},
			exp: []v1.FingerprintBounds{
				v1.NewBounds(0, math.MaxUint64/2),
				v1.NewBounds(math.MaxUint64/2+1, math.MaxUint64),
			},
		},
		{
			tokenRanges: ring.TokenRanges{
				0, math.MaxUint8,
				math.MaxUint16, math.MaxUint16 << 1,
			},
			exp: []v1.FingerprintBounds{
				v1.NewBounds(0, 0xff00000000|math.MaxUint32),
				v1.NewBounds(math.MaxUint16<<32, math.MaxUint16<<33|math.MaxUint32),
			},
		},
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			require.Equal(t, tc.exp, KeyspacesFromTokenRanges(tc.tokenRanges))
		})
	}
}
