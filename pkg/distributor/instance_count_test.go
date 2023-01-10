package distributor

import (
	"testing"
	"time"

	"github.com/grafana/dskit/ring"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
)

type mockDelegateEx struct {
	invocations map[string]int
}

func (m *mockDelegateEx) OnRingInstanceHeartbeat(lifecycler *ring.BasicLifecycler, ringDesc *ring.Desc, instanceDesc *ring.InstanceDesc) {
	m.invocations["OnRingInstanceHeartbeat"]++
}

func (m *mockDelegateEx) OnRingInstanceRegister(lifecycler *ring.BasicLifecycler, ringDesc ring.Desc, instanceExists bool, instanceID string, instanceDesc ring.InstanceDesc) (ring.InstanceState, ring.Tokens) {
	m.invocations["OnRingInstanceHeartbeat"]++

	return ring.ACTIVE, ring.GenerateTokens(255, []uint32{})
}

func (m *mockDelegateEx) OnRingInstanceStopping(lifecycler *ring.BasicLifecycler) {
	m.invocations["OnRingInstanceStopping"]++
}

func (m *mockDelegateEx) OnRingInstanceTokens(lifecycler *ring.BasicLifecycler, tokens ring.Tokens) {
	m.invocations["OnRingInstanceTokens"]++
}

func TestEmbeddingHandler(t *testing.T) {
	cnt := atomic.NewUint32(1)
	otherDelegate := &mockDelegateEx{invocations: map[string]int{}}

	healthyInstanceCheck := newHealthyInstanceDelegate(cnt, time.Second, otherDelegate)

	// require.Equal(t, 0, otherDelegate.invocations["OnRingInstanceHeartbeat"])
	// healthyInstanceCheck.OnRingInstanceHeartbeat(nil, &ring.Desc{}, &ring.InstanceDesc{})
	// require.Equal(t, 1, otherDelegate.invocations["OnRingInstanceHeartbeat"])

	require.Equal(t, 0, otherDelegate.invocations["OnRingInstanceStopping"])
	healthyInstanceCheck.OnRingInstanceStopping(nil)
	require.Equal(t, 1, otherDelegate.invocations["OnRingInstanceStopping"])
}
