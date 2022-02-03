package usagestats

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/kv"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/storage/chunk/local"
	"github.com/grafana/loki/pkg/storage/chunk/storage"
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
			}, objectClient, log.NewLogfmtLogger(os.Stdout), prometheus.NewPedanticRegistry(), true)
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

func Test_ReportLoop(t *testing.T) {
	// stub intervals
	reportCheckInterval = 500 * time.Millisecond
	reportInterval = time.Second

	objectClient, err := storage.NewObjectClient(storage.StorageTypeFileSystem, storage.Config{
		FSConfig: local.FSConfig{
			Directory: t.TempDir(),
		},
	}, storage.NewClientMetrics())
	require.NoError(t, err)

	r, err := NewReporter(kv.Config{
		Store: "inmemory",
	}, objectClient, log.NewLogfmtLogger(os.Stdout), prometheus.NewPedanticRegistry(), true)
	require.NoError(t, err)

	require.NoError(t, r.initLeader(context.Background()))
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		<-time.After(10 * time.Second)
		cancel()
	}()
	require.Equal(t, context.Canceled, r.running(ctx))
}

func Test_NextReport(t *testing.T) {
	fixtures := map[string]struct {
		interval  time.Duration
		createdAt time.Time
		now       time.Time

		next time.Time
	}{
		"createdAt aligned with interval and now": {
			interval:  1 * time.Hour,
			createdAt: time.Unix(0, time.Hour.Nanoseconds()),
			now:       time.Unix(0, 2*time.Hour.Nanoseconds()),
			next:      time.Unix(0, 2*time.Hour.Nanoseconds()),
		},
		"createdAt aligned with interval": {
			interval:  1 * time.Hour,
			createdAt: time.Unix(0, time.Hour.Nanoseconds()),
			now:       time.Unix(0, 2*time.Hour.Nanoseconds()+1),
			next:      time.Unix(0, 3*time.Hour.Nanoseconds()),
		},
		"createdAt not aligned": {
			interval:  1 * time.Hour,
			createdAt: time.Unix(0, time.Hour.Nanoseconds()+18*time.Minute.Nanoseconds()+20*time.Millisecond.Nanoseconds()),
			now:       time.Unix(0, 2*time.Hour.Nanoseconds()+1),
			next:      time.Unix(0, 2*time.Hour.Nanoseconds()+18*time.Minute.Nanoseconds()+20*time.Millisecond.Nanoseconds()),
		},
	}
	for name, f := range fixtures {
		t.Run(name, func(t *testing.T) {
			next := nextReport(f.interval, f.createdAt, f.now)
			require.Equal(t, f.next, next)
		})
	}
}
