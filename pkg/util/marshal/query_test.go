package marshal

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logqlmodel"
)

func TestNewStreams(t *testing.T) {
	s, err := NewStreams(logqlmodel.Streams{
		{
			Labels: "{asdf=\"�\"}",
		},
	})
	require.NoError(t, err)
	require.Equal(t, " ", s[0].Labels["asdf"], "expected only a space for label who only contained invalid UTF8 rune")
}
