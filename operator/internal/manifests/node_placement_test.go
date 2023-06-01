package manifests

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configv1 "github.com/grafana/loki/operator/apis/config/v1"
	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/manifests/storage"
)

func TestTolerationsAreSetForEachComponent(t *testing.T) {
	tolerations := []corev1.Toleration{{
		Key:      "type",
		Operator: corev1.TolerationOpEqual,
		Value:    "storage",
		Effect:   corev1.TaintEffectNoSchedule,
	}}
	optsWithTolerations := Options{
		Stack: lokiv1.LokiStackSpec{
			Template: &lokiv1.LokiTemplateSpec{
				Compactor: &lokiv1.LokiComponentSpec{
					Tolerations: tolerations,
					Replicas:    1,
				},
				Distributor: &lokiv1.LokiComponentSpec{
					Tolerations: tolerations,
					Replicas:    1,
				},
				Ingester: &lokiv1.LokiComponentSpec{
					Tolerations: tolerations,
					Replicas:    1,
				},
				Querier: &lokiv1.LokiComponentSpec{
					Tolerations: tolerations,
					Replicas:    1,
				},
				QueryFrontend: &lokiv1.LokiComponentSpec{
					Tolerations: tolerations,
					Replicas:    1,
				},
				IndexGateway: &lokiv1.LokiComponentSpec{
					Tolerations: tolerations,
					Replicas:    1,
				},
				Ruler: &lokiv1.LokiComponentSpec{
					Tolerations: tolerations,
					Replicas:    1,
				},
			},
		},
		ObjectStorage: storage.Options{},
	}

	optsWithoutTolerations := Options{
		Stack: lokiv1.LokiStackSpec{
			Template: &lokiv1.LokiTemplateSpec{
				Compactor: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				Distributor: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				Ingester: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				Querier: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				QueryFrontend: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				IndexGateway: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				Ruler: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
			},
		},
		ObjectStorage: storage.Options{},
	}

	t.Run("distributor", func(t *testing.T) {
		assert.Equal(t, tolerations, NewDistributorDeployment(optsWithTolerations).Spec.Template.Spec.Tolerations)
		assert.Empty(t, NewDistributorDeployment(optsWithoutTolerations).Spec.Template.Spec.Tolerations)
	})

	t.Run("query_frontend", func(t *testing.T) {
		assert.Equal(t, tolerations, NewQueryFrontendDeployment(optsWithTolerations).Spec.Template.Spec.Tolerations)
		assert.Empty(t, NewQueryFrontendDeployment(optsWithoutTolerations).Spec.Template.Spec.Tolerations)
	})

	t.Run("querier", func(t *testing.T) {
		assert.Equal(t, tolerations, NewQuerierDeployment(optsWithTolerations).Spec.Template.Spec.Tolerations)
		assert.Empty(t, NewQuerierDeployment(optsWithoutTolerations).Spec.Template.Spec.Tolerations)
	})

	t.Run("ingester", func(t *testing.T) {
		assert.Equal(t, tolerations, NewIngesterStatefulSet(optsWithTolerations).Spec.Template.Spec.Tolerations)
		assert.Empty(t, NewIngesterStatefulSet(optsWithoutTolerations).Spec.Template.Spec.Tolerations)
	})

	t.Run("compactor", func(t *testing.T) {
		assert.Equal(t, tolerations, NewCompactorStatefulSet(optsWithTolerations).Spec.Template.Spec.Tolerations)
		assert.Empty(t, NewCompactorStatefulSet(optsWithoutTolerations).Spec.Template.Spec.Tolerations)
	})

	t.Run("index_gateway", func(t *testing.T) {
		assert.Equal(t, tolerations, NewIndexGatewayStatefulSet(optsWithTolerations).Spec.Template.Spec.Tolerations)
		assert.Empty(t, NewIndexGatewayStatefulSet(optsWithoutTolerations).Spec.Template.Spec.Tolerations)
	})

	t.Run("ruler", func(t *testing.T) {
		assert.Equal(t, tolerations, NewRulerStatefulSet(optsWithTolerations).Spec.Template.Spec.Tolerations)
		assert.Empty(t, NewRulerStatefulSet(optsWithoutTolerations).Spec.Template.Spec.Tolerations)
	})
}

