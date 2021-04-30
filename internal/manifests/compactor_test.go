package manifests_test

import (
	"testing"

	"github.com/ViaQ/loki-operator/internal/manifests"
	"github.com/stretchr/testify/require"
)

func TestNewCompactorStatefulSet_SelectorMatchesLabels(t *testing.T) {
	// You must set the .spec.selector field of a StatefulSet to match the labels of
	// its .spec.template.metadata.labels. Prior to Kubernetes 1.8, the
	// .spec.selector field was defaulted when omitted. In 1.8 and later versions,
	// failing to specify a matching Pod Selector will result in a validation error
	// during StatefulSet creation.
	// See https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/#pod-selector
	ss := manifests.NewCompactorStatefulSet(manifests.Options{
		Name:      "abcd",
		Namespace: "efgh",
	})
	l := ss.Spec.Template.GetObjectMeta().GetLabels()
	for key, value := range ss.Spec.Selector.MatchLabels {
		require.Contains(t, l, key)
		require.Equal(t, l[key], value)
	}
}

func TestNewCompactorStatefulSet_HasTemplateConfigHashAnnotation(t *testing.T) {
	ss := manifests.NewCompactorStatefulSet(manifests.Options{
		Name:       "abcd",
		Namespace:  "efgh",
		ConfigSHA1: "deadbeef",
	})
	expected := "loki.openshift.io/config-hash"
	annotations := ss.Spec.Template.Annotations
	require.Contains(t, annotations, expected)
	require.Equal(t, annotations[expected], "deadbeef")
}
