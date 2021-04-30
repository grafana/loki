package manifests_test

import (
	"testing"

	lokiv1beta1 "github.com/ViaQ/loki-operator/api/v1beta1"
	"github.com/ViaQ/loki-operator/internal/manifests"
	"github.com/stretchr/testify/require"
)

func TestNewQuerierStatefulSet_HasTemplateConfigHashAnnotation(t *testing.T) {
	ss := manifests.NewQuerierStatefulSet(manifests.Options{
		Name:       "abcd",
		Namespace:  "efgh",
		ConfigSHA1: "deadbeef",
		Stack: lokiv1beta1.LokiStackSpec{
			StorageClassName: "standard",
		},
	})

	expected := "loki.openshift.io/config-hash"
	annotations := ss.Spec.Template.Annotations
	require.Contains(t, annotations, expected)
	require.Equal(t, annotations[expected], "deadbeef")
}

func TestNewQuerierStatefulSet_SelectorMatchesLabels(t *testing.T) {
	// You must set the .spec.selector field of a StatefulSet to match the labels of
	// its .spec.template.metadata.labels. Prior to Kubernetes 1.8, the
	// .spec.selector field was defaulted when omitted. In 1.8 and later versions,
	// failing to specify a matching Pod Selector will result in a validation error
	// during StatefulSet creation.
	// See https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/#pod-selector
	ss := manifests.NewQuerierStatefulSet(manifests.Options{
		Name:      "abcd",
		Namespace: "efgh",
		Stack: lokiv1beta1.LokiStackSpec{
			StorageClassName: "standard",
		},
	})

	l := ss.Spec.Template.GetObjectMeta().GetLabels()
	for key, value := range ss.Spec.Selector.MatchLabels {
		require.Contains(t, l, key)
		require.Equal(t, l[key], value)
	}
}
