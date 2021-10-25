package loki

import (
	"flag"
	"io/ioutil"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cortex_aws "github.com/cortexproject/cortex/pkg/chunk/aws"
	cortex_azure "github.com/cortexproject/cortex/pkg/chunk/azure"
	cortex_gcp "github.com/cortexproject/cortex/pkg/chunk/gcp"
	cortex_local "github.com/cortexproject/cortex/pkg/ruler/rulestore/local"
	cortex_swift "github.com/cortexproject/cortex/pkg/storage/bucket/swift"

	"github.com/grafana/loki/pkg/storage/chunk/storage"
	"github.com/grafana/loki/pkg/util/cfg"
)

func configWrapperFromYAML(t *testing.T, configFileString string, args []string) (ConfigWrapper, ConfigWrapper, error) {
	config := ConfigWrapper{}
	fs := flag.NewFlagSet(t.Name(), flag.PanicOnError)

	file, err := ioutil.TempFile("", "config.yaml")
	defer func() {
		os.Remove(file.Name())
	}()

	require.NoError(t, err)
	_, err = file.WriteString(configFileString)
	require.NoError(t, err)

	configFileArgs := []string{"-config.file", file.Name()}
	if args == nil {
		args = configFileArgs
	} else {
		args = append(args, configFileArgs...)
	}
	err = cfg.DynamicUnmarshal(&config, args, fs)
	if err != nil {
		return ConfigWrapper{}, ConfigWrapper{}, err
	}

	defaults := ConfigWrapper{}
	freshFlags := flag.NewFlagSet(t.Name(), flag.PanicOnError)
	err = cfg.DefaultUnmarshal(&defaults, args, freshFlags)
	require.NoError(t, err)

	return config, defaults, nil
}

