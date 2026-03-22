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
	"github.com/grafana/loki/operator/internal/manifests/openshift"
	"github.com/grafana/loki/operator/internal/manifests/storage"
)

func TestNewRulerStatefulSet_HasTemplateConfigHashAnnotation(t *testing.T) {
	ss := NewRulerStatefulSet(Options{
		Name:       "abcd",
		Namespace:  "efgh",
		ConfigSHA1: "deadbeef",
		Stack: lokiv1.LokiStackSpec{
			StorageClassName: "standard",
			Template: &lokiv1.LokiTemplateSpec{
				Ruler: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
			},
		},
	})

	annotations := ss.Spec.Template.Annotations
	require.Contains(t, annotations, AnnotationLokiConfigHash)
	require.Equal(t, annotations[AnnotationLokiConfigHash], "deadbeef")
}

func TestNewRulerStatefulSet_HasTemplateObjectStoreHashAnnotation(t *testing.T) {
	ss := NewRulerStatefulSet(Options{
		Name:      "abcd",
		Namespace: "efgh",
		ObjectStorage: storage.Options{
			SecretSHA1: "deadbeef",
		},
		Stack: lokiv1.LokiStackSpec{
			StorageClassName: "standard",
			Template: &lokiv1.LokiTemplateSpec{
				Ruler: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
			},
		},
	})

	annotations := ss.Spec.Template.Annotations
	require.Contains(t, annotations, AnnotationLokiObjectStoreHash)
	require.Equal(t, annotations[AnnotationLokiObjectStoreHash], "deadbeef")
}

func TestNewRulerStatefulSet_HasTemplateCertRotationRequiredAtAnnotation(t *testing.T) {
	ss := NewRulerStatefulSet(Options{
		Name:                   "abcd",
		Namespace:              "efgh",
		CertRotationRequiredAt: "deadbeef",
		Stack: lokiv1.LokiStackSpec{
			StorageClassName: "standard",
			Template: &lokiv1.LokiTemplateSpec{
				Ruler: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
			},
		},
	})

	annotations := ss.Spec.Template.Annotations
	require.Contains(t, annotations, AnnotationCertRotationRequiredAt)
	require.Equal(t, annotations[AnnotationCertRotationRequiredAt], "deadbeef")
}

func TestBuildRuler_HasExtraObjectsForTenantMode(t *testing.T) {
	objs, err := BuildRuler(Options{
		Name:      "abcd",
		Namespace: "efgh",
		OpenShiftOptions: openshift.Options{
			BuildOpts: openshift.BuildOptions{
				LokiStackName:      "abc",
				LokiStackNamespace: "efgh",
			},
		},
		Stack: lokiv1.LokiStackSpec{
			Template: &lokiv1.LokiTemplateSpec{
				Ruler: &lokiv1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
			},
			Rules: &lokiv1.RulesSpec{
				Enabled: true,
			},
			Tenants: &lokiv1.TenantsSpec{
				Mode: lokiv1.OpenshiftLogging,
			},
		},
	})

	require.NoError(t, err)
	require.Len(t, objs, 8)
}

