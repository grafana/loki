package indexgateway

import "github.com/grafana/dskit/ring"

func (g *Gateway) OnRingInstanceRegister(_ *ring.BasicLifecycler, ringDesc ring.Desc, instanceExists bool, instanceID string, instanceDesc ring.InstanceDesc) (ring.InstanceState, ring.Tokens) {
	return ring.ACTIVE, ring.GenerateTokens(ringNumTokens, []uint32{})
}

func (g *Gateway) OnRingInstanceTokens(_ *ring.BasicLifecycler, _ ring.Tokens) {}
func (g *Gateway) OnRingInstanceStopping(_ *ring.BasicLifecycler)              {}
func (g *Gateway) OnRingInstanceHeartbeat(_ *ring.BasicLifecycler, _ *ring.Desc, _ *ring.InstanceDesc) {
}
