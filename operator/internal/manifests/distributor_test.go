package manifests

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/manifests/storage"
)

func TestNewDistributorDeployment_SelectorMatchesLabels(t *testing.T) {
	dpl := NewDistributorDeployment(Options{
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
	ss := NewDistributorDeployment(Options{
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

	annotations := ss.Spec.Template.Annotations
	require.Contains(t, annotations, AnnotationLokiConfigHash)
	require.Equal(t, annotations[AnnotationLokiConfigHash], "deadbeef")
}

func TestNewDistributorDeployment_HasTemplateObjectStoreHashAnnotation(t *testing.T) {
	ss := NewDistributorDeployment(Options{
		Name:      "abcd",
		Namespace: "efgh",
		ObjectStorage: storage.Options{
			SecretSHA1: "deadbeef",
		},
		Stack: lokiv1.LokiStackSpec{
			Template: &lokiv1.LokiTemplateSpec{
				Distributor: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
			},
		},
	})

	annotations := ss.Spec.Template.Annotations
	require.Contains(t, annotations, AnnotationLokiObjectStoreHash)
	require.Equal(t, annotations[AnnotationLokiObjectStoreHash], "deadbeef")
}

func TestNewDistributorDeployment_HasTemplateCertRotationRequiredAtAnnotation(t *testing.T) {
	ss := NewDistributorDeployment(Options{
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

	annotations := ss.Spec.Template.Annotations
	require.Contains(t, annotations, AnnotationCertRotationRequiredAt)
	require.Equal(t, annotations[AnnotationCertRotationRequiredAt], "deadbeef")
}

func TestBuildDistributor_PodDisruptionBudget(t *testing.T) {
	opts := Options{
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
	objs, err := BuildDistributor(opts)
	require.NoError(t, err)
	require.Len(t, objs, 4)

	pdb := objs[3].(*policyv1.PodDisruptionBudget)
	require.NotNil(t, pdb)
	require.Equal(t, "abcd-distributor", pdb.Name)
	require.Equal(t, "efgh", pdb.Namespace)
	require.NotNil(t, pdb.Spec.MinAvailable.IntVal)
	require.Equal(t, int32(1), pdb.Spec.MinAvailable.IntVal)
	require.EqualValues(t, ComponentLabels(LabelDistributorComponent, opts.Name), pdb.Spec.Selector.MatchLabels)
}

func TestNewDistributorDeployment_TopologySpreadConstraints(t *testing.T) {
	obj, _ := BuildDistributor(Options{
		Name:      "abcd",
		Namespace: "efgh",
		Stack: lokiv1.LokiStackSpec{
			Template: &lokiv1.LokiTemplateSpec{
				Distributor: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
			},
			Replication: &lokiv1.ReplicationSpec{
				Zones: []lokiv1.ZoneSpec{
					{
						TopologyKey: "zone",
						MaxSkew:     2,
					},
					{
						TopologyKey: "region",
						MaxSkew:     1,
					},
				},
				Factor: 1,
			},
		},
	})
	d := obj[0].(*appsv1.Deployment)
	require.Equal(t, []corev1.TopologySpreadConstraint{
		{
			MaxSkew:           2,
			TopologyKey:       "zone",
			WhenUnsatisfiable: "DoNotSchedule",
			LabelSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/component": "distributor",
					"app.kubernetes.io/instance":  "abcd",
				},
			},
		},
		{
			MaxSkew:           1,
			TopologyKey:       "region",
			WhenUnsatisfiable: "DoNotSchedule",
			LabelSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/component": "distributor",
					"app.kubernetes.io/instance":  "abcd",
				},
			},
		},
	}, d.Spec.Template.Spec.TopologySpreadConstraints)
}
