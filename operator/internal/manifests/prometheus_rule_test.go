package manifests

import (
	"testing"

	"github.com/grafana/loki/operator/api/v1beta1"
	"github.com/stretchr/testify/require"
)

func TestNewPrometheusRule(t *testing.T) {
	opts := Options{
		Stack: v1beta1.LokiStackSpec{
			Size: v1beta1.SizeOneXSmall,
		},
	}

	pr, err := NewPrometheusRule(opts)

	require.NoError(t, err)
	require.NotEmpty(t, pr)
}
