package manifests_test

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"
	policyv1 "k8s.io/api/policy/v1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "github.com/grafana/loki/operator/apis/config/v1"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/manifests"
	"github.com/grafana/loki/operator/internal/manifests/internal"
)

func TestNewIngesterStatefulSet_HasTemplateConfigHashAnnotation(t *testing.T) {
	ss := manifests.NewIngesterStatefulSet(manifests.Options{
		Name:       "abcd",
		Namespace:  "efgh",
		ConfigSHA1: "deadbeef",
		Stack: lokiv1.LokiStackSpec{
			StorageClassName: "standard",
			Template: &lokiv1.LokiTemplateSpec{
				Ingester: &lokiv1.LokiComponentSpec{
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

func TestNewIngesterStatefulSet_HasTemplateCertRotationRequiredAtAnnotation(t *testing.T) {
	ss := manifests.NewIngesterStatefulSet(manifests.Options{
		Name:                   "abcd",
		Namespace:              "efgh",
		CertRotationRequiredAt: "deadbeef",
		Stack: lokiv1.LokiStackSpec{
			StorageClassName: "standard",
			Template: &lokiv1.LokiTemplateSpec{
				Ingester: &lokiv1.LokiComponentSpec{
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

func TestNewIngesterStatefulSet_SelectorMatchesLabels(t *testing.T) {
	// You must set the .spec.selector field of a StatefulSet to match the labels of
	// its .spec.template.metadata.labels. Prior to Kubernetes 1.8, the
	// .spec.selector field was defaulted when omitted. In 1.8 and later versions,
	// failing to specify a matching Pod Selector will result in a validation error
	// during StatefulSet creation.
	// See https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/#pod-selector
	sts := manifests.NewIngesterStatefulSet(manifests.Options{
		Name:      "abcd",
		Namespace: "efgh",
		Stack: lokiv1.LokiStackSpec{
			StorageClassName: "standard",
			Template: &lokiv1.LokiTemplateSpec{
				Ingester: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
			},
		},
	})

	l := sts.Spec.Template.GetObjectMeta().GetLabels()
	for key, value := range sts.Spec.Selector.MatchLabels {
		require.Contains(t, l, key)
		require.Equal(t, l[key], value)
	}
}

func TestBuildIngester_PodDisruptionBudget(t *testing.T) {
	for _, tc := range []struct {
		Name                 string
		PDBMinAvailable      int
		ExpectedMinAvailable int
	}{
		{
			Name:                 "Small stack",
			PDBMinAvailable:      1,
			ExpectedMinAvailable: 1,
		},
		{
			Name:                 "Medium stack",
			PDBMinAvailable:      2,
			ExpectedMinAvailable: 2,
		},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			opts := manifests.Options{
				Name:      "abcd",
				Namespace: "efgh",
				Gates:     v1.FeatureGates{},
				ResourceRequirements: internal.ComponentResources{
					Ingester: internal.ResourceRequirements{
						PDBMinAvailable: tc.PDBMinAvailable,
					},
				},
				Stack: lokiv1.LokiStackSpec{
					Template: &lokiv1.LokiTemplateSpec{
						Ingester: &lokiv1.LokiComponentSpec{
							Replicas: rand.Int31(),
						},
					},
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.OpenshiftLogging,
					},
				},
			}
			objs, err := manifests.BuildIngester(opts)
			require.NoError(t, err)
			require.Len(t, objs, 4)

			pdb := objs[3].(*policyv1.PodDisruptionBudget)
			require.NotNil(t, pdb)
			require.Equal(t, "abcd-ingester", pdb.Name)
			require.Equal(t, "efgh", pdb.Namespace)
			require.NotNil(t, pdb.Spec.MinAvailable.IntVal)
			require.Equal(t, int32(tc.ExpectedMinAvailable), pdb.Spec.MinAvailable.IntVal)
			require.EqualValues(t, manifests.ComponentLabels(manifests.LabelIngesterComponent, opts.Name), pdb.Spec.Selector.MatchLabels)
		})
	}
}

func TestIngesterPodAntiAffinity(t *testing.T) {
	sts := manifests.NewIngesterStatefulSet(manifests.Options{
		Name:      "abcd",
		Namespace: "efgh",
		Stack: lokiv1.LokiStackSpec{
			StorageClassName: "standard",
			Template: &lokiv1.LokiTemplateSpec{
				Ingester: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
			},
		},
	})
	expectedPodAntiAffinity := &corev1.PodAntiAffinity{
		PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
			{
				Weight: 100,
				PodAffinityTerm: corev1.PodAffinityTerm{
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app.kubernetes.io/component": manifests.LabelIngesterComponent,
							"app.kubernetes.io/instance":  "abcd",
						},
					},
					TopologyKey: "kubernetes.io/hostname",
				},
			},
		},
	}
	require.NotNil(t, sts.Spec.Template.Spec.Affinity)
	require.Equal(t, expectedPodAntiAffinity, sts.Spec.Template.Spec.Affinity.PodAntiAffinity)
}
