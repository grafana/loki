package manifests

import (
	"testing"

	"github.com/stretchr/testify/require"
	policyv1 "k8s.io/api/policy/v1"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
)

func TestNewQueryFrontendDeployment_SelectorMatchesLabels(t *testing.T) {
	ss := NewQueryFrontendDeployment(Options{
		Name:      "abcd",
		Namespace: "efgh",
		Stack: lokiv1.LokiStackSpec{
			Template: &lokiv1.LokiTemplateSpec{
				QueryFrontend: &lokiv1.LokiComponentSpec{
					Replicas: 1,
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

func TestNewQueryFrontendDeployment_HasTemplateConfigHashAnnotation(t *testing.T) {
	ss := NewQueryFrontendDeployment(Options{
		Name:       "abcd",
		Namespace:  "efgh",
		ConfigSHA1: "deadbeef",
		Stack: lokiv1.LokiStackSpec{
			Template: &lokiv1.LokiTemplateSpec{
				QueryFrontend: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
			},
		},
	})
	expected := "loki.grafana.com/config-hash"
	annotations := ss.Spec.Template.Annotations
	require.Contains(t, annotations, expected)
	require.Equal(t, annotations[expected], "deadbeef")
}

func TestNewQueryFrontendDeployment_HasTemplateCertRotationRequiredAtAnnotation(t *testing.T) {
	ss := NewQueryFrontendDeployment(Options{
		Name:                   "abcd",
		Namespace:              "efgh",
		CertRotationRequiredAt: "deadbeef",
		Stack: lokiv1.LokiStackSpec{
			Template: &lokiv1.LokiTemplateSpec{
				QueryFrontend: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
			},
		},
	})

	expected := "loki.grafana.com/certRotationRequiredAt"
	annotations := ss.Spec.Template.Annotations
	require.Contains(t, annotations, expected)
	require.Equal(t, annotations[expected], "deadbeef")
}

func TestBuildQueryFrontend_PodDisruptionBudget(t *testing.T) {
	opts := Options{
		Name:      "abcd",
		Namespace: "efgh",
		Stack: lokiv1.LokiStackSpec{
			Template: &lokiv1.LokiTemplateSpec{
				QueryFrontend: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
			},
		},
	}
	objs, err := BuildQueryFrontend(opts)

	require.NoError(t, err)
	require.Len(t, objs, 4)

	pdb := objs[3].(*policyv1.PodDisruptionBudget)
	require.NotNil(t, pdb)
	require.Equal(t, "abcd-query-frontend", pdb.Name)
	require.Equal(t, "efgh", pdb.Namespace)
	require.NotNil(t, pdb.Spec.MinAvailable.IntVal)
	require.Equal(t, int32(1), pdb.Spec.MinAvailable.IntVal)
	require.EqualValues(t, ComponentLabels(LabelQueryFrontendComponent, opts.Name), pdb.Spec.Selector.MatchLabels)
}
