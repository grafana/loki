package controllers

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s/k8sfakes"
)

func TestRecordingRuleController_RegistersCustomResource_WithDefaultPredicates(t *testing.T) {
	b := &k8sfakes.FakeBuilder{}
	k := &k8sfakes.FakeClient{}
	c := &RecordingRuleReconciler{Client: k, Log: logger, Scheme: scheme}

	b.ForReturns(b)
	b.WatchesReturns(b)
	b.WithLogConstructorReturns(b)

	err := c.buildController(b)
	require.NoError(t, err)

	require.Equal(t, 1, b.ForCallCount())

	obj, _ := b.ForArgsForCall(0)
	require.Equal(t, &lokiv1.RecordingRule{}, obj)
}

func TestRecordingRuleController_RegisterWatchesResources(t *testing.T) {
	b := &k8sfakes.FakeBuilder{}
	k := &k8sfakes.FakeClient{}
	c := &RecordingRuleReconciler{Client: k, Log: logger, Scheme: scheme}

	b.ForReturns(b)
	b.WatchesReturns(b)
	b.WithLogConstructorReturns(b)

	err := c.buildController(b)
	require.NoError(t, err)

	require.Equal(t, 1, b.WatchesCallCount())

	obj, _, _ := b.WatchesArgsForCall(0)
	require.Equal(t, &corev1.Namespace{}, obj)
}

func TestRecordingRuleController_RegisterGenericLogContructor(t *testing.T) {
	b := &k8sfakes.FakeBuilder{}
	k := &k8sfakes.FakeClient{}
	c := &RecordingRuleReconciler{Client: k, Log: logger, Scheme: scheme}

	b.ForReturns(b)
	b.WatchesReturns(b)
	b.WithLogConstructorReturns(b)

	err := c.buildController(b)
	require.NoError(t, err)

	require.Equal(t, 1, b.WithLogConstructorCallCount())
}
