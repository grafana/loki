package manifests_test

import (
	"testing"

	lokiv1beta1 "github.com/grafana/loki-operator/api/v1beta1"
	"github.com/grafana/loki-operator/internal/manifests"
	"github.com/stretchr/testify/require"
)

func TestNewRulerDeployment_SelectorMatchesLabels(t *testing.T) {
	ss := manifests.NewRulerDeployment(manifests.Options{
		Name:      "abcd",
		Namespace: "efgh",
		Stack: lokiv1beta1.LokiStackSpec{
			Template: &lokiv1beta1.LokiTemplateSpec{
				Ruler: &lokiv1beta1.LokiRulerComponentSpec{
					LokiComponentSpec: lokiv1beta1.LokiComponentSpec{
						Replicas: 1,
					},
				},
			},
		},
	})
	l := ss.Spec.Template.GetObjectMeta().GetLabels()
	for key, value := range ss.Spec.Selector.MatchLabels {
		require.Contains(t, l, key)
		require.Equal(t, l[key], value)
	}
}

func TestNewRulerDeployment_HasTemplateConfigHashAnnotation(t *testing.T) {
	ss := manifests.NewRulerDeployment(manifests.Options{
		Name:       "abcd",
		Namespace:  "efgh",
		ConfigSHA1: "deadbeef",
		Stack: lokiv1beta1.LokiStackSpec{
			Template: &lokiv1beta1.LokiTemplateSpec{
				Ruler: &lokiv1beta1.LokiRulerComponentSpec{
					LokiComponentSpec: lokiv1beta1.LokiComponentSpec{
						Replicas: 1,
					},
				},
			},
		},
	})
	expected := "loki.openshift.io/config-hash"
	annotations := ss.Spec.Template.Annotations
	require.Contains(t, annotations, expected)
	require.Equal(t, annotations[expected], "deadbeef")
}
