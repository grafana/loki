package bucket

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/bucket/gcs"
)

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
