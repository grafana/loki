package manifests

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewPrometheusRule(t *testing.T) {
	opts := Options{
		Name: "test",
	}

	pr, err := NewPrometheusRule(opts)

	require.NoError(t, err)
	require.NotEmpty(t, pr)
	require.NotEmpty(t, pr.Spec)
}
