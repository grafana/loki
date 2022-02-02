package usagestats

import (
	"context"
	"os"
	"testing"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/loki/pkg/storage/chunk/local"
	"github.com/grafana/loki/pkg/storage/chunk/storage"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func Test_LeaderElection(t *testing.T) {
	result := make(chan *ClusterSeed, 10)
	objectClient, err := storage.NewObjectClient(storage.StorageTypeFileSystem, storage.Config{
		FSConfig: local.FSConfig{
			Directory: t.TempDir(),
		},
	}, storage.NewClientMetrics())
	require.NoError(t, err)
	for i := 0; i < 10; i++ {
		go func() {
			r, err := NewReporter(kv.Config{
				Store: "inmemory",
			}, objectClient, log.NewLogfmtLogger(os.Stdout), prometheus.NewPedanticRegistry())
			require.NoError(t, err)
			require.NoError(t, r.initLeader(context.Background()))
			result <- r.cluster
		}()
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
	kvClient, err := kv.NewClient(kv.Config{Store: "inmemory"}, JSONCodec, prometheus.DefaultRegisterer, log.NewLogfmtLogger(os.Stdout))
	require.NoError(t, err)
	// verify that the ID found is also correctly stored in the kv store and not overridden by another leader.
	data, err := kvClient.Get(context.Background(), seedKey)
	require.NoError(t, err)
	require.Equal(t, data.(*ClusterSeed).UID, first)
}