func TestNodeSelectorsAreSetForEachComponent(t *testing.T) {
	nodeSelectors := map[string]string{"type": "storage"}
	optsWithNodeSelectors := Options{
		Stack: lokiv1.LokiStackSpec{
			Template: &lokiv1.LokiTemplateSpec{
				Compactor: &lokiv1.LokiComponentSpec{
					NodeSelector: nodeSelectors,
					Replicas:     1,
				},
				Distributor: &lokiv1.LokiComponentSpec{
					NodeSelector: nodeSelectors,
					Replicas:     1,
				},
				Ingester: &lokiv1.LokiComponentSpec{
					NodeSelector: nodeSelectors,
					Replicas:     1,
				},
				Querier: &lokiv1.LokiComponentSpec{
					NodeSelector: nodeSelectors,
					Replicas:     1,
				},
				QueryFrontend: &lokiv1.LokiComponentSpec{
					NodeSelector: nodeSelectors,
					Replicas:     1,
				},
				IndexGateway: &lokiv1.LokiComponentSpec{
					NodeSelector: nodeSelectors,
					Replicas:     1,
				},
				Ruler: &lokiv1.LokiComponentSpec{
					NodeSelector: nodeSelectors,
					Replicas:     1,
				},
			},
		},
		ObjectStorage: storage.Options{},
	}

	optsWithoutNodeSelectors := Options{
		Stack: lokiv1.LokiStackSpec{
			Template: &lokiv1.LokiTemplateSpec{
				Compactor: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				Distributor: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				Ingester: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				Querier: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				QueryFrontend: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				IndexGateway: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				Ruler: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
			},
		},
		ObjectStorage: storage.Options{},
	}

	t.Run("distributor", func(t *testing.T) {
		assert.Equal(t, nodeSelectors, NewDistributorDeployment(optsWithNodeSelectors).Spec.Template.Spec.NodeSelector)
		assert.Empty(t, NewDistributorDeployment(optsWithoutNodeSelectors).Spec.Template.Spec.NodeSelector)
	})

	t.Run("query_frontend", func(t *testing.T) {
		assert.Equal(t, nodeSelectors, NewQueryFrontendDeployment(optsWithNodeSelectors).Spec.Template.Spec.NodeSelector)
		assert.Empty(t, NewQueryFrontendDeployment(optsWithoutNodeSelectors).Spec.Template.Spec.NodeSelector)
	})

	t.Run("querier", func(t *testing.T) {
		assert.Equal(t, nodeSelectors, NewQuerierDeployment(optsWithNodeSelectors).Spec.Template.Spec.NodeSelector)
		assert.Empty(t, NewQuerierDeployment(optsWithoutNodeSelectors).Spec.Template.Spec.NodeSelector)
	})

	t.Run("ingester", func(t *testing.T) {
		assert.Equal(t, nodeSelectors, NewIngesterStatefulSet(optsWithNodeSelectors).Spec.Template.Spec.NodeSelector)
		assert.Empty(t, NewIngesterStatefulSet(optsWithoutNodeSelectors).Spec.Template.Spec.NodeSelector)
	})

	t.Run("compactor", func(t *testing.T) {
		assert.Equal(t, nodeSelectors, NewCompactorStatefulSet(optsWithNodeSelectors).Spec.Template.Spec.NodeSelector)
		assert.Empty(t, NewCompactorStatefulSet(optsWithoutNodeSelectors).Spec.Template.Spec.NodeSelector)
	})

	t.Run("index_gateway", func(t *testing.T) {
		assert.Equal(t, nodeSelectors, NewIndexGatewayStatefulSet(optsWithNodeSelectors).Spec.Template.Spec.NodeSelector)
		assert.Empty(t, NewIndexGatewayStatefulSet(optsWithoutNodeSelectors).Spec.Template.Spec.NodeSelector)
	})

	t.Run("ruler", func(t *testing.T) {
		assert.Equal(t, nodeSelectors, NewRulerStatefulSet(optsWithNodeSelectors).Spec.Template.Spec.NodeSelector)
		assert.Empty(t, NewRulerStatefulSet(optsWithoutNodeSelectors).Spec.Template.Spec.NodeSelector)
	})
}

