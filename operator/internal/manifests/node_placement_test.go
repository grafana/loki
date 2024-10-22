package manifests

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configv1 "github.com/grafana/loki/operator/api/config/v1"
	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
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

var podAntiAffinityTestTable = []struct {
	component string
	generator func(Options) *corev1.Affinity
}{
	{
		component: "lokistack-gateway",
		generator: func(opts Options) *corev1.Affinity {
			return NewGatewayDeployment(opts, "").Spec.Template.Spec.Affinity
		},
	},
	{
		component: "distributor",
		generator: func(opts Options) *corev1.Affinity {
			return NewDistributorDeployment(opts).Spec.Template.Spec.Affinity
		},
	},
	{
		component: "query-frontend",
		generator: func(opts Options) *corev1.Affinity {
			return NewQueryFrontendDeployment(opts).Spec.Template.Spec.Affinity
		},
	},
	{
		component: "querier",
		generator: func(opts Options) *corev1.Affinity {
			return NewQuerierDeployment(opts).Spec.Template.Spec.Affinity
		},
	},
	{
		component: "ingester",
		generator: func(opts Options) *corev1.Affinity {
			return NewIngesterStatefulSet(opts).Spec.Template.Spec.Affinity
		},
	},
	{
		component: "compactor",
		generator: func(opts Options) *corev1.Affinity {
			return NewCompactorStatefulSet(opts).Spec.Template.Spec.Affinity
		},
	},
	{
		component: "index-gateway",
		generator: func(opts Options) *corev1.Affinity {
			return NewIndexGatewayStatefulSet(opts).Spec.Template.Spec.Affinity
		},
	},
	{
		component: "ruler",
		generator: func(opts Options) *corev1.Affinity {
			return NewRulerStatefulSet(opts).Spec.Template.Spec.Affinity
		},
	},
}

func TestDefaultPodAntiAffinity(t *testing.T) {
	opts := Options{
		// We need to set name here to properly validate default PodAntiAffinity
		Name: "abcd",
		Stack: lokiv1.LokiStackSpec{
			Template: &lokiv1.LokiTemplateSpec{
				Compactor: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				Distributor: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
				Gateway: &lokiv1.LokiComponentSpec{
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

	for _, tc := range podAntiAffinityTestTable {
		t.Run(tc.component, func(t *testing.T) {
			t.Parallel()

			wantAffinity := defaultPodAntiAffinity(tc.component, "abcd")

			affinity := tc.generator(opts)
			assert.Equal(t, wantAffinity, affinity.PodAntiAffinity)
		})
	}
}

func TestCustomPodAntiAffinity(t *testing.T) {
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

	wantAffinity := &corev1.PodAntiAffinity{
		PreferredDuringSchedulingIgnoredDuringExecution: paTerm,
	}

	opts := Options{
		Stack: lokiv1.LokiStackSpec{
			Template: &lokiv1.LokiTemplateSpec{
				Compactor: &lokiv1.LokiComponentSpec{
					Replicas:        1,
					PodAntiAffinity: wantAffinity,
				},
				Distributor: &lokiv1.LokiComponentSpec{
					Replicas:        1,
					PodAntiAffinity: wantAffinity,
				},
				Gateway: &lokiv1.LokiComponentSpec{
					Replicas:        1,
					PodAntiAffinity: wantAffinity,
				},
				Ingester: &lokiv1.LokiComponentSpec{
					Replicas:        1,
					PodAntiAffinity: wantAffinity,
				},
				Querier: &lokiv1.LokiComponentSpec{
					Replicas:        1,
					PodAntiAffinity: wantAffinity,
				},
				QueryFrontend: &lokiv1.LokiComponentSpec{
					Replicas:        1,
					PodAntiAffinity: wantAffinity,
				},
				IndexGateway: &lokiv1.LokiComponentSpec{
					Replicas:        1,
					PodAntiAffinity: wantAffinity,
				},
				Ruler: &lokiv1.LokiComponentSpec{
					Replicas:        1,
					PodAntiAffinity: wantAffinity,
				},
			},
		},
	}

	for _, tc := range podAntiAffinityTestTable {
		t.Run(tc.component, func(t *testing.T) {
			t.Parallel()

			affinity := tc.generator(opts)
			assert.Equal(t, wantAffinity, affinity.PodAntiAffinity)
		})
	}
}

func TestCustomTopologySpreadConstraints(t *testing.T) {
	template := &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"abc": "edf",
				"gfi": "jkl",
			},
			Annotations: map[string]string{
				"one": "value",
				"two": "values",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "a-container",
					Image: "an-image:latest",
					Env: []corev1.EnvVar{
						{
							Name:  "test",
							Value: "other",
						},
					},
				},
				{
					Name:  "b-container",
					Image: "an-image:latest",
					Env: []corev1.EnvVar{
						{
							Name:  "test",
							Value: "other",
						},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "test-volume",
				},
			},
		},
	}

	spec := &lokiv1.ReplicationSpec{
		Factor: 2,
		Zones: []lokiv1.ZoneSpec{
			{
				MaxSkew:     1,
				TopologyKey: "datacenter",
			},
			{
				MaxSkew:     1,
				TopologyKey: "rack",
			},
		},
	}

	expectedTemplate := &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"abc":                    "edf",
				"gfi":                    "jkl",
				lokiv1.LabelZoneAwarePod: "enabled",
			},
			Annotations: map[string]string{
				"one":                                   "value",
				"two":                                   "values",
				lokiv1.AnnotationAvailabilityZoneLabels: "datacenter,rack",
			},
		},
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{
				{
					Name:  "az-annotation-check",
					Image: "an-image:latest",
					Command: []string{
						"sh",
						"-c",
						"while ! [ -s /etc/az-annotation/az ]; do echo Waiting for availability zone annotation to be set; sleep 2; done; echo availability zone annotation is set; cat /etc/az-annotation/az; echo",
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "az-annotation",
							MountPath: "/etc/az-annotation",
						},
					},
				},
			},
			Containers: []corev1.Container{
				{
					Name:  "a-container",
					Image: "an-image:latest",
					Env: []corev1.EnvVar{
						{
							Name:  "test",
							Value: "other",
						},
						availabilityZoneEnvVar,
					},
				},
				{
					Name:  "b-container",
					Image: "an-image:latest",
					Env: []corev1.EnvVar{
						{
							Name:  "test",
							Value: "other",
						},
						availabilityZoneEnvVar,
					},
				},
			},
			TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
				{
					MaxSkew:           int32(1),
					TopologyKey:       "datacenter",
					WhenUnsatisfiable: corev1.DoNotSchedule,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							kubernetesComponentLabel: "component",
							kubernetesInstanceLabel:  "a-stack",
						},
					},
				},
				{
					MaxSkew:           int32(1),
					TopologyKey:       "rack",
					WhenUnsatisfiable: corev1.DoNotSchedule,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							kubernetesComponentLabel: "component",
							kubernetesInstanceLabel:  "a-stack",
						},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "test-volume",
				},
				{
					Name: "az-annotation",
					VolumeSource: corev1.VolumeSource{
						DownwardAPI: &corev1.DownwardAPIVolumeSource{
							Items: []corev1.DownwardAPIVolumeFile{
								{
									Path: "az",
									FieldRef: &corev1.ObjectFieldSelector{
										FieldPath: availabilityZoneFieldPath,
									},
								},
							},
						},
					},
				},
			},
		},
	}

	err := configureReplication(template, spec, "component", "a-stack")
	require.NoError(t, err)
	require.Equal(t, expectedTemplate, template)
}
