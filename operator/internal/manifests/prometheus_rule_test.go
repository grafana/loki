package manifests_test

import (
	"testing"

	"github.com/grafana/loki/operator/api/v1beta1"
	"github.com/grafana/loki/operator/internal/manifests"
	"github.com/stretchr/testify/require"
)

func TestNewPrometheusRule(t *testing.T) {
	opts := manifests.Options{
		Stack: v1beta1.LokiStackSpec{
			Size: v1beta1.SizeOneXSmall,
		},
	}

	pr, err := manifests.NewPrometheusRule(opts)

	require.NoError(t, err)
	require.NotEmpty(t, pr)
}
