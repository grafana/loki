package manifests

import (
	"math/rand"
	"testing"

	lokiv1beta1 "github.com/ViaQ/loki-operator/api/v1beta1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestNewGatewayDeployment_HasTemplateConfigHashAnnotation(t *testing.T) {
	sha1C := "deadbeef"
	ss := NewGatewayDeployment(Options{
		Name:      "abcd",
		Namespace: "efgh",
		Stack: lokiv1beta1.LokiStackSpec{
			Template: &lokiv1beta1.LokiTemplateSpec{
				Compactor: &lokiv1beta1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
				Distributor: &lokiv1beta1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
				Ingester: &lokiv1beta1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
				Querier: &lokiv1beta1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
				QueryFrontend: &lokiv1beta1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
			},
		},
	}, sha1C)

	expected := "loki.openshift.io/config-hash"
	annotations := ss.Spec.Template.Annotations
	require.Contains(t, annotations, expected)
	require.Equal(t, annotations[expected], sha1C)
}

func TestGatewayConfigMap_ReturnsSHA1OfBinaryContents(t *testing.T) {
	opts := Options{
		Name:      uuid.New().String(),
		Namespace: uuid.New().String(),
		Image:     uuid.New().String(),
		Stack: lokiv1beta1.LokiStackSpec{
			Template: &lokiv1beta1.LokiTemplateSpec{
				Compactor: &lokiv1beta1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
				Distributor: &lokiv1beta1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
				Ingester: &lokiv1beta1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
				Querier: &lokiv1beta1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
				QueryFrontend: &lokiv1beta1.LokiComponentSpec{
					Replicas: rand.Int31(),
				},
			},
		},
	}

	_, sha1C, err := gatewayConfigMap(opts)
	require.NoError(t, err)
	require.NotEmpty(t, sha1C)
}