func Test_ApplyDynamicConfig(t *testing.T) {
	testContext := func(configFileString string, args []string) (ConfigWrapper, ConfigWrapper) {
		config, defaults, err := configWrapperFromYAML(t, configFileString, args)
		require.NoError(t, err)

		return config, defaults
	}

	//the unmarshaller overwrites default values with 0s when a completely empty
	//config file is passed, so our "empty" config has some non-relevant config in it
	const emptyConfigString = `---
server:
  http_listen_port: 80`

	t.Run("common path prefix config", func(t *testing.T) {
		t.Run("does not override defaults for file paths when not provided", func(t *testing.T) {
			config, defaults := testContext(emptyConfigString, nil)

			assert.EqualValues(t, defaults.Ruler.RulePath, config.Ruler.RulePath)
			assert.EqualValues(t, defaults.Ingester.WAL.Dir, config.Ingester.WAL.Dir)
		})

		t.Run("when provided, rewrites all default file paths to use common prefix", func(t *testing.T) {
			configFileString := `---
common:
  path_prefix: /opt/loki`
			config, _ := testContext(configFileString, nil)

			assert.EqualValues(t, "/opt/loki/rules", config.Ruler.RulePath)
			assert.EqualValues(t, "/opt/loki/wal", config.Ingester.WAL.Dir)
		})

		t.Run("does not rewrite custom (non-default) paths passed via config file", func(t *testing.T) {
			configFileString := `---
common:
  path_prefix: /opt/loki
ruler:
  rule_path: /etc/ruler/rules`
			config, _ := testContext(configFileString, nil)

			assert.EqualValues(t, "/etc/ruler/rules", config.Ruler.RulePath)
			assert.EqualValues(t, "/opt/loki/wal", config.Ingester.WAL.Dir)
		})

		t.Run("does not rewrite custom (non-default) paths passed via the command line", func(t *testing.T) {
			configFileString := `---
common:
  path_prefix: /opt/loki`
			config, _ := testContext(configFileString, []string{"-ruler.rule-path", "/etc/ruler/rules"})

			assert.EqualValues(t, "/etc/ruler/rules", config.Ruler.RulePath)
			assert.EqualValues(t, "/opt/loki/wal", config.Ingester.WAL.Dir)
		})
	})

	t.Run("common memberlist config", func(t *testing.T) {
		// components with rings
		// * ingester
		// * distributor
		// * ruler

		t.Run("does not automatically configure memberlist when no top-level memberlist config is provided", func(t *testing.T) {
			config, defaults := testContext(emptyConfigString, nil)

			assert.EqualValues(t, defaults.Ingester.LifecyclerConfig.RingConfig.KVStore.Store, config.Ingester.LifecyclerConfig.RingConfig.KVStore.Store)
			assert.EqualValues(t, defaults.Distributor.DistributorRing.KVStore.Store, config.Distributor.DistributorRing.KVStore.Store)
			assert.EqualValues(t, defaults.Ruler.Ring.KVStore.Store, config.Ruler.Ring.KVStore.Store)
		})

		t.Run("when top-level memberlist join_members are provided, all applicable rings are defaulted to use memberlist", func(t *testing.T) {
			configFileString := `---
memberlist:
  join_members:
    - foo.bar.example.com`

			config, _ := testContext(configFileString, nil)

			assert.EqualValues(t, memberlistStr, config.Ingester.LifecyclerConfig.RingConfig.KVStore.Store)
			assert.EqualValues(t, memberlistStr, config.Distributor.DistributorRing.KVStore.Store)
			assert.EqualValues(t, memberlistStr, config.Ruler.Ring.KVStore.Store)
		})

		t.Run("explicit ring configs provided via config file are preserved", func(t *testing.T) {
			configFileString := `---
memberlist:
  join_members:
    - foo.bar.example.com
distributor:
  ring:
    kvstore:
      store: etcd`

			config, _ := testContext(configFileString, nil)

			assert.EqualValues(t, "etcd", config.Distributor.DistributorRing.KVStore.Store)

			assert.EqualValues(t, memberlistStr, config.Ingester.LifecyclerConfig.RingConfig.KVStore.Store)
			assert.EqualValues(t, memberlistStr, config.Ruler.Ring.KVStore.Store)
		})

		t.Run("explicit ring configs provided via command line are preserved", func(t *testing.T) {
			configFileString := `---
memberlist:
  join_members:
    - foo.bar.example.com`

			config, _ := testContext(configFileString, []string{"-ruler.ring.store", "inmemory"})

			assert.EqualValues(t, "inmemory", config.Ruler.Ring.KVStore.Store)

			assert.EqualValues(t, memberlistStr, config.Ingester.LifecyclerConfig.RingConfig.KVStore.Store)
			assert.EqualValues(t, memberlistStr, config.Distributor.DistributorRing.KVStore.Store)
		})
	})

	t.Run("common object store config", func(t *testing.T) {
		//config file structure
		//common:
		//  storage:
		//    azure: azure.BlobStorageConfig
		//    gcs: gcp.GCSConfig
		//    s3: aws.S3Config
		//    swift: openstack.SwiftConfig

		t.Run("does not automatically configure cloud object storage", func(t *testing.T) {
			config, defaults := testContext(emptyConfigString, nil)

			assert.EqualValues(t, defaults.Ruler.StoreConfig.Type, config.Ruler.StoreConfig.Type)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.Azure, config.Ruler.StoreConfig.Azure)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.GCS, config.Ruler.StoreConfig.GCS)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.S3, config.Ruler.StoreConfig.S3)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.Swift, config.Ruler.StoreConfig.Swift)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.Local, config.Ruler.StoreConfig.Local)

			assert.EqualValues(t, defaults.StorageConfig.AWSStorageConfig, config.StorageConfig.AWSStorageConfig)
			assert.EqualValues(t, defaults.StorageConfig.AzureStorageConfig, config.StorageConfig.AzureStorageConfig)
			assert.EqualValues(t, defaults.StorageConfig.GCSConfig, config.StorageConfig.GCSConfig)
			assert.EqualValues(t, defaults.StorageConfig.Swift, config.StorageConfig.Swift)
			assert.EqualValues(t, defaults.StorageConfig.FSConfig, config.StorageConfig.FSConfig)
		})

		t.Run("when multiple configs are provided, an error is returned", func(t *testing.T) {
			multipleConfig := `common:
  storage:
    s3:
      s3: s3://foo-bucket/example
      endpoint: s3://foo-bucket
      region: us-east1
      access_key_id: abc123
      secret_access_key: def789
    gcs:
      bucket_name: foobar
      chunk_buffer_size: 27
      request_timeout: 5m`

			_, _, err := configWrapperFromYAML(t, multipleConfig, nil)
			assert.ErrorIs(t, err, ErrTooManyStorageConfigs)
		})

		t.Run("when common s3 storage config is provided, ruler and storage config are defaulted to use it", func(t *testing.T) {
			s3Config := `common:
  storage:
    s3:
      s3: s3://foo-bucket/example
      endpoint: s3://foo-bucket
      region: us-east1
      access_key_id: abc123
      secret_access_key: def789
      insecure: true
      signature_version: v4
      http_config:
        idle_conn_timeout: 5m
        response_header_timeout: 5m`

			config, defaults := testContext(s3Config, nil)

			expected, err := url.Parse("s3://foo-bucket/example")
			require.NoError(t, err)

			assert.Equal(t, "s3", config.Ruler.StoreConfig.Type)

			for _, actual := range []cortex_aws.S3Config{
				config.Ruler.StoreConfig.S3,
				config.StorageConfig.AWSStorageConfig.S3Config.ToCortexS3Config(),
			} {
				require.NotNil(t, actual.S3.URL)
				assert.Equal(t, *expected, *actual.S3.URL)

				assert.Equal(t, false, actual.S3ForcePathStyle)
				assert.Equal(t, "s3://foo-bucket", actual.Endpoint)
				assert.Equal(t, "us-east1", actual.Region)
				assert.Equal(t, "abc123", actual.AccessKeyID)
				assert.Equal(t, "def789", actual.SecretAccessKey)
				assert.Equal(t, true, actual.Insecure)
				assert.Equal(t, false, actual.SSEEncryption)
				assert.Equal(t, 5*time.Minute, actual.HTTPConfig.IdleConnTimeout)
				assert.Equal(t, 5*time.Minute, actual.HTTPConfig.ResponseHeaderTimeout)
				assert.Equal(t, false, actual.HTTPConfig.InsecureSkipVerify)
				assert.Equal(t, "v4", actual.SignatureVersion)
			}

			//should remain empty
			assert.EqualValues(t, defaults.Ruler.StoreConfig.Azure, config.Ruler.StoreConfig.Azure)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.GCS, config.Ruler.StoreConfig.GCS)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.Swift, config.Ruler.StoreConfig.Swift)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.Local, config.Ruler.StoreConfig.Local)

			//should remain empty
			assert.EqualValues(t, defaults.StorageConfig.AzureStorageConfig, config.StorageConfig.AzureStorageConfig)
			assert.EqualValues(t, defaults.StorageConfig.GCSConfig, config.StorageConfig.GCSConfig)
			assert.EqualValues(t, defaults.StorageConfig.Swift, config.StorageConfig.Swift)
			assert.EqualValues(t, defaults.StorageConfig.FSConfig, config.StorageConfig.FSConfig)
		})

		t.Run("when common gcs storage config is provided, ruler and storage config are defaulted to use it", func(t *testing.T) {
			gcsConfig := `common:
  storage:
    gcs:
      bucket_name: foobar
      chunk_buffer_size: 27
      request_timeout: 5m
      enable_opencensus: true`

			config, defaults := testContext(gcsConfig, nil)

			assert.Equal(t, "gcs", config.Ruler.StoreConfig.Type)

			for _, actual := range []cortex_gcp.GCSConfig{
				config.Ruler.StoreConfig.GCS,
				config.StorageConfig.GCSConfig.ToCortexGCSConfig(),
			} {
				assert.Equal(t, "foobar", actual.BucketName)
				assert.Equal(t, 27, actual.ChunkBufferSize)
				assert.Equal(t, 5*time.Minute, actual.RequestTimeout)
				assert.Equal(t, true, actual.EnableOpenCensus)
			}

			//should remain empty
			assert.EqualValues(t, defaults.Ruler.StoreConfig.Azure, config.Ruler.StoreConfig.Azure)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.S3, config.Ruler.StoreConfig.S3)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.Swift, config.Ruler.StoreConfig.Swift)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.Local, config.Ruler.StoreConfig.Local)
			//should remain empty
			assert.EqualValues(t, defaults.StorageConfig.AzureStorageConfig, config.StorageConfig.AzureStorageConfig)
			assert.EqualValues(t, defaults.StorageConfig.AWSStorageConfig.S3Config, config.StorageConfig.AWSStorageConfig.S3Config)
			assert.EqualValues(t, defaults.StorageConfig.Swift, config.StorageConfig.Swift)
			assert.EqualValues(t, defaults.StorageConfig.FSConfig, config.StorageConfig.FSConfig)
		})

		t.Run("when common azure storage config is provided, ruler and storage config are defaulted to use it", func(t *testing.T) {
			azureConfig := `common:
  storage:
    azure:
      environment: earth
      container_name: milkyway
      account_name: 3rd_planet
      account_key: water
      download_buffer_size: 27
      upload_buffer_size: 42
      upload_buffer_count: 13
      request_timeout: 5m
      max_retries: 3
      min_retry_delay: 10s
      max_retry_delay: 10m`

			config, defaults := testContext(azureConfig, nil)

			assert.Equal(t, "azure", config.Ruler.StoreConfig.Type)

			for _, actual := range []cortex_azure.BlobStorageConfig{
				config.Ruler.StoreConfig.Azure,
				config.StorageConfig.AzureStorageConfig.ToCortexAzureConfig(),
			} {
				assert.Equal(t, "earth", actual.Environment)
				assert.Equal(t, "milkyway", actual.ContainerName)
				assert.Equal(t, "3rd_planet", actual.AccountName)
				assert.Equal(t, "water", actual.AccountKey.Value)
				assert.Equal(t, 27, actual.DownloadBufferSize)
				assert.Equal(t, 42, actual.UploadBufferSize)
				assert.Equal(t, 13, actual.UploadBufferCount)
				assert.Equal(t, 5*time.Minute, actual.RequestTimeout)
				assert.Equal(t, 3, actual.MaxRetries)
				assert.Equal(t, 10*time.Second, actual.MinRetryDelay)
				assert.Equal(t, 10*time.Minute, actual.MaxRetryDelay)
			}

			//should remain empty
			assert.EqualValues(t, defaults.Ruler.StoreConfig.GCS, config.Ruler.StoreConfig.GCS)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.S3, config.Ruler.StoreConfig.S3)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.Swift, config.Ruler.StoreConfig.Swift)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.Local, config.Ruler.StoreConfig.Local)

			//should remain empty
			assert.EqualValues(t, defaults.StorageConfig.GCSConfig, config.StorageConfig.GCSConfig)
			assert.EqualValues(t, defaults.StorageConfig.AWSStorageConfig.S3Config, config.StorageConfig.AWSStorageConfig.S3Config)
			assert.EqualValues(t, defaults.StorageConfig.Swift, config.StorageConfig.Swift)
			assert.EqualValues(t, defaults.StorageConfig.FSConfig, config.StorageConfig.FSConfig)
		})

		t.Run("when common swift storage config is provided, ruler and storage config are defaulted to use it", func(t *testing.T) {
			swiftConfig := `common:
  storage:
    swift:
      auth_version: 3
      auth_url: http://example.com
      username: steve
      user_domain_name: example.com
      user_domain_id: 1
      user_id: 27
      password: supersecret
      domain_id: 2
      domain_name: test.com
      project_id: 13
      project_name: tower
      project_domain_id: 3
      project_domain_name: tower.com
      region_name: us-east1
      container_name: tupperware
      max_retries: 6
      connect_timeout: 5m
      request_timeout: 5s`

			config, defaults := testContext(swiftConfig, nil)

			assert.Equal(t, "swift", config.Ruler.StoreConfig.Type)

			for _, actual := range []cortex_swift.Config{
				config.Ruler.StoreConfig.Swift.Config,
				config.StorageConfig.Swift.Config,
			} {
				assert.Equal(t, 3, actual.AuthVersion)
				assert.Equal(t, "http://example.com", actual.AuthURL)
				assert.Equal(t, "steve", actual.Username)
				assert.Equal(t, "example.com", actual.UserDomainName)
				assert.Equal(t, "1", actual.UserDomainID)
				assert.Equal(t, "27", actual.UserID)
				assert.Equal(t, "supersecret", actual.Password)
				assert.Equal(t, "2", actual.DomainID)
				assert.Equal(t, "test.com", actual.DomainName)
				assert.Equal(t, "13", actual.ProjectID)
				assert.Equal(t, "tower", actual.ProjectName)
				assert.Equal(t, "3", actual.ProjectDomainID)
				assert.Equal(t, "tower.com", actual.ProjectDomainName)
				assert.Equal(t, "us-east1", actual.RegionName)
				assert.Equal(t, "tupperware", actual.ContainerName)
				assert.Equal(t, 6, actual.MaxRetries)
				assert.Equal(t, 5*time.Minute, actual.ConnectTimeout)
				assert.Equal(t, 5*time.Second, actual.RequestTimeout)
			}

			//should remain empty
			assert.EqualValues(t, defaults.Ruler.StoreConfig.GCS, config.Ruler.StoreConfig.GCS)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.S3, config.Ruler.StoreConfig.S3)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.Azure, config.Ruler.StoreConfig.Azure)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.Local, config.Ruler.StoreConfig.Local)

			//should remain empty
			assert.EqualValues(t, defaults.StorageConfig.GCSConfig, config.StorageConfig.GCSConfig)
			assert.EqualValues(t, defaults.StorageConfig.AWSStorageConfig.S3Config, config.StorageConfig.AWSStorageConfig.S3Config)
			assert.EqualValues(t, defaults.StorageConfig.AzureStorageConfig, config.StorageConfig.AzureStorageConfig)
			assert.EqualValues(t, defaults.StorageConfig.FSConfig, config.StorageConfig.FSConfig)
		})

		t.Run("when common filesystem/local config is provided, ruler and storage config are defaulted to use it", func(t *testing.T) {
			fsConfig := `common:
  storage:
    filesystem:
      directory: /tmp/foo`

			config, defaults := testContext(fsConfig, nil)

			assert.Equal(t, "local", config.Ruler.StoreConfig.Type)

			for _, actual := range []cortex_local.Config{
				config.Ruler.StoreConfig.Local,
				config.StorageConfig.FSConfig.ToCortexLocalConfig(),
			} {
				assert.Equal(t, "/tmp/foo", actual.Directory)
			}

			//should remain empty
			assert.EqualValues(t, defaults.Ruler.StoreConfig.GCS, config.Ruler.StoreConfig.GCS)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.S3, config.Ruler.StoreConfig.S3)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.Azure, config.Ruler.StoreConfig.Azure)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.Swift, config.Ruler.StoreConfig.Swift)

			//should remain empty
			assert.EqualValues(t, defaults.StorageConfig.GCSConfig, config.StorageConfig.GCSConfig)
			assert.EqualValues(t, defaults.StorageConfig.AWSStorageConfig.S3Config, config.StorageConfig.AWSStorageConfig.S3Config)
			assert.EqualValues(t, defaults.StorageConfig.AzureStorageConfig, config.StorageConfig.AzureStorageConfig)
			assert.EqualValues(t, defaults.StorageConfig.Swift, config.StorageConfig.Swift)
		})

		t.Run("explicit ruler storage object storage configuration provided via config file is preserved", func(t *testing.T) {
			specificRulerConfig := `common:
  storage:
    gcs:
      bucket_name: foobar
      chunk_buffer_size: 27
      request_timeout: 5m
ruler:
  storage:
    type: s3
    s3:
      endpoint: s3://foo-bucket
      region: us-east1
      access_key_id: abc123
      secret_access_key: def789`
			config, defaults := testContext(specificRulerConfig, nil)

			assert.Equal(t, "s3", config.Ruler.StoreConfig.Type)
			assert.Equal(t, "s3://foo-bucket", config.Ruler.StoreConfig.S3.Endpoint)
			assert.Equal(t, "us-east1", config.Ruler.StoreConfig.S3.Region)
			assert.Equal(t, "abc123", config.Ruler.StoreConfig.S3.AccessKeyID)
			assert.Equal(t, "def789", config.Ruler.StoreConfig.S3.SecretAccessKey)

			//should remain empty
			assert.EqualValues(t, defaults.Ruler.StoreConfig.GCS, config.Ruler.StoreConfig.GCS)

			//should be set by common config
			assert.EqualValues(t, "foobar", config.StorageConfig.GCSConfig.BucketName)
			assert.EqualValues(t, 27, config.StorageConfig.GCSConfig.ChunkBufferSize)
			assert.EqualValues(t, 5*time.Minute, config.StorageConfig.GCSConfig.RequestTimeout)

			//should remain empty
			assert.EqualValues(t, defaults.StorageConfig.AWSStorageConfig.S3Config, config.StorageConfig.AWSStorageConfig.S3Config)
		})

		t.Run("explicit storage config provided via config file is preserved", func(t *testing.T) {
			specificRulerConfig := `common:
  storage:
    gcs:
      bucket_name: foobar
      chunk_buffer_size: 27
      request_timeout: 5m
storage_config:
  aws:
    endpoint: s3://foo-bucket
    region: us-east1
    access_key_id: abc123
    secret_access_key: def789`

			config, defaults := testContext(specificRulerConfig, nil)

			assert.Equal(t, "s3://foo-bucket", config.StorageConfig.AWSStorageConfig.S3Config.Endpoint)
			assert.Equal(t, "us-east1", config.StorageConfig.AWSStorageConfig.S3Config.Region)
			assert.Equal(t, "abc123", config.StorageConfig.AWSStorageConfig.S3Config.AccessKeyID)
			assert.Equal(t, "def789", config.StorageConfig.AWSStorageConfig.S3Config.SecretAccessKey)

			//should remain empty
			assert.EqualValues(t, defaults.StorageConfig.GCSConfig, config.StorageConfig.GCSConfig)

			//should be set by common config
			assert.EqualValues(t, "foobar", config.Ruler.StoreConfig.GCS.BucketName)
			assert.EqualValues(t, 27, config.Ruler.StoreConfig.GCS.ChunkBufferSize)
			assert.EqualValues(t, 5*time.Minute, config.Ruler.StoreConfig.GCS.RequestTimeout)

			//should remain empty
			assert.EqualValues(t, defaults.Ruler.StoreConfig.S3, config.Ruler.StoreConfig.S3)
		})

		t.Run("when common object store config is provided, compactor shared store is defaulted to use it", func(t *testing.T) {
			for _, tt := range []struct {
				configString string
				expected     string
			}{
				{
					configString: `common:
  storage:
    s3:
      s3: s3://foo-bucket/example
      access_key_id: abc123
      secret_access_key: def789`,
					expected: storage.StorageTypeS3,
				},
				{
					configString: `common:
  storage:
    gcs:
      bucket_name: foobar`,
					expected: storage.StorageTypeGCS,
				},
				{
					configString: `common:
  storage:
    azure:
      account_name: 3rd_planet
      account_key: water`,
					expected: storage.StorageTypeAzure,
				},
				{
					configString: `common:
  storage:
    swift:
      username: steve
      password: supersecret`,
					expected: storage.StorageTypeSwift,
				},
				{
					configString: `common:
  storage:
    filesystem:
      directory: /tmp/foo`,
					expected: storage.StorageTypeFileSystem,
				},
			} {
				config, _ := testContext(tt.configString, nil)

				assert.Equal(t, tt.expected, config.CompactorConfig.SharedStoreType)
			}
		})

		t.Run("explicit compactor shared_store config is preserved", func(t *testing.T) {
			configString := `common:
  storage:
    s3:
      s3: s3://foo-bucket/example
      access_key_id: abc123
      secret_access_key: def789
compactor:
  shared_store: gcs`
			config, _ := testContext(configString, nil)

			assert.Equal(t, "gcs", config.CompactorConfig.SharedStoreType)
		})
	})

	t.Run("when using boltdb storage type", func(t *testing.T) {
		t.Run("default storage_config.boltdb.shared_store to the value of current_schema.object_store", func(t *testing.T) {
			const boltdbSchemaConfig = `---
schema_config:
  configs:
    - from: 2021-08-01
      store: boltdb-shipper
      object_store: s3
      schema: v11
      index:
        prefix: index_
        period: 24h`
			config, _ := testContext(boltdbSchemaConfig, nil)

			assert.Equal(t, storage.StorageTypeS3, config.StorageConfig.BoltDBShipperConfig.SharedStoreType)
		})

		t.Run("default compactor.shared_store to the value of current_schema.object_store", func(t *testing.T) {
			const boltdbSchemaConfig = `---
schema_config:
  configs:
    - from: 2021-08-01
      store: boltdb-shipper
      object_store: gcs
      schema: v11
      index:
        prefix: index_
        period: 24h`
			config, _ := testContext(boltdbSchemaConfig, nil)

			assert.Equal(t, storage.StorageTypeGCS, config.CompactorConfig.SharedStoreType)
		})

		t.Run("shared store types provided via config file take precedence", func(t *testing.T) {
			const boltdbSchemaConfig = `---
schema_config:
  configs:
    - from: 2021-08-01
      store: boltdb-shipper
      object_store: gcs
      schema: v11
      index:
        prefix: index_
        period: 24h

storage_config:
  boltdb_shipper:
    shared_store: s3

compactor:
  shared_store: s3`
			config, _ := testContext(boltdbSchemaConfig, nil)

			assert.Equal(t, storage.StorageTypeS3, config.StorageConfig.BoltDBShipperConfig.SharedStoreType)
			assert.Equal(t, storage.StorageTypeS3, config.CompactorConfig.SharedStoreType)
		})

		t.Run("shared store types provided via command line take precedence", func(t *testing.T) {
			const boltdbSchemaConfig = `---
schema_config:
  configs:
    - from: 2021-08-01
      store: boltdb-shipper
      object_store: gcs
      schema: v11
      index:
        prefix: index_
        period: 24h`
			config, _ := testContext(boltdbSchemaConfig, []string{"-boltdb.shipper.compactor.shared-store", "s3", "-boltdb.shipper.shared-store", "s3"})

			assert.Equal(t, storage.StorageTypeS3, config.StorageConfig.BoltDBShipperConfig.SharedStoreType)
			assert.Equal(t, storage.StorageTypeS3, config.CompactorConfig.SharedStoreType)
		})
	})
}

