package resultscache

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/util/httpreq"
)

func TestPipelineWrapperKeygen(t *testing.T) {
	kg := &stubKeygen{key: "cache-key"}
	keygen := NewPipelineWrapperKeygen(kg)

	t.Run("it does nothing if pipeline wrappers aren't disabled", func(t *testing.T) {
		key := keygen.GenerateCacheKey(context.Background(), "", nil)
		require.Equal(t, "cache-key", key)
	})

	t.Run("it changes the key when pipeline wrappers are disabled", func(t *testing.T) {
		ctx := httpreq.InjectHeader(context.Background(), httpreq.LokiDisablePipelineWrappersHeader, "true")
		key := keygen.GenerateCacheKey(ctx, "", nil)
		require.Equal(t, "pipeline-disabled:cache-key", key)
	})
}

type stubKeygen struct {
	key string
}

func (k *stubKeygen) GenerateCacheKey(_ context.Context, _ string, _ Request) string {
	return k.key
}
