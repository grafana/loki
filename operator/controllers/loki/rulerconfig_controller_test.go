package controllers

import (
	"testing"

	"github.com/stretchr/testify/require"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s/k8sfakes"
)

func TestRulerConfigController_RegistersCustomResource_WithDefaultPredicates(t *testing.T) {
	b := &k8sfakes.FakeBuilder{}
	k := &k8sfakes.FakeClient{}
	c := &RulerConfigReconciler{Client: k, Log: logger, Scheme: scheme}

	b.ForReturns(b)
	b.WithLogConstructorReturns(b)

	err := c.buildController(b)
	require.NoError(t, err)

	require.Equal(t, 1, b.ForCallCount())

	obj, _ := b.ForArgsForCall(0)
	require.Equal(t, &lokiv1.RulerConfig{}, obj)
}

func TestRulerConfigController_RegisterGenericLogContructor(t *testing.T) {
	b := &k8sfakes.FakeBuilder{}
	k := &k8sfakes.FakeClient{}
	c := &RulerConfigReconciler{Client: k, Log: logger, Scheme: scheme}

	b.ForReturns(b)
	b.WithLogConstructorReturns(b)

	err := c.buildController(b)
	require.NoError(t, err)

	require.Equal(t, 1, b.WithLogConstructorCallCount())
}
