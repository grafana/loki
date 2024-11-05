package manifests

import (
	"testing"

	"github.com/stretchr/testify/assert"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/manifests/storage"
)

func TestServiceAccountName_MatchesPodSpecServiceAccountName(t *testing.T) {
	opts := Options{
		Name:      "lokistack",
		Namespace: "ns",
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

	sa := BuildServiceAccount(opts)

	t.Run("distributor", func(t *testing.T) {
		assert.Equal(t, sa.GetName(), NewDistributorDeployment(opts).Spec.Template.Spec.ServiceAccountName)
	})

	t.Run("query_frontend", func(t *testing.T) {
		assert.Equal(t, sa.GetName(), NewQueryFrontendDeployment(opts).Spec.Template.Spec.ServiceAccountName)
	})

	t.Run("querier", func(t *testing.T) {
		assert.Equal(t, sa.GetName(), NewQuerierDeployment(opts).Spec.Template.Spec.ServiceAccountName)
	})

	t.Run("ingester", func(t *testing.T) {
		assert.Equal(t, sa.GetName(), NewIngesterStatefulSet(opts).Spec.Template.Spec.ServiceAccountName)
	})

	t.Run("compactor", func(t *testing.T) {
		assert.Equal(t, sa.GetName(), NewCompactorStatefulSet(opts).Spec.Template.Spec.ServiceAccountName)
	})

	t.Run("index_gateway", func(t *testing.T) {
		assert.Equal(t, sa.GetName(), NewIndexGatewayStatefulSet(opts).Spec.Template.Spec.ServiceAccountName)
	})
}
