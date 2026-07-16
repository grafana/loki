package tsdb

import (
	"fmt"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
)

type fakeIndex struct {
	bounds
	Index
}

func (b fakeIndex) Bounds() (model.Time, model.Time) { return b.mint, b.maxt }

func newFakeIndex(mint, maxt model.Time) Index {
	return fakeIndex{bounds: bounds{mint: mint, maxt: maxt}}
}

func TestOverlap(t *testing.T) {
	for i, tc := range []struct {
		a       Index
		b       bounds
		overlap bool
	}{
		{
			a:       newFakeIndex(1, 5),
			b:       newBounds(2, 6),
			overlap: true,
		},
		{
			a:       newFakeIndex(1, 5),
			b:       newBounds(6, 7),
			overlap: false,
		},
		{
			// ensure [start,end) inclusivity works as expected
			a:       newFakeIndex(1, 5),
			b:       newBounds(5, 6),
			overlap: true,
		},
		{
			// ensure [start,end) inclusivity works as expected
			a:       newFakeIndex(5, 6),
			b:       newBounds(1, 5),
			overlap: false,
		},
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			require.Equal(t, tc.overlap, overlapIndex(tc.a, tc.b))
		})
	}
}
