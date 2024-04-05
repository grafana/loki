package bloomgateway

import (
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
)

type provider struct {
	addresses []string
}

func (p *provider) Addresses() []string {
	return p.addresses
}

func TestJumpHashClientPool_UpdateLoop(t *testing.T) {
	interval := 100 * time.Millisecond

	provider := &provider{[]string{"localhost:9095"}}
	pool := NewJumpHashClientPool(nil, provider, interval, log.NewNopLogger())
	require.Len(t, pool.Addrs(), 1)
	require.Equal(t, "127.0.0.1:9095", pool.Addrs()[0].String())

	// update address list
	provider.addresses = []string{"localhost:9095", "localhost:9096"}
	// wait refresh interval
	time.Sleep(2 * interval)
	// pool has been updated
	require.Len(t, pool.Addrs(), 2)
	require.Equal(t, "127.0.0.1:9095", pool.Addrs()[0].String())
	require.Equal(t, "127.0.0.1:9096", pool.Addrs()[1].String())
}
