package audit

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/go-kit/log"
	"github.com/grafana/loki/v3/pkg/compactor"
	"github.com/grafana/loki/v3/pkg/compactor/retention"
	"github.com/grafana/loki/v3/pkg/storage"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/gcp"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/stretchr/testify/require"
)

func TestGetObjectClient(t *testing.T) {
	objClient, err := GetObjectClient(Config{
		StorageConfig: storage.Config{},
		SchemaConfig: config.SchemaConfig{
			Configs: []config.PeriodConfig{
				{
					ObjectType: "gcs",
				},
			},
		},
	})

	require.NoError(t, err)
	require.Equal(t, reflect.TypeOf(&gcp.GCSObjectClient{}), reflect.TypeOf(objClient))
}

type testObjClient struct {
	client.ObjectClient
}

func (t testObjClient) ObjectExists(ctx context.Context, object string) (bool, error) {
	if strings.Contains(object, "missing") {
		return false, nil
	}
	return true, nil
}

type testCompactedIdx struct {
	compactor.CompactedIndex

	chunks []retention.ChunkEntry
}

func (t testCompactedIdx) ForEachChunk(ctx context.Context, f retention.ChunkEntryCallback) error {
	for _, chunk := range t.chunks {
		if _, err := f(chunk); err != nil {
			return err
		}
	}
	return nil
}

func TestAuditIndex(t *testing.T) {
	ctx := context.Background()
	objClient := testObjClient{}
	compactedIdx := testCompactedIdx{
		chunks: []retention.ChunkEntry{
			{ChunkRef: retention.ChunkRef{ChunkID: []byte("found-1")}},
			{ChunkRef: retention.ChunkRef{ChunkID: []byte("found-2")}},
			{ChunkRef: retention.ChunkRef{ChunkID: []byte("found-3")}},
			{ChunkRef: retention.ChunkRef{ChunkID: []byte("found-4")}},
			{ChunkRef: retention.ChunkRef{ChunkID: []byte("missing-1")}},
		},
	}
	logger := log.NewNopLogger()
	found, missing, err := ValidateCompactedIndex(ctx, objClient, compactedIdx, 1, logger)
	require.NoError(t, err)
	require.Equal(t, 4, found)
	require.Equal(t, 1, missing)
}
