package client

import (
	"context"
	"maps"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"

	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/testkafka"
)

// recordAPIVersions returns the versions of every ApiVersions request the cluster receives.
func recordAPIVersions(cluster *kfake.Cluster) func() []int16 {
	var (
		mu       sync.Mutex
		versions []int16
	)
	cluster.ControlKey(kmsg.ApiVersions.Int16(), func(req kmsg.Request) (kmsg.Response, error, bool) {
		cluster.KeepControl()
		mu.Lock()
		versions = append(versions, req.GetVersion())
		mu.Unlock()
		return nil, nil, false
	})
	return func() []int16 {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(versions)
	}
}

// TestClientsCapAPIVersions checks that the reader and writer clients send ApiVersions
// at the configured cap. The lower cap proves the option is wired in, since
// franz-go v1.21.x sends v4 anyway.
func TestClientsCapAPIVersions(t *testing.T) {
	tests := []struct {
		name       string
		maxVersion int
		want       int16
	}{
		{name: "default", maxVersion: kafka.DefaultAPIVersionsMaxVersion, want: kafka.DefaultAPIVersionsMaxVersion},
		{name: "lower cap", maxVersion: 3, want: 3},
	}

	newClients := map[string]func(cfg kafka.Config) (*kgo.Client, error){
		"reader": func(cfg kafka.Config) (*kgo.Client, error) {
			return NewReaderClient("test-reader", cfg, log.NewNopLogger(), prometheus.NewRegistry())
		},
		"writer": func(cfg kafka.Config) (*kgo.Client, error) {
			return NewWriterClient("test-writer", cfg, log.NewNopLogger(), prometheus.NewRegistry())
		},
	}

	for _, tt := range tests {
		for clientName, newClient := range newClients {
			t.Run(tt.name+"/"+clientName, func(t *testing.T) {
				cluster, cfg := testkafka.CreateCluster(t, 1, "test")
				seen := recordAPIVersions(cluster)

				cfg.APIVersionsMaxVersion = tt.maxVersion
				client, err := newClient(cfg)
				require.NoError(t, err)
				t.Cleanup(client.Close)

				require.NoError(t, client.Ping(context.Background()))

				versions := seen()
				require.NotEmpty(t, versions)
				for _, v := range versions {
					require.Equal(t, tt.want, v)
				}
			})
		}
	}
}

// recordRequestVersions returns the highest version of each request type the cluster receives.
func recordRequestVersions(cluster *kfake.Cluster) func() map[int16]int16 {
	var (
		mu       sync.Mutex
		versions = map[int16]int16{}
	)
	cluster.Control(func(req kmsg.Request) (kmsg.Response, error, bool) {
		cluster.KeepControl()
		mu.Lock()
		versions[req.Key()] = max(versions[req.Key()], req.GetVersion())
		mu.Unlock()
		return nil, nil, false
	})
	return func() map[int16]int16 {
		mu.Lock()
		defer mu.Unlock()
		return maps.Clone(versions)
	}
}

// TestAPIVersionsCapChangesNoOtherRequest checks that the cap changes only the
// ApiVersions request: every other request uses the same version as without the cap.
func TestAPIVersionsCapChangesNoOtherRequest(t *testing.T) {
	requestVersions := func(t *testing.T, maxVersion int) map[int16]int16 {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		t.Cleanup(cancel)

		cluster, cfg := testkafka.CreateCluster(t, 1, "test")
		seen := recordRequestVersions(cluster)
		cfg.APIVersionsMaxVersion = maxVersion

		writer, err := NewWriterClient("test-writer", cfg, log.NewNopLogger(), prometheus.NewRegistry())
		require.NoError(t, err)
		t.Cleanup(writer.Close)
		require.NoError(t, writer.ProduceSync(ctx, &kgo.Record{Topic: "test", Value: []byte("x")}).FirstErr())

		reader, err := NewReaderClient("test-reader", cfg, log.NewNopLogger(), prometheus.NewRegistry(),
			kgo.ConsumePartitions(map[string]map[int32]kgo.Offset{"test": {0: kgo.NewOffset().AtStart()}}))
		require.NoError(t, err)
		t.Cleanup(reader.Close)
		require.Equal(t, 1, reader.PollFetches(ctx).NumRecords())

		return seen()
	}

	uncapped := requestVersions(t, 0)
	capped := requestVersions(t, 3)
	require.Equal(t, int16(3), capped[kmsg.ApiVersions.Int16()])

	delete(uncapped, kmsg.ApiVersions.Int16())
	delete(capped, kmsg.ApiVersions.Int16())
	require.NotEmpty(t, capped)
	require.Equal(t, uncapped, capped)
}
