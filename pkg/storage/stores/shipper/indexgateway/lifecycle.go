package indexgateway

import (
	"github.com/grafana/dskit/ring"
)

func (rm *RingManager) OnRingInstanceRegister(_ *ring.BasicLifecycler, ringDesc ring.Desc, instanceExists bool, _ string, instanceDesc ring.InstanceDesc) (ring.InstanceState, ring.Tokens) {
	// When we initialize the index gateway instance in the ring we want to start from
	// a clean situation, so whatever is the state we set it JOINING, while we keep existing
	// tokens (if any) or the ones loaded from file.
	var tokens []uint32
	if instanceExists {
		tokens = instanceDesc.GetTokens()
	}

	takenTokens := ringDesc.GetTokens()
	gen := ring.NewRandomTokenGenerator()
	newTokens := gen.GenerateTokens(ringNumTokens-len(tokens), takenTokens)

	// Tokens sorting will be enforced by the parent caller.
	tokens = append(tokens, newTokens...)

	return ring.JOINING, tokens
}

func (rm *RingManager) OnRingInstanceTokens(_ *ring.BasicLifecycler, _ ring.Tokens) {}
func (rm *RingManager) OnRingInstanceStopping(_ *ring.BasicLifecycler)              {}
func (rm *RingManager) OnRingInstanceHeartbeat(_ *ring.BasicLifecycler, _ *ring.Desc, _ *ring.InstanceDesc) {
}
