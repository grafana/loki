package storage

import (
	"context"
	"os"
	"path"
	"strings"
	"testing"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/grafana/dskit/flagext"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	lokiaws "github.com/grafana/loki/v3/pkg/storage/chunk/client/aws"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper"
	"github.com/grafana/loki/v3/pkg/storage/types"
	"github.com/grafana/loki/v3/pkg/util/constants"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/validation"
)

func TestNamedStores(t *testing.T) {
	tempDir := t.TempDir()

	shipperCfg := indexshipper.Config{}
	flagext.DefaultValues(&shipperCfg)
	shipperCfg.ActiveIndexDirectory = path.Join(tempDir, "index")
	shipperCfg.CacheLocation = path.Join(tempDir, "tsdb-cache")
	shipperCfg.Mode = indexshipper.ModeReadWrite

	cfg := Config{
		NamedStores: NamedStores{
			Filesystem: map[string]NamedFSConfig{
				"named-store": {Directory: path.Join(tempDir, "named-store")},
			},
		},
		FSConfig: local.FSConfig{
			Directory: path.Join(tempDir, "default"),
		},
		TSDBShipperConfig: shipperCfg,
	}
	require.NoError(t, cfg.NamedStores.Validate())

	schemaConfig := config.SchemaConfig{
		Configs: []config.PeriodConfig{
			{
				From:       config.DayTime{Time: timeToModelTime(parseDate("2026-01-01"))},
				IndexType:  "tsdb",
				ObjectType: "named-store",
				Schema:     "v13",
				IndexTables: config.IndexPeriodicTableConfig{
					PeriodicTableConfig: config.PeriodicTableConfig{
						Prefix: "index_",
						Period: 24 * time.Hour,
					}},
			},
		},
	}

	limits, err := validation.NewOverrides(validation.Limits{}, nil)
	require.NoError(t, err)

	t.Run("period config referring to configured named store", func(t *testing.T) {
		err := os.Remove(cfg.NamedStores.Filesystem["named-store"].Directory)
		if err != nil {
			require.True(t, os.IsNotExist(err))
		}

		err = os.Remove(cfg.FSConfig.Directory)
		if err != nil {
			require.True(t, os.IsNotExist(err))
		}

		store, err := NewStore(cfg, config.ChunkStoreConfig{}, schemaConfig, limits, cm, nil, util_log.Logger, constants.Loki)
		require.NoError(t, err)
		defer store.Stop()

		// FSObjectClient creates the configured dir on init, ensure that correct cfg is picked by checking for it.
		_, err = os.Stat(cfg.NamedStores.Filesystem["named-store"].Directory)
		require.NoError(t, err)

		// dir specified in StorageConfig/FSConfig should not be created as we are not referring to it.
		_, err = os.Stat(cfg.FSConfig.Directory)
		require.True(t, os.IsNotExist(err))

	})

	t.Run("period config referring to unrecognized store", func(t *testing.T) {
		schemaConfig := schemaConfig
		schemaConfig.Configs[0].ObjectType = "not-found"
		_, err := NewStore(cfg, config.ChunkStoreConfig{}, schemaConfig, limits, cm, nil, util_log.Logger, constants.Loki)
		require.Error(t, err)
		require.Contains(t, err.Error(), "unrecognized chunk client type not-found, choose one of:")
	})
}

func TestNamedStores_populateStoreType(t *testing.T) {
	t.Run("found duplicates", func(t *testing.T) {
		ns := NamedStores{
			AWS: map[string]NamedAWSStorageConfig{
				"store-1": {},
				"store-2": {},
			},
			GCS: map[string]NamedGCSConfig{
				"store-1": {},
			},
		}

		err := ns.populateStoreType()
		require.ErrorContains(t, err, `named store "store-1" is already defined under`)

	})

	t.Run("illegal store name", func(t *testing.T) {
		ns := NamedStores{
			GCS: map[string]NamedGCSConfig{
				"aws": {},
			},
		}

		err := ns.populateStoreType()
		require.ErrorContains(t, err, `named store "aws" should not match with the name of a predefined storage type`)

	})

	t.Run("lookup populated entries", func(t *testing.T) {
		ns := NamedStores{
			AWS: map[string]NamedAWSStorageConfig{
				"store-1": {},
				"store-2": {},
			},
			GCS: map[string]NamedGCSConfig{
				"store-3": {},
			},
		}

		err := ns.populateStoreType()
		require.NoError(t, err)

		storeType, ok := ns.storeType["store-1"]
		assert.True(t, ok)
		assert.Equal(t, types.StorageTypeAWS, storeType)

		storeType, ok = ns.storeType["store-2"]
		assert.True(t, ok)
		assert.Equal(t, types.StorageTypeAWS, storeType)

		storeType, ok = ns.storeType["store-3"]
		assert.True(t, ok)
		assert.Equal(t, types.StorageTypeGCS, storeType)

		_, ok = ns.storeType["store-4"]
		assert.False(t, ok)
	})
}

type listObjectKeysS3Client struct {
	*s3.Client
	keys []string
}

func (c listObjectKeysS3Client) ListObjectsV2(context.Context, *s3.ListObjectsV2Input, ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	modifiedAt := time.Unix(0, 0)
	contents := make([]s3types.Object, 0, len(c.keys))
	for _, key := range c.keys {
		contents = append(contents, s3types.Object{
			Key:          awssdk.String(key),
			LastModified: awssdk.Time(modifiedAt),
		})
	}

	return &s3.ListObjectsV2Output{
		Contents:    contents,
		IsTruncated: awssdk.Bool(false),
	}, nil
}

