package manifests_test

import (
	"testing"

	lokiv1beta1 "github.com/ViaQ/loki-operator/api/v1beta1"
	"github.com/ViaQ/loki-operator/internal/manifests"
	"github.com/stretchr/testify/require"
)

func TestNewDistributorDeployment_SelectorMatchesLabels(t *testing.T) {
	dpl := manifests.NewDistributorDeployment(manifests.Options{
		Name:      "abcd",
		Namespace: "efgh",
		Stack: lokiv1beta1.LokiStackSpec{
			Template: &lokiv1beta1.LokiTemplateSpec{
				Distributor: &lokiv1beta1.LokiComponentSpec{
					Replicas: 1,
				},
			},
		},
	})

	l := dpl.Spec.Template.GetObjectMeta().GetLabels()
	for key, value := range dpl.Spec.Selector.MatchLabels {
		require.Contains(t, l, key)
		require.Equal(t, l[key], value)
	}
}

func TestNewDistributorDeployme_HasTemplateConfigHashAnnotation(t *testing.T) {
	ss := manifests.NewDistributorDeployment(manifests.Options{
		Name:       "abcd",
		Namespace:  "efgh",
		ConfigSHA1: "deadbeef",
		Stack: lokiv1beta1.LokiStackSpec{
			Template: &lokiv1beta1.LokiTemplateSpec{
				Distributor: &lokiv1beta1.LokiComponentSpec{
					Replicas: 1,
				},
			},
		},
	})

	expected := "loki.openshift.io/config-hash"
	annotations := ss.Spec.Template.Annotations
	require.Contains(t, annotations, expected)
	require.Equal(t, annotations[expected], "deadbeef")
}
