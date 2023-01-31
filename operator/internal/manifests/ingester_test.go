package manifests_test

import (
	"testing"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/manifests"
	"github.com/stretchr/testify/require"
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
