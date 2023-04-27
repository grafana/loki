package manifests_test

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"
	policyv1 "k8s.io/api/policy/v1"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/manifests"
)

func TestNewDistributorDeployment_SelectorMatchesLabels(t *testing.T) {
	dpl := manifests.NewDistributorDeployment(manifests.Options{
		Name:      "abcd",
		Namespace: "efgh",
		Stack: lokiv1.LokiStackSpec{
			Template: &lokiv1.LokiTemplateSpec{
				Distributor: &lokiv1.LokiComponentSpec{
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

func TestNewDistributorDeployment_HasTemplateConfigHashAnnotation(t *testing.T) {
	ss := manifests.NewDistributorDeployment(manifests.Options{
		Name:       "abcd",
		Namespace:  "efgh",
		ConfigSHA1: "deadbeef",
		Stack: lokiv1.LokiStackSpec{
			Template: &lokiv1.LokiTemplateSpec{
				Distributor: &lokiv1.LokiComponentSpec{
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

func TestNewDistributorDeployment_HasTemplateCertRotationRequiredAtAnnotation(t *testing.T) {
	ss := manifests.NewDistributorDeployment(manifests.Options{
		Name:                   "abcd",
		Namespace:              "efgh",
		CertRotationRequiredAt: "deadbeef",
		Stack: lokiv1.LokiStackSpec{
			Template: &lokiv1.LokiTemplateSpec{
				Distributor: &lokiv1.LokiComponentSpec{
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

func TestBuildDistributor_PodDisruptionBudget(t *testing.T) {
	opts := manifests.Options{
		Name:      "abcd",
		Namespace: "efgh",
		Stack: lokiv1.LokiStackSpec{
			Template: &lokiv1.LokiTemplateSpec{
				Distributor: &lokiv1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
			},
			Tenants: &lokiv1.TenantsSpec{
				Mode: lokiv1.OpenshiftLogging,
			},
		},
	}
	objs, err := manifests.BuildDistributor(opts)
	require.NoError(t, err)
	require.Len(t, objs, 4)

	pdb := objs[3].(*policyv1.PodDisruptionBudget)
	require.NotNil(t, pdb)
	require.Equal(t, "abcd-distributor", pdb.Name)
	require.Equal(t, "efgh", pdb.Namespace)
	require.NotNil(t, pdb.Spec.MinAvailable.IntVal)
	require.Equal(t, int32(1), pdb.Spec.MinAvailable.IntVal)
	require.EqualValues(t, manifests.ComponentLabels(manifests.LabelDistributorComponent, opts.Name), pdb.Spec.Selector.MatchLabels)
}
