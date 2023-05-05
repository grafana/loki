package manifests

import (
	"fmt"
	"testing"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestAffinity(t *testing.T, componentLabel string, makeObject func(cSpec *lokiv1.LokiComponentSpec, nAffinity bool) client.Object) {
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
	expectedPATerm := []corev1.WeightedPodAffinityTerm{
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
	defaultPAA := &corev1.PodAntiAffinity{
		PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
			{
				Weight: 100,
				PodAffinityTerm: corev1.PodAffinityTerm{
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app.kubernetes.io/component": componentLabel,
							"app.kubernetes.io/instance":  "abcd",
						},
					},
					TopologyKey: "kubernetes.io/hostname",
				},
			},
		},
	}

	for _, tc := range []struct {
		Name             string
		ComponentSpec    *lokiv1.LokiComponentSpec
		NodeAffinity     bool
		ExpectedAffinity *corev1.Affinity
	}{
		{
			Name: "default_no_config",
			ComponentSpec: &lokiv1.LokiComponentSpec{
				Replicas: 1,
			},
		},
		{
			Name: "node_affinity_default_config",
			ComponentSpec: &lokiv1.LokiComponentSpec{
				Replicas: 1,
			},
			NodeAffinity: true,
			ExpectedAffinity: &corev1.Affinity{
				NodeAffinity: defaultNodeAffinity(true),
			},
		},
		{
			Name: "pod_anti_affinity_user_config",
			ComponentSpec: &lokiv1.LokiComponentSpec{
				Replicas: 1,
				PodAntiAffinity: &corev1.PodAntiAffinity{
					PreferredDuringSchedulingIgnoredDuringExecution: paTerm,
				},
			},
			ExpectedAffinity: &corev1.Affinity{
				PodAntiAffinity: &corev1.PodAntiAffinity{
					PreferredDuringSchedulingIgnoredDuringExecution: expectedPATerm,
				},
			},
		},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			object := makeObject(tc.ComponentSpec, tc.NodeAffinity)
			affinity, ok, err := extractAffinity(object)
			require.NoError(t, err)
			require.False(t, ok)

			// Some components have default PodAntiAffinity configured for those
			// we have to change the expectedAffinity slightly
			if _, ok := podAntiAffinityComponents[componentLabel]; ok {
				if tc.ExpectedAffinity == nil {
					tc.ExpectedAffinity = &corev1.Affinity{}
				}
				if tc.ExpectedAffinity.PodAntiAffinity == nil {
					tc.ExpectedAffinity.PodAntiAffinity = defaultPAA
				}
			}

			if tc.ExpectedAffinity == nil {
				require.Nil(t, affinity)
				return
			}
			require.Equal(t, tc.ExpectedAffinity, affinity)
		})
	}
}

func extractAffinity(raw client.Object) (*corev1.Affinity, bool, error) {
	switch obj := raw.(type) {
	case *appsv1.Deployment:
		return obj.Spec.Template.Spec.Affinity, false, nil
	case *appsv1.StatefulSet:
		return obj.Spec.Template.Spec.Affinity, false, nil
	case *corev1.ConfigMap, *corev1.Service, *policyv1.PodDisruptionBudget:
		return nil, true, nil
	default:
	}

	return nil, false, fmt.Errorf("unknown kind: %s", raw.GetObjectKind().GroupVersionKind())
}
