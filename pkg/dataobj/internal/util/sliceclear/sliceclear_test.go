package sliceclear_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/sliceclear"
)

func Test(t *testing.T) {
	s := make([]*int, 0, 10)
	for range 10 {
		s = append(s, new(int))
	}

	s = sliceclear.Clear(s)
	require.Equal(t, 10, cap(s))
	require.Equal(t, 0, len(s))

	// Reexpand s to its full capacity and ensure that all elements have been
	// zeroed out.
	full := s[:cap(s)]
	require.Equal(t, 10, len(full))
	for i := range 10 {
		require.Nil(t, full[i], "element %d was not zeroed; this can cause memory leaks", i)
	}
}