func TestDefaultNodeAffinityForEachComponent(t *testing.T) {
	nodeAffinity := &corev1.NodeAffinity{
		RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
			NodeSelectorTerms: []corev1.NodeSelectorTerm{
				{
					MatchExpressions: []corev1.NodeSelectorRequirement{
						{
							Key:      "kubernetes.io/os",
							Operator: "In",
							Values: []string{
								"linux",
							},
						},
					},
				},
			},
		},
	}
	optsWithNoDefaultAffinity := Options{
		Stack: lokiv1.LokiStackSpec{
			Template: &lokiv1.LokiTemplateSpec{
				Compactor: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				Distributor: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				Ingester: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				Querier: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				QueryFrontend: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				IndexGateway: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				Ruler: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
			},
		},
	}
	optsWithDefaultAffinity := Options{
		Stack: lokiv1.LokiStackSpec{
			Template: &lokiv1.LokiTemplateSpec{
				Compactor: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				Distributor: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				Ingester: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				Querier: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				QueryFrontend: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				IndexGateway: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				Ruler: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
			},
		},
		Gates: configv1.FeatureGates{
			DefaultNodeAffinity: true,
		},
	}

	t.Run("distributor", func(t *testing.T) {
		assert.Equal(t, nodeAffinity, NewDistributorDeployment(optsWithDefaultAffinity).Spec.Template.Spec.Affinity.NodeAffinity)
		affinity := NewDistributorDeployment(optsWithNoDefaultAffinity).Spec.Template.Spec.Affinity
		if affinity != nil {
			assert.Empty(t, affinity.NodeAffinity)
		}
	})

	t.Run("query_frontend", func(t *testing.T) {
		assert.Equal(t, nodeAffinity, NewQueryFrontendDeployment(optsWithDefaultAffinity).Spec.Template.Spec.Affinity.NodeAffinity)
		affinity := NewQueryFrontendDeployment(optsWithNoDefaultAffinity).Spec.Template.Spec.Affinity
		if affinity != nil {
			assert.Empty(t, affinity.NodeAffinity)
		}
	})

	t.Run("querier", func(t *testing.T) {
		assert.Equal(t, nodeAffinity, NewQuerierDeployment(optsWithDefaultAffinity).Spec.Template.Spec.Affinity.NodeAffinity)
		affinity := NewQuerierDeployment(optsWithNoDefaultAffinity).Spec.Template.Spec.Affinity
		if affinity != nil {
			assert.Empty(t, affinity.NodeAffinity)
		}
	})

	t.Run("ingester", func(t *testing.T) {
		assert.Equal(t, nodeAffinity, NewIngesterStatefulSet(optsWithDefaultAffinity).Spec.Template.Spec.Affinity.NodeAffinity)
		affinity := NewIngesterStatefulSet(optsWithNoDefaultAffinity).Spec.Template.Spec.Affinity
		if affinity != nil {
			assert.Empty(t, affinity.NodeAffinity)
		}
	})

	t.Run("compactor", func(t *testing.T) {
		assert.Equal(t, nodeAffinity, NewCompactorStatefulSet(optsWithDefaultAffinity).Spec.Template.Spec.Affinity.NodeAffinity)
		affinity := NewCompactorStatefulSet(optsWithNoDefaultAffinity).Spec.Template.Spec.Affinity
		if affinity != nil {
			assert.Empty(t, affinity.NodeAffinity)
		}
	})

	t.Run("index_gateway", func(t *testing.T) {
		assert.Equal(t, nodeAffinity, NewIndexGatewayStatefulSet(optsWithDefaultAffinity).Spec.Template.Spec.Affinity.NodeAffinity)
		affinity := NewIndexGatewayStatefulSet(optsWithNoDefaultAffinity).Spec.Template.Spec.Affinity
		if affinity != nil {
			assert.Empty(t, affinity.NodeAffinity)
		}
	})

	t.Run("ruler", func(t *testing.T) {
		assert.Equal(t, nodeAffinity, NewRulerStatefulSet(optsWithDefaultAffinity).Spec.Template.Spec.Affinity.NodeAffinity)
		affinity := NewRulerStatefulSet(optsWithNoDefaultAffinity).Spec.Template.Spec.Affinity
		if affinity != nil {
			assert.Empty(t, affinity.NodeAffinity)
		}
	})
}

