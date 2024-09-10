package ingester

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-kit/log"
	gokitlog "github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/kv/consul"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/grafana/loki/v3/pkg/ingester-rf1/metastore/metastorepb"
	"github.com/grafana/loki/v3/pkg/ingester-rf1/objstore"
	"github.com/grafana/loki/v3/pkg/util/test"
)

func TestPreparePartitionDownscaleHandler(t *testing.T) {
	cfg := defaultIngesterTestConfig(t)
	// start ingester.
	storage, err := objstore.NewTestStorage(t)
	require.NoError(t, err)
	ing, err := New(cfg,
		NewConsumerFactory(NewTestMetastore(), storage, cfg.FlushInterval, cfg.FlushSize, gokitlog.NewNopLogger(), prometheus.NewRegistry()),
		gokitlog.NewNopLogger(), "test", prometheus.NewRegistry())
	require.NoError(t, err)
	err = services.StartAndAwaitRunning(context.Background(), ing)
	require.NoError(t, err)

	t.Run("get state", func(t *testing.T) {
		w := httptest.NewRecorder()
		ing.PreparePartitionDownscaleHandler(w, httptest.NewRequest("GET", "/", nil))
		require.Equal(t, http.StatusOK, w.Code)
		require.Equal(t, "{\"timestamp\":0}", w.Body.String())
	})
	t.Run("prepare shutdown pending", func(t *testing.T) {
		w := httptest.NewRecorder()
		ing.PreparePartitionDownscaleHandler(w, httptest.NewRequest("POST", "/", nil))
		require.Equal(t, http.StatusConflict, w.Code)
	})
	t.Run("prepare shutdown and cancel", func(t *testing.T) {
		w := httptest.NewRecorder()
		test.Poll(t, 5*time.Second, ring.PartitionActive, func() interface{} {
			return getState(t, cfg)
		})
		ing.PreparePartitionDownscaleHandler(w, httptest.NewRequest("POST", "/", nil))
		require.Equal(t, http.StatusOK, w.Code)
		test.Poll(t, 5*time.Second, ring.PartitionInactive, func() interface{} {
			return getState(t, cfg)
		})
		w2 := httptest.NewRecorder()
		ing.PreparePartitionDownscaleHandler(w2, httptest.NewRequest("DELETE", "/", nil))
		require.Equal(t, http.StatusOK, w.Code)
		test.Poll(t, 5*time.Second, ring.PartitionActive, func() interface{} {
			return getState(t, cfg)
		})
	})
	require.NoError(t, services.StopAndAwaitTerminated(context.Background(), ing))
}

func getState(t *testing.T, cfg Config) ring.PartitionState {
	get, err := cfg.PartitionRingConfig.KVStore.Mock.Get(context.Background(), PartitionRingName+"-key")
	require.NoError(t, err)

	ringDesc := ring.GetOrCreatePartitionRingDesc(get)
	return ringDesc.Partitions[0].State
}

// nolint
func defaultIngesterTestConfig(t testing.TB) Config {
	kvRing, closer := consul.NewInMemoryClient(ring.GetCodec(), log.NewNopLogger(), nil)
	t.Cleanup(func() { require.NoError(t, closer.Close()) })

	kvPartitionRing, closerPartitionRing := consul.NewInMemoryClient(ring.GetPartitionRingCodec(), log.NewNopLogger(), nil)
	t.Cleanup(func() { require.NoError(t, closerPartitionRing.Close()) })

	cfg := Config{}
	flagext.DefaultValues(&cfg)

	cfg.LifecyclerConfig.RingConfig.KVStore.Mock = kvRing
	cfg.PartitionRingConfig.KVStore.Mock = kvPartitionRing
	cfg.PartitionRingConfig.MinOwnersCount = 1
	cfg.PartitionRingConfig.MinOwnersDuration = 0
	cfg.LifecyclerConfig.RingConfig.ReplicationFactor = 1
	cfg.LifecyclerConfig.NumTokens = 1
	cfg.LifecyclerConfig.ListenPort = 0
	cfg.LifecyclerConfig.Addr = "localhost"
	cfg.LifecyclerConfig.ID = "localhost"
	cfg.LifecyclerConfig.FinalSleep = 0
	cfg.LifecyclerConfig.MinReadyDuration = 0

	return cfg
}

func TestExtractIngesterPartitionID(t *testing.T) {
	tests := []struct {
		name       string
		ingesterID string
		want       int32
		wantErr    bool
	}{
		{
			name:       "Valid ingester ID",
			ingesterID: "ingester-5",
			want:       5,
			wantErr:    false,
		},
		{
			name:       "Local ingester ID",
			ingesterID: "ingester-local",
			want:       0,
			wantErr:    false,
		},
		{
			name:       "Invalid ingester ID format",
			ingesterID: "invalid-format",
			want:       0,
			wantErr:    true,
		},
		{
			name:       "Invalid sequence number",
			ingesterID: "ingester-abc",
			want:       0,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractIngesterPartitionID(tt.ingesterID)
			if (err != nil) != tt.wantErr {
				t.Errorf("extractIngesterPartitionID() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("extractIngesterPartitionID() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestMetastore is a simple in-memory metastore for testing
type TestMetastore struct {
	blocks map[string][]*metastorepb.BlockMeta
}

func NewTestMetastore() *TestMetastore {
	return &TestMetastore{blocks: make(map[string][]*metastorepb.BlockMeta)}
}

func (m *TestMetastore) ListBlocksForQuery(_ context.Context, req *metastorepb.ListBlocksForQueryRequest, _ ...grpc.CallOption) (*metastorepb.ListBlocksForQueryResponse, error) {
	blocks := m.blocks[req.TenantId]
	var result []*metastorepb.BlockMeta
	for _, block := range blocks {
		if block.MinTime <= req.EndTime && block.MaxTime >= req.StartTime {
			result = append(result, block)
		}
	}
	return &metastorepb.ListBlocksForQueryResponse{Blocks: result}, nil
}

func (m *TestMetastore) AddBlock(_ context.Context, in *metastorepb.AddBlockRequest, _ ...grpc.CallOption) (*metastorepb.AddBlockResponse, error) {
	for _, stream := range in.Block.TenantStreams {
		m.blocks[stream.TenantId] = append(m.blocks[stream.TenantId], in.Block)
	}
	return &metastorepb.AddBlockResponse{}, nil
}
