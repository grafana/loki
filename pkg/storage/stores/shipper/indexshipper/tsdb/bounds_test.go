package tsdb

import (
	"fmt"
	"math"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
)

func TestOverlap(t *testing.T) {
	for i, tc := range []struct {
		a, b    Bounded
		overlap bool
	}{
		{
			a:       newBounds(1, 5),
			b:       newBounds(2, 6),
			overlap: true,
		},
		{
			a:       newBounds(1, 5),
			b:       newBounds(6, 7),
			overlap: false,
		},
		{
			// ensure [start,end) inclusivity works as expected
			a:       newBounds(1, 5),
			b:       newBounds(5, 6),
			overlap: true,
		},
		{
			// ensure [start,end) inclusivity works as expected
			a:       newBounds(5, 6),
			b:       newBounds(1, 5),
			overlap: false,
		},
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			require.Equal(t, tc.overlap, Overlap(tc.a, tc.b))
		})
	}
}

func TestInclusiveBounds(t *testing.T) {
	for i, tc := range []struct {
		input        Bounded
		lower, upper int64
	}{
		{
			input: newBounds(0, 4),
			lower: 0,
			upper: 5, // increment upper by 1
		},
		{
			input: newBounds(0, math.MaxInt64),
			lower: 0,
			upper: math.MaxInt64, // do nothing since we're already at the maximum value.
		},
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			a, b := inclusiveBounds(tc.input)
			require.Equal(t, model.Time(tc.lower), a)
			require.Equal(t, model.Time(tc.upper), b)
		})
	}
}
