package bloomgateway

import (
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
)

type provider struct {
	mu        sync.Mutex
	addresses []string
}

func (p *provider) Addresses() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.addresses
}

func (p *provider) UpdateAddresses(newAddresses []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.addresses = newAddresses
}

func TestJumpHashClientPool_UpdateLoop(t *testing.T) {
	interval := 100 * time.Millisecond

	provider := &provider{}
	provider.UpdateAddresses([]string{"localhost:9095"})
	pool := NewJumpHashClientPool(nil, provider, interval, log.NewNopLogger())
	require.Len(t, pool.Addrs(), 1)
	require.Equal(t, "127.0.0.1:9095", pool.Addrs()[0].String())

	// update address list
	provider.UpdateAddresses([]string{"localhost:9095", "localhost:9096"})
	// wait refresh interval
	time.Sleep(2 * interval)
	// pool has been updated
	require.Len(t, pool.Addrs(), 2)
	require.Equal(t, "127.0.0.1:9095", pool.Addrs()[0].String())
	require.Equal(t, "127.0.0.1:9096", pool.Addrs()[1].String())
}
