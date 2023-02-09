package manifests_test

import (
	"math/rand"
	"testing"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/manifests"
	"github.com/grafana/loki/operator/internal/manifests/openshift"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func TestNewRulerStatefulSet_HasTemplateConfigHashAnnotation(t *testing.T) {
	ss := manifests.NewRulerStatefulSet(manifests.Options{
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

	expected := "loki.grafana.com/config-hash"
	annotations := ss.Spec.Template.Annotations
	require.Contains(t, annotations, expected)
	require.Equal(t, annotations[expected], "deadbeef")
}

func TestNewRulerStatefulSet_HasTemplateCertRotationRequiredAtAnnotation(t *testing.T) {
	ss := manifests.NewRulerStatefulSet(manifests.Options{
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
	expected := "loki.grafana.com/certRotationRequiredAt"
	annotations := ss.Spec.Template.Annotations
	require.Contains(t, annotations, expected)
	require.Equal(t, annotations[expected], "deadbeef")
}

func TestBuildRuler_HasExtraObjectsForTenantMode(t *testing.T) {
	objs, err := manifests.BuildRuler(manifests.Options{
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
	require.Len(t, objs, 7)
}

func TestNewRulerStatefulSet_SelectorMatchesLabels(t *testing.T) {
	// You must set the .spec.selector field of a StatefulSet to match the labels of
	// its .spec.template.metadata.labels. Prior to Kubernetes 1.8, the
	// .spec.selector field was defaulted when omitted. In 1.8 and later versions,
	// failing to specify a matching Pod Selector will result in a validation error
	// during StatefulSet creation.
	// See https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/#pod-selector
	sts := manifests.NewRulerStatefulSet(manifests.Options{
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
	sts := manifests.NewRulerStatefulSet(manifests.Options{
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
		Tenants: manifests.Tenants{
			Configs: map[string]manifests.TenantConfig{
				"tenant-a": {RuleFiles: []string{"rule-a-alerts.yaml", "rule-b-recs.yaml"}},
				"tenant-b": {RuleFiles: []string{"rule-a-alerts.yaml", "rule-b-recs.yaml"}},
			},
		},
	})

	vs := sts.Spec.Template.Spec.Volumes

	var (
		volumeNames []string
		volumeItems []corev1.KeyToPath
	)
	for _, v := range vs {
		volumeNames = append(volumeNames, v.Name)
		volumeItems = append(volumeItems, v.ConfigMap.Items...)
	}

	require.NotEmpty(t, volumeNames)
	require.NotEmpty(t, volumeItems)
}
