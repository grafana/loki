package analytics

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/kv/codec"
	"github.com/grafana/dskit/kv/memberlist"
	"github.com/grafana/dskit/services"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
)

type dnsProviderMock struct {
	resolved []string
}

func (p *dnsProviderMock) Resolve(_ context.Context, addrs []string) error {
	p.resolved = addrs
	return nil
}

func (p dnsProviderMock) Addresses() []string {
	return p.resolved
}

func createMemberlist(t *testing.T, port, memberID int) *memberlist.KV {
	t.Helper()
	var cfg memberlist.KVConfig
	flagext.DefaultValues(&cfg)
	cfg.TCPTransport = memberlist.TCPTransportConfig{
		BindAddrs: []string{"127.0.0.1"},
		BindPort:  0,
	}
	cfg.GossipInterval = 100 * time.Millisecond
	cfg.GossipNodes = 3
	cfg.PushPullInterval = 5 * time.Second
	cfg.NodeName = fmt.Sprintf("Member-%d", memberID)
	cfg.Codecs = []codec.Codec{JSONCodec}

	mkv := memberlist.NewKV(cfg, log.NewNopLogger(), &dnsProviderMock{}, nil)
	require.NoError(t, services.StartAndAwaitRunning(context.Background(), mkv))
	if port != 0 {
		_, err := mkv.JoinMembers([]string{fmt.Sprintf("127.0.0.1:%d", port)})
		require.NoError(t, err, "%s failed to join the cluster: %v", memberID, err)
	}
	t.Cleanup(func() {
		_ = services.StopAndAwaitTerminated(context.TODO(), mkv)
	})
	return mkv
}

func Test_Memberlist(t *testing.T) {
	stabilityCheckInterval = time.Second

	objectClient, err := local.NewFSObjectClient(local.FSConfig{
		Directory: t.TempDir(),
	})
	require.NoError(t, err)
	result := make(chan *ClusterSeed, 10)

	// create a first memberlist to get a valid listening port.
	initMKV := createMemberlist(t, 0, -1)

	for i := 0; i < 10; i++ {
		go func(i int) {
			leader, err := NewReporter(Config{
				Leader:  true,
				Enabled: true,
			}, kv.Config{
				Store: "memberlist",
				StoreConfig: kv.StoreConfig{
					MemberlistKV: func() (*memberlist.KV, error) {
						return createMemberlist(t, initMKV.GetListeningPort(), i), nil
					},
				},
			}, objectClient, log.NewLogfmtLogger(os.Stdout), nil)
			require.NoError(t, err)
			leader.init(context.Background())
			result <- leader.cluster
		}(i)
	}

	var UID []string
	for i := 0; i < 10; i++ {
		cluster := <-result
		require.NotNil(t, cluster)
		UID = append(UID, cluster.UID)
	}
	first := UID[0]
	for _, uid := range UID {
		require.Equal(t, first, uid)
	}
}
