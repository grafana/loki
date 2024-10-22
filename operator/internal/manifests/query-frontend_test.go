package manifests

import (
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/manifests/storage"
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

	annotations := ss.Spec.Template.Annotations
	require.Contains(t, annotations, AnnotationLokiConfigHash)
	require.Equal(t, annotations[AnnotationLokiConfigHash], "deadbeef")
}

func TestNewQueryFrontendDeployment_HasTemplateObjectStoreHashAnnotation(t *testing.T) {
	ss := NewQueryFrontendDeployment(Options{
		Name:      "abcd",
		Namespace: "efgh",
		ObjectStorage: storage.Options{
			SecretSHA1: "deadbeef",
		},
		Stack: lokiv1.LokiStackSpec{
			Template: &lokiv1.LokiTemplateSpec{
				QueryFrontend: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
			},
		},
	})

	annotations := ss.Spec.Template.Annotations
	require.Contains(t, annotations, AnnotationLokiObjectStoreHash)
	require.Equal(t, annotations[AnnotationLokiObjectStoreHash], "deadbeef")
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

	annotations := ss.Spec.Template.Annotations
	require.Contains(t, annotations, AnnotationCertRotationRequiredAt)
	require.Equal(t, annotations[AnnotationCertRotationRequiredAt], "deadbeef")
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

func TestNewQueryFrontendDeployment_TopologySpreadConstraints(t *testing.T) {
	obj, _ := BuildQueryFrontend(Options{
		Name:      "abcd",
		Namespace: "efgh",
		Stack: lokiv1.LokiStackSpec{
			Template: &lokiv1.LokiTemplateSpec{
				QueryFrontend: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
			},
			Replication: &lokiv1.ReplicationSpec{
				Zones: []lokiv1.ZoneSpec{
					{
						TopologyKey: "zone",
						MaxSkew:     1,
					},
					{
						TopologyKey: "region",
						MaxSkew:     2,
					},
				},
				Factor: 1,
			},
		},
	})

	depl := obj[0].(*appsv1.Deployment)
	require.Equal(t, []corev1.TopologySpreadConstraint{
		{
			MaxSkew:           1,
			TopologyKey:       "zone",
			WhenUnsatisfiable: "DoNotSchedule",
			LabelSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/component": "query-frontend",
					"app.kubernetes.io/instance":  "abcd",
				},
			},
		},
		{
			MaxSkew:           2,
			TopologyKey:       "region",
			WhenUnsatisfiable: "DoNotSchedule",
			LabelSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/component": "query-frontend",
					"app.kubernetes.io/instance":  "abcd",
				},
			},
		},
	}, depl.Spec.Template.Spec.TopologySpreadConstraints)
}
