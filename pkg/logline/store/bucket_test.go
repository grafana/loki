package store

import (
	"context"
	"testing"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/bucket"
	"github.com/grafana/loki/v3/pkg/storage/config"
)

// TestNewBucket_ResolvesNamedStoreBackend verifies that a schema whose
// object_store points at a named store (e.g. "my-store" -> filesystem)
// resolves to the underlying backend. This depends on the caller having
// validated the object-store config so that the NamedStores reverse-lookup
// map is populated.
func TestNewBucket_ResolvesNamedStoreBackend(t *testing.T) {
	const namedStore = "my-store"

	dir := t.TempDir()

	schemaCfg := config.SchemaConfig{Configs: []config.PeriodConfig{{
		From:       config.DayTime{Time: 0},
		Schema:     "v13",
		IndexType:  "tsdb",
		ObjectType: namedStore,
	}}}

	var objectStoreCfg bucket.ConfigWithNamedStores
	objectStoreCfg.NamedStores.Filesystem = map[string]bucket.NamedFilesystemStorageConfig{
		namedStore: {Directory: dir},
	}
	require.NoError(t, objectStoreCfg.Validate())

	bkt, err := NewBucket(context.Background(), schemaCfg, objectStoreCfg, "logline/", "", log.NewNopLogger())
	require.NoError(t, err)
	require.NotNil(t, bkt)
}

// TestNewBucket_UnknownBackend ensures a schema referencing a backend that is
// neither a predefined backend nor a configured named store surfaces the
// underlying error.
func TestNewBucket_UnknownBackend(t *testing.T) {
	schemaCfg := config.SchemaConfig{Configs: []config.PeriodConfig{{
		From:       config.DayTime{Time: 0},
		Schema:     "v13",
		IndexType:  "tsdb",
		ObjectType: "not-a-real-store",
	}}}

	var objectStoreCfg bucket.ConfigWithNamedStores
	require.NoError(t, objectStoreCfg.Validate())

	_, err := NewBucket(context.Background(), schemaCfg, objectStoreCfg, "logline/", "", log.NewNopLogger())
	require.Error(t, err)
}