func TestPodAntiAffinityForEachComponent(t *testing.T) {
	paTerm := []corev1.WeightedPodAffinityTerm{
		{
			Weight: 100,
			PodAffinityTerm: corev1.PodAffinityTerm{
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"foo": "bar",
					},
				},
				TopologyKey: "foo",
			},
		},
	}
	expectedPATerm := &corev1.PodAntiAffinity{
		PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
			{
				Weight: 100,
				PodAffinityTerm: corev1.PodAffinityTerm{
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"foo": "bar",
						},
					},
					TopologyKey: "foo",
				},
			},
		},
	}
	optsWithNoPodAntiAffinity := Options{
		// We need to set name here to propperly validate default PodAntiAffinity
		Name: "abcd",
		Stack: lokiv1.LokiStackSpec{
			Template: &lokiv1.LokiTemplateSpec{
				Compactor: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				Distributor: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				Ingester: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				Querier: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				QueryFrontend: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				IndexGateway: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				Ruler: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
			},
		},
	}
	optsWithPodAntiAffinity := Options{
		Stack: lokiv1.LokiStackSpec{
			Template: &lokiv1.LokiTemplateSpec{
				Compactor: &lokiv1.LokiComponentSpec{
					Replicas: 1,
					PodAntiAffinity: &corev1.PodAntiAffinity{
						PreferredDuringSchedulingIgnoredDuringExecution: paTerm,
					},
				},
				Distributor: &lokiv1.LokiComponentSpec{
					Replicas: 1,
					PodAntiAffinity: &corev1.PodAntiAffinity{
						PreferredDuringSchedulingIgnoredDuringExecution: paTerm,
					},
				},
				Ingester: &lokiv1.LokiComponentSpec{
					Replicas: 1,
					PodAntiAffinity: &corev1.PodAntiAffinity{
						PreferredDuringSchedulingIgnoredDuringExecution: paTerm,
					},
				},
				Querier: &lokiv1.LokiComponentSpec{
					Replicas: 1,
					PodAntiAffinity: &corev1.PodAntiAffinity{
						PreferredDuringSchedulingIgnoredDuringExecution: paTerm,
					},
				},
				QueryFrontend: &lokiv1.LokiComponentSpec{
					Replicas: 1,
					PodAntiAffinity: &corev1.PodAntiAffinity{
						PreferredDuringSchedulingIgnoredDuringExecution: paTerm,
					},
				},
				IndexGateway: &lokiv1.LokiComponentSpec{
					Replicas: 1,
					PodAntiAffinity: &corev1.PodAntiAffinity{
						PreferredDuringSchedulingIgnoredDuringExecution: paTerm,
					},
				},
				Ruler: &lokiv1.LokiComponentSpec{
					Replicas: 1,
					PodAntiAffinity: &corev1.PodAntiAffinity{
						PreferredDuringSchedulingIgnoredDuringExecution: paTerm,
					},
				},
			},
		},
	}

	t.Run("distributor", func(t *testing.T) {
		assert.Equal(t, expectedPATerm, NewDistributorDeployment(optsWithPodAntiAffinity).Spec.Template.Spec.Affinity.PodAntiAffinity)
		affinity := NewDistributorDeployment(optsWithNoPodAntiAffinity).Spec.Template.Spec.Affinity
		if affinity != nil {
			assert.Empty(t, affinity.PodAntiAffinity)
		}
	})

	t.Run("query_frontend", func(t *testing.T) {
		assert.Equal(t, expectedPATerm, NewQueryFrontendDeployment(optsWithPodAntiAffinity).Spec.Template.Spec.Affinity.PodAntiAffinity)
		affinity := NewQueryFrontendDeployment(optsWithNoPodAntiAffinity).Spec.Template.Spec.Affinity
		if affinity != nil {
			assert.Equal(t, expectedDefaultPodAntiAffinity("query-frontend"), affinity.PodAntiAffinity)
		}
	})

	t.Run("querier", func(t *testing.T) {
		assert.Equal(t, expectedPATerm, NewQuerierDeployment(optsWithPodAntiAffinity).Spec.Template.Spec.Affinity.PodAntiAffinity)
		affinity := NewQuerierDeployment(optsWithNoPodAntiAffinity).Spec.Template.Spec.Affinity
		if affinity != nil {
			assert.Empty(t, affinity.PodAntiAffinity)
		}
	})

	t.Run("ingester", func(t *testing.T) {
		assert.Equal(t, expectedPATerm, NewIngesterStatefulSet(optsWithPodAntiAffinity).Spec.Template.Spec.Affinity.PodAntiAffinity)
		affinity := NewIngesterStatefulSet(optsWithNoPodAntiAffinity).Spec.Template.Spec.Affinity
		if affinity != nil {
			assert.Equal(t, expectedDefaultPodAntiAffinity("ingester"), affinity.PodAntiAffinity)
		}
	})

	t.Run("compactor", func(t *testing.T) {
		assert.Equal(t, expectedPATerm, NewCompactorStatefulSet(optsWithPodAntiAffinity).Spec.Template.Spec.Affinity.PodAntiAffinity)
		affinity := NewCompactorStatefulSet(optsWithNoPodAntiAffinity).Spec.Template.Spec.Affinity
		if affinity != nil {
			assert.Empty(t, affinity.PodAntiAffinity)
		}
	})

	t.Run("index_gateway", func(t *testing.T) {
		assert.Equal(t, expectedPATerm, NewIndexGatewayStatefulSet(optsWithPodAntiAffinity).Spec.Template.Spec.Affinity.PodAntiAffinity)
		affinity := NewIndexGatewayStatefulSet(optsWithNoPodAntiAffinity).Spec.Template.Spec.Affinity
		if affinity != nil {
			assert.Empty(t, affinity.PodAntiAffinity)
		}
	})

	t.Run("ruler", func(t *testing.T) {
		assert.Equal(t, expectedPATerm, NewRulerStatefulSet(optsWithPodAntiAffinity).Spec.Template.Spec.Affinity.PodAntiAffinity)
		affinity := NewRulerStatefulSet(optsWithNoPodAntiAffinity).Spec.Template.Spec.Affinity
		if affinity != nil {
			assert.Equal(t, expectedDefaultPodAntiAffinity("ruler"), affinity.PodAntiAffinity)
		}
	})
}

func expectedDefaultPodAntiAffinity(component string) *corev1.PodAntiAffinity {
	return &corev1.PodAntiAffinity{
		PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
			{
				Weight: 100,
				PodAffinityTerm: corev1.PodAffinityTerm{
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app.kubernetes.io/instance":  "abcd",
							"app.kubernetes.io/component": component,
						},
					},
					TopologyKey: "kubernetes.io/hostname",
				},
			},
		},
	}
}
