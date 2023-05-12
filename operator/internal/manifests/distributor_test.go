package manifests

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
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

	expected := "loki.grafana.com/config-hash"
	annotations := ss.Spec.Template.Annotations
	require.Contains(t, annotations, expected)
	require.Equal(t, annotations[expected], "deadbeef")
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

	expected := "loki.grafana.com/certRotationRequiredAt"
	annotations := ss.Spec.Template.Annotations
	require.Contains(t, annotations, expected)
	require.Equal(t, annotations[expected], "deadbeef")
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
	for _, tc := range []struct {
		Name                            string
		Replication                     *lokiv1.ReplicationSpec
		ExpectedTopologySpreadContraint []corev1.TopologySpreadConstraint
	}{
		{
			Name: "default",
			ExpectedTopologySpreadContraint: []corev1.TopologySpreadConstraint{
				{
					MaxSkew:     1,
					TopologyKey: "kubernetes.io/hostname",
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app.kubernetes.io/component": "distributor",
							"app.kubernetes.io/instance":  "abcd",
						},
					},
					WhenUnsatisfiable: corev1.ScheduleAnyway,
				},
			},
		},
		{
			Name: "replication_defined",
			Replication: &lokiv1.ReplicationSpec{
				Zones: []lokiv1.ZoneSpec{
					{
						TopologyKey: "zone",
						MaxSkew:     3,
					},
					{
						TopologyKey: "region",
						MaxSkew:     2,
					},
				},
				Factor: 1,
			},
			ExpectedTopologySpreadContraint: []corev1.TopologySpreadConstraint{
				{
					MaxSkew:     1,
					TopologyKey: "kubernetes.io/hostname",
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app.kubernetes.io/component": "distributor",
							"app.kubernetes.io/instance":  "abcd",
						},
					},
					WhenUnsatisfiable: corev1.ScheduleAnyway,
				},
				{
					MaxSkew:           3,
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
					MaxSkew:           2,
					TopologyKey:       "region",
					WhenUnsatisfiable: "DoNotSchedule",
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app.kubernetes.io/component": "distributor",
							"app.kubernetes.io/instance":  "abcd",
						},
					},
				},
			},
		},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			depl := NewDistributorDeployment(Options{
				Name:      "abcd",
				Namespace: "efgh",
				Stack: lokiv1.LokiStackSpec{
					Template: &lokiv1.LokiTemplateSpec{
						Distributor: &lokiv1.LokiComponentSpec{
							Replicas: 1,
						},
					},
					Replication: tc.Replication,
				},
			})

			require.Equal(t, tc.ExpectedTopologySpreadContraint, depl.Spec.Template.Spec.TopologySpreadConstraints)
		})
	}
}