func TestNewRulerStatefulSet_SelectorMatchesLabels(t *testing.T) {
	// You must set the .spec.selector field of a StatefulSet to match the labels of
	// its .spec.template.metadata.labels. Prior to Kubernetes 1.8, the
	// .spec.selector field was defaulted when omitted. In 1.8 and later versions,
	// failing to specify a matching Pod Selector will result in a validation error
	// during StatefulSet creation.
	// See https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/#pod-selector
	sts := NewRulerStatefulSet(Options{
		Name:      "abcd",
		Namespace: "efgh",
		Stack: lokiv1.LokiStackSpec{
			StorageClassName: "standard",
			Template: &lokiv1.LokiTemplateSpec{
				Ruler: &lokiv1.LokiComponentSpec{
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

func TestNewRulerStatefulSet_MountsRulesInPerTenantIDSubDirectories(t *testing.T) {
	opts := Options{
		Name:      "abcd",
		Namespace: "efgh",
		Stack: lokiv1.LokiStackSpec{
			StorageClassName: "standard",
			Template: &lokiv1.LokiTemplateSpec{
				Ruler: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
			},
		},
		Tenants: Tenants{
			Configs: map[string]TenantConfig{
				"tenant-a": {RuleFiles: []string{"test-rules-0___tenant-a___rule-a-alerts.yaml", "test-rules-0___tenant-a___rule-b-recs.yaml"}},
				"tenant-b": {RuleFiles: []string{"test-rules-0___tenant-b___rule-a-alerts.yaml", "test-rules-0___tenant-b___rule-b-recs.yaml"}},
			},
		},
		RulesConfigMapNames: []string{"config"},
	}
	sts := NewRulerStatefulSet(opts)

	vs := sts.Spec.Template.Spec.Volumes

	var volumeNames []string
	for _, v := range vs {
		volumeNames = append(volumeNames, v.Name)
	}

	require.NotEmpty(t, volumeNames)
}

func TestNewRulerStatefulSet_ShardedRulesConfigMap(t *testing.T) {
	// Create a large config map which will be split into 2 shards
	opts := testOptions_withSharding()
	rulesCMShards, err := RulesConfigMapShards(opts)
	require.NoError(t, err)
	require.NotNil(t, rulesCMShards)
	require.Len(t, rulesCMShards, 2)

	for _, shard := range rulesCMShards {
		opts.RulesConfigMapNames = append(opts.RulesConfigMapNames, shard.Name)
	}

	// Create the Ruler StatefulSet and mount the ConfigMap shards into the Ruler pod
	sts := NewRulerStatefulSet(*opts)

	vs := sts.Spec.Template.Spec.Volumes

	var volumeNames []string
	var volumeProjections []corev1.VolumeProjection
	for _, v := range vs {
		volumeNames = append(volumeNames, v.Name)
		if v.Name == RulesStorageVolumeName() {
			volumeProjections = append(volumeProjections, v.Projected.Sources...)
		}
	}

	require.Len(t, volumeNames, 2)
	require.Len(t, volumeProjections, 2)
}

func TestBuildRuler_PodDisruptionBudget(t *testing.T) {
	opts := Options{
		Name:      "abcd",
		Namespace: "efgh",
		Stack: lokiv1.LokiStackSpec{
			Template: &lokiv1.LokiTemplateSpec{
				Ruler: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
			},
		},
	}

	objs, err := BuildRuler(opts)

	require.NoError(t, err)
	require.Len(t, objs, 4)

	pdb := objs[3].(*policyv1.PodDisruptionBudget)
	require.NotNil(t, pdb)
	require.Equal(t, "abcd-ruler", pdb.Name)
	require.Equal(t, "efgh", pdb.Namespace)
	require.NotNil(t, pdb.Spec.MinAvailable.IntVal)
	require.Equal(t, int32(1), pdb.Spec.MinAvailable.IntVal)
	require.EqualValues(t, ComponentLabels(LabelRulerComponent, opts.Name), pdb.Spec.Selector.MatchLabels)
}

func TestNewRulerStatefulSet_TopologySpreadConstraints(t *testing.T) {
	obj, _ := BuildRuler(Options{
		Name:      "abcd",
		Namespace: "efgh",
		Stack: lokiv1.LokiStackSpec{
			Template: &lokiv1.LokiTemplateSpec{
				Ruler: &lokiv1.LokiComponentSpec{
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

	ss := obj[0].(*appsv1.StatefulSet)
	require.Equal(t, []corev1.TopologySpreadConstraint{
		{
			MaxSkew:           2,
			TopologyKey:       "zone",
			WhenUnsatisfiable: "DoNotSchedule",
			LabelSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/component": "ruler",
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
					"app.kubernetes.io/component": "ruler",
					"app.kubernetes.io/instance":  "abcd",
				},
			},
		},
	}, ss.Spec.Template.Spec.TopologySpreadConstraints)
}
