package manifests

import (
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/manifests/storage"
)

func TestNewQuerierDeployment_HasTemplateConfigHashAnnotation(t *testing.T) {
	ss := NewQuerierDeployment(Options{
		Name:       "abcd",
		Namespace:  "efgh",
		ConfigSHA1: "deadbeef",
		Stack: lokiv1.LokiStackSpec{
			StorageClassName: "standard",
			Template: &lokiv1.LokiTemplateSpec{
				Querier: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
			},
		},
	})

	annotations := ss.Spec.Template.Annotations
	require.Contains(t, annotations, AnnotationLokiConfigHash)
	require.Equal(t, annotations[AnnotationLokiConfigHash], "deadbeef")
}

func TestNewQuerierDeployment_HasTemplateObjectStoreHashAnnotation(t *testing.T) {
	ss := NewQuerierDeployment(Options{
		Name:      "abcd",
		Namespace: "efgh",
		ObjectStorage: storage.Options{
			SecretSHA1: "deadbeef",
		},
		Stack: lokiv1.LokiStackSpec{
			StorageClassName: "standard",
			Template: &lokiv1.LokiTemplateSpec{
				Querier: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
			},
		},
	})

	annotations := ss.Spec.Template.Annotations
	require.Contains(t, annotations, AnnotationLokiObjectStoreHash)
	require.Equal(t, annotations[AnnotationLokiObjectStoreHash], "deadbeef")
}

func TestNewQuerierDeployment_HasTemplateCertRotationRequiredAtAnnotation(t *testing.T) {
	ss := NewQuerierDeployment(Options{
		Name:                   "abcd",
		Namespace:              "efgh",
		CertRotationRequiredAt: "deadbeef",
		Stack: lokiv1.LokiStackSpec{
			Template: &lokiv1.LokiTemplateSpec{
				Querier: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
			},
		},
	})

	annotations := ss.Spec.Template.Annotations
	require.Contains(t, annotations, AnnotationCertRotationRequiredAt)
	require.Equal(t, annotations[AnnotationCertRotationRequiredAt], "deadbeef")
}

func TestNewQuerierDeployment_SelectorMatchesLabels(t *testing.T) {
	// You must set the .spec.selector field of a Deployment to match the labels of
	// its .spec.template.metadata.labels. Prior to Kubernetes 1.8, the
	// .spec.selector field was defaulted when omitted. In 1.8 and later versions,
	// failing to specify a matching Pod Selector will result in a validation error
	// during Deployment creation.
	// See https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/#pod-selector
	ss := NewQuerierDeployment(Options{
		Name:      "abcd",
		Namespace: "efgh",
		Stack: lokiv1.LokiStackSpec{
			StorageClassName: "standard",
			Template: &lokiv1.LokiTemplateSpec{
				Querier: &lokiv1.LokiComponentSpec{
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

func TestBuildQuerier_PodDisruptionBudget(t *testing.T) {
	tt := []struct {
		name string
		opts Options
		want policyv1.PodDisruptionBudget
	}{
		{
			name: "Querier with 1 replica",
			opts: Options{
				Name:      "abcd",
				Namespace: "efgh",
				Stack: lokiv1.LokiStackSpec{
					Template: &lokiv1.LokiTemplateSpec{
						Querier: &lokiv1.LokiComponentSpec{
							Replicas: 1,
						},
					},
				},
			},
			want: policyv1.PodDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "abcd-querier",
					Namespace: "efgh",
					Labels:    ComponentLabels(LabelQuerierComponent, "abcd"),
				},
				Spec: policyv1.PodDisruptionBudgetSpec{
					MinAvailable: &intstr.IntOrString{Type: intstr.Int, IntVal: 1},
					Selector: &metav1.LabelSelector{
						MatchLabels: ComponentLabels(LabelQuerierComponent, "abcd"),
					},
				},
			},
		},
		{
			name: "Querier with 2 replicas",
			opts: Options{
				Name:      "abcd",
				Namespace: "efgh",
				Stack: lokiv1.LokiStackSpec{
					Template: &lokiv1.LokiTemplateSpec{
						Querier: &lokiv1.LokiComponentSpec{
							Replicas: 2,
						},
					},
				},
			},
			want: policyv1.PodDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "abcd-querier",
					Namespace: "efgh",
					Labels:    ComponentLabels(LabelQuerierComponent, "abcd"),
				},
				Spec: policyv1.PodDisruptionBudgetSpec{
					MinAvailable: &intstr.IntOrString{Type: intstr.Int, IntVal: 1},
					Selector: &metav1.LabelSelector{
						MatchLabels: ComponentLabels(LabelQuerierComponent, "abcd"),
					},
				},
			},
		},
		{
			name: "Querier with 3 replicas",
			opts: Options{
				Name:      "abcd",
				Namespace: "efgh",
				Stack: lokiv1.LokiStackSpec{
					Template: &lokiv1.LokiTemplateSpec{
						Querier: &lokiv1.LokiComponentSpec{
							Replicas: 3,
						},
					},
				},
			},
			want: policyv1.PodDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "abcd-querier",
					Namespace: "efgh",
					Labels:    ComponentLabels(LabelQuerierComponent, "abcd"),
				},
				Spec: policyv1.PodDisruptionBudgetSpec{
					MinAvailable: &intstr.IntOrString{Type: intstr.Int, IntVal: 2},
					Selector: &metav1.LabelSelector{
						MatchLabels: ComponentLabels(LabelQuerierComponent, "abcd"),
					},
				},
			},
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			objs, err := BuildQuerier(tc.opts)
			require.NoError(t, err)
			require.Len(t, objs, 4)

			pdb := objs[3].(*policyv1.PodDisruptionBudget)
			require.NotNil(t, pdb)
			require.Equal(t, tc.want.ObjectMeta, pdb.ObjectMeta)
			require.Equal(t, tc.want.Spec, pdb.Spec)
		})
	}
}

func TestNewQuerierDeployment_TopologySpreadConstraints(t *testing.T) {
	obj, _ := BuildQuerier(Options{
		Name:      "abcd",
		Namespace: "efgh",
		Stack: lokiv1.LokiStackSpec{
			Template: &lokiv1.LokiTemplateSpec{
				Querier: &lokiv1.LokiComponentSpec{
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
					"app.kubernetes.io/component": "querier",
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
					"app.kubernetes.io/component": "querier",
					"app.kubernetes.io/instance":  "abcd",
				},
			},
		},
	}, d.Spec.Template.Spec.TopologySpreadConstraints)
}
