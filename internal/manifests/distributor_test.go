package manifests_test

import (
	"testing"

	"github.com/ViaQ/loki-operator/internal/manifests"
	"github.com/stretchr/testify/require"
)

func TestNewDistributorDeployment_SelectorMatchesLabels(t *testing.T) {
	ss := manifests.NewDistributorDeployment(manifests.Options{
		Name:      "abcd",
		Namespace: "efgh",
	})

	l := ss.Spec.Template.GetObjectMeta().GetLabels()
	for key, value := range ss.Spec.Selector.MatchLabels {
		require.Contains(t, l, key)
		require.Equal(t, l[key], value)
	}
}

func TestNewDistributorDeployme_HasTemplateConfigHashAnnotation(t *testing.T) {
	ss := manifests.NewDistributorDeployment(manifests.Options{
		Name:       "abcd",
		Namespace:  "efgh",
		ConfigSHA1: "deadbeef",
	})

	expected := "loki.openshift.io/config-hash"
	annotations := ss.Spec.Template.Annotations
	require.Contains(t, annotations, expected)
	require.Equal(t, annotations[expected], "deadbeef")
}
