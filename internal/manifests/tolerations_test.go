package manifests

import (
	"testing"

	lokiv1beta1 "github.com/ViaQ/loki-operator/api/v1beta1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
)

func TestTolerationsAreSetForEachComponent(t *testing.T) {
	tolerations := []corev1.Toleration{{
		Key:      "type",
		Operator: corev1.TolerationOpEqual,
		Value:    "storage",
		Effect:   corev1.TaintEffectNoSchedule,
	}}
	optsWithTolerations := Options{
		Stack: lokiv1beta1.LokiStackSpec{
			Template: lokiv1beta1.LokiTemplateSpec{
				Compactor: lokiv1beta1.LokiComponentSpec{
					Tolerations: tolerations,
				},
				Distributor: lokiv1beta1.LokiComponentSpec{
					Tolerations: tolerations,
				},
				Ingester: lokiv1beta1.LokiComponentSpec{
					Tolerations: tolerations,
				},
				Querier: lokiv1beta1.LokiComponentSpec{
					Tolerations: tolerations,
				},
				QueryFrontend: lokiv1beta1.LokiComponentSpec{
					Tolerations: tolerations,
				},
			},
		},
		ObjectStorage: ObjectStorage{},
	}

	t.Run("distributor", func(t *testing.T) {
		assert.Equal(t, tolerations, NewDistributorDeployment(optsWithTolerations).Spec.Template.Spec.Tolerations)
		assert.Empty(t, NewDistributorDeployment(Options{}).Spec.Template.Spec.Tolerations)
	})

	t.Run("query_frontend", func(t *testing.T) {
		assert.Equal(t, tolerations, NewQueryFrontendDeployment(optsWithTolerations).Spec.Template.Spec.Tolerations)
		assert.Empty(t, NewQueryFrontendDeployment(Options{}).Spec.Template.Spec.Tolerations)
	})

	t.Run("querier", func(t *testing.T) {
		assert.Equal(t, tolerations, NewQuerierStatefulSet(optsWithTolerations).Spec.Template.Spec.Tolerations)
		assert.Empty(t, NewQuerierStatefulSet(Options{}).Spec.Template.Spec.Tolerations)
	})

	t.Run("ingester", func(t *testing.T) {
		assert.Equal(t, tolerations, NewIngesterStatefulSet(optsWithTolerations).Spec.Template.Spec.Tolerations)
		assert.Empty(t, NewIngesterStatefulSet(Options{}).Spec.Template.Spec.Tolerations)
	})

	t.Run("compactor", func(t *testing.T) {
		assert.Equal(t, tolerations, NewCompactorStatefulSet(optsWithTolerations).Spec.Template.Spec.Tolerations)
		assert.Empty(t, NewCompactorStatefulSet(Options{}).Spec.Template.Spec.Tolerations)
	})
}
