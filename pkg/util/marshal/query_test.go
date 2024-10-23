package marshal

import (
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestNewStreams(t *testing.T) {
	_, err := NewStreams(logqlmodel.Streams{
		{
			Labels: "{asdf=\"�\"}",
		},
	})
	require.NoError(t, err)
}
