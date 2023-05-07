package distributor

import (
	"time"

	"github.com/grafana/dskit/ring"
	"go.uber.org/atomic"
)

// healthyInstanceDelegate counts the number of healthy instances that are part of the ring
// and stores the count to the provided atomic integer. Used here to count the number of
// distributors in the ring to determine how to enforce rate limiting.
type healthyInstanceDelegate struct {
	count            *atomic.Uint32
	heartbeatTimeout time.Duration

	ring.BasicLifecyclerDelegate
}

func newHealthyInstanceDelegate(count *atomic.Uint32, heartbeatTimeout time.Duration, next ring.BasicLifecyclerDelegate) *healthyInstanceDelegate {
	return &healthyInstanceDelegate{count: count, heartbeatTimeout: heartbeatTimeout, BasicLifecyclerDelegate: next}
}

// OnRingInstanceHeartbeat implements the ring.BasicLifecyclerDelegate interface
func (d *healthyInstanceDelegate) OnRingInstanceHeartbeat(lifecycler *ring.BasicLifecycler, ringDesc *ring.Desc, instanceDesc *ring.InstanceDesc) {
	activeMembers := uint32(0)
	now := time.Now()

	for _, instance := range ringDesc.Ingesters {
		if ring.ACTIVE == instance.State && instance.IsHeartbeatHealthy(d.heartbeatTimeout, now) {
			activeMembers++
		}
	}

	d.count.Store(activeMembers)
	d.BasicLifecyclerDelegate.OnRingInstanceHeartbeat(lifecycler, ringDesc, instanceDesc)
}
