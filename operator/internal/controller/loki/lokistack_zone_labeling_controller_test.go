package loki

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/grafana/loki/operator/internal/external/k8s/k8sfakes"
)

func TestLokiStackZoneAwarePodController_RegisterWatchedResources(t *testing.T) {
	k := &k8sfakes.FakeClient{}

	// Require owned resources
	type test struct {
		index             int
		watchesCallsCount int
		src               client.Object
		pred              builder.OwnsOption
	}
	table := []test{
		{
			src:               &corev1.Pod{},
			index:             0,
			watchesCallsCount: 1,
			pred:              createOrUpdatePodWithLabelPred,
		},
	}
	for _, tst := range table {
		b := &k8sfakes.FakeBuilder{}
		b.NamedReturns(b)
		b.WatchesReturns(b)

		c := &LokiStackZoneAwarePodReconciler{Client: k}
		err := c.buildController(b)
		require.NoError(t, err)

		// Require Watches-calls for all watches resources
		require.Equal(t, tst.watchesCallsCount, b.WatchesCallCount())

		src, _, opts := b.WatchesArgsForCall(tst.index)
		require.Equal(t, tst.src, src)
		require.Equal(t, tst.pred, opts[0])
	}
}