func TestDefaultFIFOCacheBehavior(t *testing.T) {
	t.Run("for the chunk cache config", func(t *testing.T) {
		t.Run("no FIFO cache enabled by default if Redis is set", func(t *testing.T) {
			configFileString := `---
chunk_store_config:
  chunk_cache_config: 
    redis:
      endpoint: endpoint.redis.org`

			config, _, _ := configWrapperFromYAML(t, configFileString, nil)
			assert.EqualValues(t, "endpoint.redis.org", config.ChunkStoreConfig.ChunkCacheConfig.Redis.Endpoint)
			assert.False(t, config.ChunkStoreConfig.ChunkCacheConfig.EnableFifoCache)
		})

		t.Run("no FIFO cache enabled by default if Memcache is set", func(t *testing.T) {
			configFileString := `---
chunk_store_config:
  chunk_cache_config: 
    memcached_client:
      host: host.memcached.org`

			config, _, _ := configWrapperFromYAML(t, configFileString, nil)
			assert.EqualValues(t, "host.memcached.org", config.ChunkStoreConfig.ChunkCacheConfig.MemcacheClient.Host)
			assert.False(t, config.ChunkStoreConfig.ChunkCacheConfig.EnableFifoCache)
		})

		t.Run("FIFO cache is enabled by default if no other cache is set", func(t *testing.T) {
			config, _, _ := configWrapperFromYAML(t, minimalConfig, nil)
			assert.True(t, config.ChunkStoreConfig.ChunkCacheConfig.EnableFifoCache)
		})
	})

	t.Run("for the write dedupe cache config", func(t *testing.T) {
		t.Run("no FIFO cache enabled by default if Redis is set", func(t *testing.T) {
			configFileString := `---
chunk_store_config:
  write_dedupe_cache_config:
    redis:
      endpoint: endpoint.redis.org`

			config, _, _ := configWrapperFromYAML(t, configFileString, nil)
			assert.EqualValues(t, "endpoint.redis.org", config.ChunkStoreConfig.WriteDedupeCacheConfig.Redis.Endpoint)
			assert.False(t, config.ChunkStoreConfig.WriteDedupeCacheConfig.EnableFifoCache)
		})

		t.Run("no FIFO cache enabled by default if Memcache is set", func(t *testing.T) {
			configFileString := `---
chunk_store_config:
  write_dedupe_cache_config:
    memcached_client:
      host: host.memcached.org`

			config, _, _ := configWrapperFromYAML(t, configFileString, nil)
			assert.EqualValues(t, "host.memcached.org", config.ChunkStoreConfig.WriteDedupeCacheConfig.MemcacheClient.Host)
			assert.False(t, config.ChunkStoreConfig.WriteDedupeCacheConfig.EnableFifoCache)
		})

		t.Run("no FIFO cache is enabled by default even if no other cache is set", func(t *testing.T) {
			config, _, _ := configWrapperFromYAML(t, minimalConfig, nil)
			assert.False(t, config.ChunkStoreConfig.WriteDedupeCacheConfig.EnableFifoCache)
		})
	})

	t.Run("for the index queries cache config", func(t *testing.T) {
		t.Run("no FIFO cache enabled by default if Redis is set", func(t *testing.T) {
			configFileString := `---
storage_config:
  index_queries_cache_config:
    redis:
      endpoint: endpoint.redis.org`

			config, _, _ := configWrapperFromYAML(t, configFileString, nil)
			assert.EqualValues(t, "endpoint.redis.org", config.StorageConfig.IndexQueriesCacheConfig.Redis.Endpoint)
			assert.False(t, config.StorageConfig.IndexQueriesCacheConfig.EnableFifoCache)
		})

		t.Run("no FIFO cache enabled by default if Memcache is set", func(t *testing.T) {
			configFileString := `---
storage_config:
  index_queries_cache_config:
    memcached_client:
      host: host.memcached.org`

			config, _, _ := configWrapperFromYAML(t, configFileString, nil)

			assert.EqualValues(t, "host.memcached.org", config.StorageConfig.IndexQueriesCacheConfig.MemcacheClient.Host)
			assert.False(t, config.StorageConfig.IndexQueriesCacheConfig.EnableFifoCache)
		})

		t.Run("no FIFO cache is enabled by default even if no other cache is set", func(t *testing.T) {
			config, _, _ := configWrapperFromYAML(t, minimalConfig, nil)
			assert.False(t, config.StorageConfig.IndexQueriesCacheConfig.EnableFifoCache)
		})
	})

	t.Run("for the query range results cache config", func(t *testing.T) {
		t.Run("no FIFO cache enabled by default if Redis is set", func(t *testing.T) {
			configFileString := `---
query_range:
  results_cache: 
    cache:
      redis:
        endpoint: endpoint.redis.org`

			config, _, _ := configWrapperFromYAML(t, configFileString, nil)
			assert.EqualValues(t, config.QueryRange.CacheConfig.Redis.Endpoint, "endpoint.redis.org")
			assert.False(t, config.QueryRange.CacheConfig.EnableFifoCache)
		})

		t.Run("no FIFO cache enabled by default if Memcache is set", func(t *testing.T) {
			configFileString := `---
query_range:
  results_cache: 
    cache:
      memcached_client:
        host: memcached.host.org`

			config, _, _ := configWrapperFromYAML(t, configFileString, nil)
			assert.EqualValues(t, "memcached.host.org", config.QueryRange.CacheConfig.MemcacheClient.Host)
			assert.False(t, config.QueryRange.CacheConfig.EnableFifoCache)
		})

		t.Run("FIFO cache is enabled by default if no other cache is set", func(t *testing.T) {
			config, _, _ := configWrapperFromYAML(t, minimalConfig, nil)
			assert.True(t, config.QueryRange.CacheConfig.EnableFifoCache)
		})
	})
}

// Can't use a totally empty yaml file or it causes weird behavior in the unmarhsalling.
const minimalConfig = `---
schema_config:
  configs:
    - from: 2021-08-01
      schema: v11
 
memberlist: 
  join_members: 
    - loki.loki-dev-single-binary.svc.cluster.local`

func TestDefaultUnmarshal(t *testing.T) {
	t.Run("with a minimal config file and no command line args, defaults are use", func(t *testing.T) {
		file, err := ioutil.TempFile("", "config.yaml")
		defer func() {
			os.Remove(file.Name())
		}()
		require.NoError(t, err)

		_, err = file.WriteString(minimalConfig)
		require.NoError(t, err)
		var config ConfigWrapper

		flags := flag.NewFlagSet(t.Name(), flag.PanicOnError)
		args := []string{"-config.file", file.Name()}
		err = cfg.DefaultUnmarshal(&config, args, flags)
		require.NoError(t, err)

		assert.True(t, config.AuthEnabled)
		assert.Equal(t, 80, config.Server.HTTPListenPort)
		assert.Equal(t, 9095, config.Server.GRPCListenPort)
	})
}
