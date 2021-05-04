package manifests

import (
	"testing"

	lokiv1beta1 "github.com/ViaQ/loki-operator/api/v1beta1"
	"github.com/ViaQ/loki-operator/internal/manifests/internal"
	"github.com/stretchr/testify/require"
)

func TestApplyUserOptions_OverrideDefaults(t *testing.T) {
	allSizes := []lokiv1beta1.LokiStackSizeType{
		lokiv1beta1.SizeOneXExtraSmall,
		lokiv1beta1.SizeOneXSmall,
		lokiv1beta1.SizeOneXMedium,
	}
	for _, size := range allSizes {
		in := Options{
			Name:      "abcd",
			Namespace: "efgh",
			Stack: lokiv1beta1.LokiStackSpec{
				Size: size,
				Template: &lokiv1beta1.LokiTemplateSpec{
					Distributor: &lokiv1beta1.LokiComponentSpec{
						Replicas: 42,
					},
				},
			},
		}
		out, err := applyUserOptions(in)
		defs := internal.StackSizeTable[size]

		require.NoError(t, err)
		require.Equal(t, defs.Size, out.Stack.Size)
		require.Equal(t, defs.Limits, out.Stack.Limits)
		require.Equal(t, defs.ReplicationFactor, out.Stack.ReplicationFactor)
		require.Equal(t, defs.ManagementState, out.Stack.ManagementState)
		require.Equal(t, defs.Template.Ingester, out.Stack.Template.Ingester)
		require.Equal(t, defs.Template.Querier, out.Stack.Template.Querier)
		require.Equal(t, defs.Template.QueryFrontend, out.Stack.Template.QueryFrontend)

		// Require distributor replicas to be set by user overwrite
		require.NotEqual(t, defs.Template.Distributor.Replicas, out.Stack.Template.Distributor.Replicas)

		// Require distributor tolerations and nodeselectors to use defaults
		require.Equal(t, defs.Template.Distributor.Tolerations, out.Stack.Template.Distributor.Tolerations)
		require.Equal(t, defs.Template.Distributor.NodeSelector, out.Stack.Template.Distributor.NodeSelector)
	}
}

func TestApplyUserOptions_AlwaysSetCompactorReplicasToOne(t *testing.T) {
	allSizes := []lokiv1beta1.LokiStackSizeType{
		lokiv1beta1.SizeOneXExtraSmall,
		lokiv1beta1.SizeOneXSmall,
		lokiv1beta1.SizeOneXMedium,
	}
	for _, size := range allSizes {
		in := Options{
			Name:      "abcd",
			Namespace: "efgh",
			Stack: lokiv1beta1.LokiStackSpec{
				Size: size,
				Template: &lokiv1beta1.LokiTemplateSpec{
					Compactor: &lokiv1beta1.LokiComponentSpec{
						Replicas: 2,
					},
				},
			},
		}
		out, err := applyUserOptions(in)
		defs := internal.StackSizeTable[size]

		require.NoError(t, err)

		// Require compactor to be reverted to 1 replica
		require.Equal(t, defs.Template.Compactor, out.Stack.Template.Compactor)
	}
}
