package bucket

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/bucket/gcs"
	"github.com/grafana/loki/v3/pkg/storage/bucket/s3"
)

func TestConfigWithNamedStores_DisableRetries(t *testing.T) {
	cfg := ConfigWithNamedStores{
		Config: Config{
			S3:  s3.Config{MaxRetries: 10},
			GCS: gcs.Config{MaxRetries: 10},
		},
		NamedStores: NamedStores{
			S3: map[string]NamedS3StorageConfig{
				"named-s3": {MaxRetries: 10},
			},
			GCS: map[string]NamedGCSStorageConfig{
				"named-gcs": {MaxRetries: 10},
			},
		},
	}
	require.NoError(t, cfg.NamedStores.populateStoreType())

	require.NoError(t, cfg.DisableRetries(S3))
	require.Equal(t, 1, cfg.S3.MaxRetries)

	require.NoError(t, cfg.DisableRetries(GCS))
	require.Equal(t, 1, cfg.GCS.MaxRetries)

	require.NoError(t, cfg.DisableRetries("named-s3"))
	require.Equal(t, 1, cfg.NamedStores.S3["named-s3"].MaxRetries)

	require.NoError(t, cfg.DisableRetries("named-gcs"))
	require.Equal(t, 1, cfg.NamedStores.GCS["named-gcs"].MaxRetries)
}

func TestNamedStores_populateStoreType(t *testing.T) {
	t.Run("found duplicates", func(t *testing.T) {
		ns := NamedStores{
			S3: map[string]NamedS3StorageConfig{
				"store-1": {},
				"store-2": {},
			},
			GCS: map[string]NamedGCSStorageConfig{
				"store-1": {},
			},
		}

		err := ns.populateStoreType()
		require.ErrorContains(t, err, `named store "store-1" is already defined under`)

	})

	t.Run("illegal store name", func(t *testing.T) {
		ns := NamedStores{
			GCS: map[string]NamedGCSStorageConfig{
				"s3": {},
			},
		}

		err := ns.populateStoreType()
		require.ErrorContains(t, err, `named store "s3" should not match with the name of a predefined storage type`)

	})

	t.Run("lookup populated entries", func(t *testing.T) {
		ns := NamedStores{
			S3: map[string]NamedS3StorageConfig{
				"store-1": {},
				"store-2": {},
			},
			GCS: map[string]NamedGCSStorageConfig{
				"store-3": {},
			},
		}

		err := ns.populateStoreType()
		require.NoError(t, err)

		storeType, ok := ns.LookupStoreType("store-1")
		require.True(t, ok)
		require.Equal(t, S3, storeType)

		storeType, ok = ns.LookupStoreType("store-2")
		require.True(t, ok)
		require.Equal(t, S3, storeType)

		storeType, ok = ns.LookupStoreType("store-3")
		require.True(t, ok)
		require.Equal(t, GCS, storeType)

		_, ok = ns.LookupStoreType("store-4")
		require.False(t, ok)
	})
}

func TestNamedStores_OverrideConfig(t *testing.T) {
	namedStoreCfg := NamedStores{
		GCS: map[string]NamedGCSStorageConfig{
			"store-1": {
				BucketName:      "bar",
				ChunkBufferSize: 100,
			},
			"store-2": {
				BucketName: "baz",
			},
		},
	}
	require.NoError(t, namedStoreCfg.populateStoreType())

	storeCfg := Config{
		GCS: gcs.Config{
			BucketName: "foo",
		},
	}
	err := namedStoreCfg.OverrideConfig(&storeCfg, "store-1")
	require.NoError(t, err)
	require.Equal(t, "bar", storeCfg.GCS.BucketName)
	require.Equal(t, 100, storeCfg.GCS.ChunkBufferSize)
}