func listedS3ObjectKeys(t *testing.T, objectClient client.ObjectClient, keys ...string) []string {
	t.Helper()

	s3ObjectClient, ok := objectClient.(*lokiaws.S3ObjectClient)
	require.True(t, ok)
	s3ObjectClient.S3 = listObjectKeysS3Client{keys: keys}

	objects, _, err := s3ObjectClient.List(context.Background(), "", "")
	require.NoError(t, err)

	listedKeys := make([]string, 0, len(objects))
	for _, object := range objects {
		listedKeys = append(listedKeys, object.Key)
	}

	return listedKeys
}

func testS3Config() lokiaws.S3Config {
	var cfg lokiaws.S3Config
	flagext.DefaultValues(&cfg)
	cfg.BucketNames = "bucket"
	cfg.ChunkDelimiter = "-"
	cfg.AccessKeyID = "test-key"
	cfg.SecretAccessKey = flagext.SecretWithValue("test-secret")
	return cfg
}

func TestNewIndexObjectClientDoesNotApplyS3ChunkDelimiter(t *testing.T) {
	const indexKey = "tsdb_index_20332/1779799681971192955-compactor-1779659125419-1779761781223-4eafaf2b.tsdb"
	colonizedIndexKey := strings.ReplaceAll(indexKey, "-", ":")

	t.Run("default s3 storage", func(t *testing.T) {
		var cfg Config
		flagext.DefaultValues(&cfg)
		cfg.S3Config = testS3Config()

		chunkObjectClient, err := NewObjectClient(types.StorageTypeS3, "chunk-store", cfg, cm)
		require.NoError(t, err)
		require.Equal(t, []string{colonizedIndexKey}, listedS3ObjectKeys(t, chunkObjectClient, indexKey))

		indexObjectClient, err := newIndexObjectClient(types.StorageTypeS3, "index-store", cfg, cm)
		require.NoError(t, err)
		require.Equal(t, []string{indexKey}, listedS3ObjectKeys(t, indexObjectClient, indexKey))
		require.Equal(t, "-", cfg.S3Config.ChunkDelimiter)
	})

	t.Run("named aws storage", func(t *testing.T) {
		var cfg Config
		flagext.DefaultValues(&cfg)
		cfg.NamedStores = NamedStores{
			AWS: map[string]NamedAWSStorageConfig{
				"store-1": NamedAWSStorageConfig(testS3Config()),
			},
		}
		require.NoError(t, cfg.NamedStores.Validate())

		chunkObjectClient, err := NewObjectClient("store-1", "chunk-store", cfg, cm)
		require.NoError(t, err)
		require.Equal(t, []string{colonizedIndexKey}, listedS3ObjectKeys(t, chunkObjectClient, indexKey))

		indexObjectClient, err := newIndexObjectClient("store-1", "index-store", cfg, cm)
		require.NoError(t, err)
		require.Equal(t, []string{indexKey}, listedS3ObjectKeys(t, indexObjectClient, indexKey))
		require.Equal(t, "-", cfg.NamedStores.AWS["store-1"].ChunkDelimiter)
	})
}

func TestNewObjectClient_prefixing(t *testing.T) {
	newFSCfg := func(t *testing.T) Config {
		var cfg Config
		flagext.DefaultValues(&cfg)
		cfg.FSConfig = local.FSConfig{Directory: t.TempDir()}
		return cfg
	}

	t.Run("no prefix", func(t *testing.T) {
		cfg := newFSCfg(t)

		objectClient, err := NewObjectClient("filesystem", "test", cfg, cm)
		require.NoError(t, err)

		_, ok := objectClient.(client.PrefixedObjectClient)
		assert.False(t, ok)
	})

	t.Run("prefix with trailing /", func(t *testing.T) {
		cfg := newFSCfg(t)
		cfg.ObjectPrefix = "my/prefix/"

		objectClient, err := NewObjectClient("filesystem", "test", cfg, cm)
		require.NoError(t, err)

		prefixed, ok := objectClient.(client.PrefixedObjectClient)
		assert.True(t, ok)
		assert.Equal(t, "my/prefix/", prefixed.GetPrefix())
	})

	t.Run("prefix without trailing /", func(t *testing.T) {
		cfg := newFSCfg(t)
		cfg.ObjectPrefix = "my/prefix"

		objectClient, err := NewObjectClient("filesystem", "test", cfg, cm)
		require.NoError(t, err)

		prefixed, ok := objectClient.(client.PrefixedObjectClient)
		assert.True(t, ok)
		assert.Equal(t, "my/prefix/", prefixed.GetPrefix())
	})

	t.Run("prefix with starting and trailing /", func(t *testing.T) {
		cfg := newFSCfg(t)
		cfg.ObjectPrefix = "/my/prefix/"

		objectClient, err := NewObjectClient("filesystem", "test", cfg, cm)
		require.NoError(t, err)

		prefixed, ok := objectClient.(client.PrefixedObjectClient)
		assert.True(t, ok)
		assert.Equal(t, "my/prefix/", prefixed.GetPrefix())
	})
}

// // DefaultSchemaConfig creates a simple schema config for testing
// func DefaultSchemaConfig(store, schema string, from model.Time) config.SchemaConfig {
// 	s := config.SchemaConfig{
// 		Configs: []config.PeriodConfig{{
// 			IndexType: store,
// 			Schema:    schema,
// 			From:      config.DayTime{Time: from},
// 			ChunkTables: config.PeriodicTableConfig{
// 				Prefix: "cortex",
// 				Period: 24 * time.Hour,
// 			},
// 			IndexTables: config.IndexPeriodicTableConfig{
// 				PeriodicTableConfig: config.PeriodicTableConfig{
// 					Prefix: "cortex_chunks",
// 					Period: 24 * time.Hour,
// 				}},
// 		}},
// 	}
// 	if err := s.Validate(); err != nil {
// 		panic(err)
// 	}
// 	return s
// }
