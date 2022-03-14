package tsdb

import (
	"fmt"
	"testing"

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
			overlap: false,
		},
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			require.Equal(t, tc.overlap, Overlap(tc.a, tc.b))
		})
	}
}
