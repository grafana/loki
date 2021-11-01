package loki

import (
	"flag"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/distributor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cortex_aws "github.com/cortexproject/cortex/pkg/chunk/aws"
	cortex_azure "github.com/cortexproject/cortex/pkg/chunk/azure"
	cortex_gcp "github.com/cortexproject/cortex/pkg/chunk/gcp"
	cortex_local "github.com/cortexproject/cortex/pkg/ruler/rulestore/local"
	cortex_swift "github.com/cortexproject/cortex/pkg/storage/bucket/swift"

	"github.com/grafana/loki/pkg/loki/common"
	"github.com/grafana/loki/pkg/storage/chunk/storage"
	"github.com/grafana/loki/pkg/util"
	"github.com/grafana/loki/pkg/util/cfg"
	loki_net "github.com/grafana/loki/pkg/util/net"
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
			assert.EqualValues(t, "/opt/loki/compactor", config.CompactorConfig.WorkingDirectory)
		})

		t.Run("accepts paths both with and without trailing slash", func(t *testing.T) {
			configFileString := `---
common:
  path_prefix: /opt/loki/`
			config, _ := testContext(configFileString, nil)

			assert.EqualValues(t, "/opt/loki/rules", config.Ruler.RulePath)
			assert.EqualValues(t, "/opt/loki/wal", config.Ingester.WAL.Dir)
			assert.EqualValues(t, "/opt/loki/compactor", config.CompactorConfig.WorkingDirectory)
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
		//    filesystem: local.FSConfig

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
      http_config:
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
				assert.Equal(t, 5*time.Minute, actual.HTTPConfig.ResponseHeaderTimeout)
				assert.Equal(t, false, actual.HTTPConfig.InsecureSkipVerify)

				assert.Equal(t, cortex_aws.SignatureVersionV4, actual.SignatureVersion,
					"signature version should equal default value")
				assert.Equal(t, 90*time.Second, actual.HTTPConfig.IdleConnTimeout,
					"idle connection timeout should equal default value")
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
      request_timeout: 5m`

			config, defaults := testContext(gcsConfig, nil)

			assert.Equal(t, "gcs", config.Ruler.StoreConfig.Type)

			for _, actual := range []cortex_gcp.GCSConfig{
				config.Ruler.StoreConfig.GCS,
				config.StorageConfig.GCSConfig.ToCortexGCSConfig(),
			} {
				assert.Equal(t, "foobar", actual.BucketName)
				assert.Equal(t, 27, actual.ChunkBufferSize)
				assert.Equal(t, 5*time.Minute, actual.RequestTimeout)
				assert.Equal(t, true, actual.EnableOpenCensus, "should get default value for unspecified oc config")
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
				assert.Equal(t, "AzureGlobal", actual.Environment,
					"should equal default environment since unspecified in config")

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
      connect_timeout: 5m`

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
				assert.Equal(t, 5*time.Minute, actual.ConnectTimeout)

				assert.Equal(t, 3, actual.MaxRetries,
					"unspecified max retries should get default value")
				assert.Equal(t, 5*time.Second, actual.RequestTimeout,
					"unspecified connection timeout should get default value")
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

			//should be set by common config
			assert.EqualValues(t, "foobar", config.Ruler.StoreConfig.GCS.BucketName)
			assert.EqualValues(t, 27, config.Ruler.StoreConfig.GCS.ChunkBufferSize)
			assert.EqualValues(t, 5*time.Minute, config.Ruler.StoreConfig.GCS.RequestTimeout)

			//should remain empty
			assert.EqualValues(t, defaults.Ruler.StoreConfig.S3, config.Ruler.StoreConfig.S3)
		})

		t.Run("partial ruler config from file is honored for overriding things like bucket names", func(t *testing.T) {
			specificRulerConfig := `common:
  storage:
    gcs:
      bucket_name: foobar
      chunk_buffer_size: 27
      request_timeout: 5m
ruler:
  storage:
    gcs:
      bucket_name: rules`

			config, _ := testContext(specificRulerConfig, nil)

			assert.EqualValues(t, "rules", config.Ruler.StoreConfig.GCS.BucketName)

			//from common config
			assert.EqualValues(t, 27, config.Ruler.StoreConfig.GCS.ChunkBufferSize)
			assert.EqualValues(t, 5*time.Minute, config.Ruler.StoreConfig.GCS.RequestTimeout)
		})

		t.Run("partial chunk store config from file is honored for overriding things like bucket names", func(t *testing.T) {
			specificRulerConfig := `common:
  storage:
    gcs:
      bucket_name: foobar
      chunk_buffer_size: 27
      request_timeout: 5m
storage_config:
  gcs:
    bucket_name: chunks`

			config, _ := testContext(specificRulerConfig, nil)

			assert.EqualValues(t, "chunks", config.StorageConfig.GCSConfig.BucketName)

			//from common config
			assert.EqualValues(t, 27, config.StorageConfig.GCSConfig.ChunkBufferSize)
			assert.EqualValues(t, 5*time.Minute, config.StorageConfig.GCSConfig.RequestTimeout)
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

		t.Run("if path prefix provided in common config, default active_index_directory and cache_location", func(t *testing.T) {})
		const boltdbSchemaConfig = `---
common:
  path_prefix: /opt/loki
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

		assert.Equal(t, "/opt/loki/boltdb-shipper-active", config.StorageConfig.BoltDBShipperConfig.ActiveIndexDirectory)
		assert.Equal(t, "/opt/loki/boltdb-shipper-cache", config.StorageConfig.BoltDBShipperConfig.CacheLocation)
	})

	t.Run("boltdb shipper directories correctly handle trailing slash in path prefix", func(t *testing.T) {
		const boltdbSchemaConfig = `---
common:
  path_prefix: /opt/loki/
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

		assert.Equal(t, "/opt/loki/boltdb-shipper-active", config.StorageConfig.BoltDBShipperConfig.ActiveIndexDirectory)
		assert.Equal(t, "/opt/loki/boltdb-shipper-cache", config.StorageConfig.BoltDBShipperConfig.CacheLocation)
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

func Test_applyIngesterRingConfig(t *testing.T) {

	t.Run("Attempt to catch changes to a RingConfig", func(t *testing.T) {
		msgf := "%s has changed, this is a crude attempt to catch mapping errors missed in config_wrapper.applyIngesterRingConfig when a ring config changes. Please add a new mapping and update the expected value in this test."

		assert.Equal(t, 8, reflect.TypeOf(distributor.RingConfig{}).NumField(), fmt.Sprintf(msgf, reflect.TypeOf(distributor.RingConfig{}).String()))
		assert.Equal(t, 12, reflect.TypeOf(util.RingConfig{}).NumField(), fmt.Sprintf(msgf, reflect.TypeOf(util.RingConfig{}).String()))
	})

	t.Run("compactor and scheduler tokens file should not be configured if persist_tokens is false", func(t *testing.T) {
		yamlContent := `
common:
  path_prefix: /loki
`
		config, _, err := configWrapperFromYAML(t, yamlContent, []string{})
		assert.NoError(t, err)

		assert.Equal(t, "", config.Ingester.LifecyclerConfig.TokensFilePath)
		assert.Equal(t, "", config.CompactorConfig.CompactorRing.TokensFilePath)
		assert.Equal(t, "", config.QueryScheduler.SchedulerRing.TokensFilePath)
	})

	t.Run("tokens files should be set from common config when persist_tokens is true and path_prefix is defined", func(t *testing.T) {
		yamlContent := `
common:
  persist_tokens: true
  path_prefix: /loki
`
		config, _, err := configWrapperFromYAML(t, yamlContent, []string{})
		assert.NoError(t, err)

		assert.Equal(t, "/loki/ingester.tokens", config.Ingester.LifecyclerConfig.TokensFilePath)
		assert.Equal(t, "/loki/compactor.tokens", config.CompactorConfig.CompactorRing.TokensFilePath)
		assert.Equal(t, "/loki/scheduler.tokens", config.QueryScheduler.SchedulerRing.TokensFilePath)
	})

	t.Run("common config ignored if actual values set", func(t *testing.T) {
		yamlContent := `
ingester:
  lifecycler:
    tokens_file_path: /loki/toookens
compactor:
  compactor_ring:
    tokens_file_path: /foo/tokens
query_scheduler:
  scheduler_ring:
    tokens_file_path: /sched/tokes
common:
  persist_tokens: true
  path_prefix: /loki
`
		config, _, err := configWrapperFromYAML(t, yamlContent, []string{})
		assert.NoError(t, err)

		assert.Equal(t, "/loki/toookens", config.Ingester.LifecyclerConfig.TokensFilePath)
		assert.Equal(t, "/foo/tokens", config.CompactorConfig.CompactorRing.TokensFilePath)
		assert.Equal(t, "/sched/tokes", config.QueryScheduler.SchedulerRing.TokensFilePath)
	})

}

func TestRingInterfaceNames(t *testing.T) {
	defaultIface, err := loki_net.LoopbackInterfaceName()
	assert.NoError(t, err)
	assert.NotEmpty(t, defaultIface)

	t.Run("by default, loopback is available for all ring interfaces", func(t *testing.T) {
		config, _, err := configWrapperFromYAML(t, minimalConfig, []string{})
		assert.NoError(t, err)

		assert.Contains(t, config.Ingester.LifecyclerConfig.InfNames, defaultIface)
		assert.Contains(t, config.Distributor.DistributorRing.InstanceInterfaceNames, defaultIface)
		assert.Contains(t, config.QueryScheduler.SchedulerRing.InstanceInterfaceNames, defaultIface)
		assert.Contains(t, config.Ruler.Ring.InstanceInterfaceNames, defaultIface)
	})

	t.Run("if ingestor interface is set, it overrides other rings default interfaces", func(t *testing.T) {
		yamlContent := `ingester:
  lifecycler:
    interface_names:
    - ingesteriface`

		config, _, err := configWrapperFromYAML(t, yamlContent, []string{})
		assert.NoError(t, err)
		assert.Equal(t, config.Distributor.DistributorRing.InstanceInterfaceNames, []string{"ingesteriface"})
		assert.Equal(t, config.QueryScheduler.SchedulerRing.InstanceInterfaceNames, []string{"ingesteriface"})
		assert.Equal(t, config.Ruler.Ring.InstanceInterfaceNames, []string{"ingesteriface"})
		assert.Equal(t, config.Ingester.LifecyclerConfig.InfNames, []string{"ingesteriface"})
	})

	t.Run("if all rings have different net interface sets, doesn't override any of them", func(t *testing.T) {
		yamlContent := `distributor:
  ring:
    instance_interface_names:
    - distributoriface
ruler:
  ring:
    instance_interface_names:
    - ruleriface
query_scheduler:
  scheduler_ring:
    instance_interface_names:
    - scheduleriface
ingester:
  lifecycler:
    interface_names:
    - ingesteriface`

		config, _, err := configWrapperFromYAML(t, yamlContent, []string{})
		assert.NoError(t, err)
		assert.Equal(t, config.Ingester.LifecyclerConfig.InfNames, []string{"ingesteriface"})
		assert.Equal(t, config.Distributor.DistributorRing.InstanceInterfaceNames, []string{"distributoriface"})
		assert.Equal(t, config.QueryScheduler.SchedulerRing.InstanceInterfaceNames, []string{"scheduleriface"})
		assert.Equal(t, config.Ruler.Ring.InstanceInterfaceNames, []string{"ruleriface"})
	})

	t.Run("if all rings except ingester have net interface sets, doesn't override them with ingester default value", func(t *testing.T) {
		yamlContent := `distributor:
  ring:
    instance_interface_names:
    - distributoriface
ruler:
  ring:
    instance_interface_names:
    - ruleriface
query_scheduler:
  scheduler_ring:
    instance_interface_names:
    - scheduleriface`

		config, _, err := configWrapperFromYAML(t, yamlContent, []string{})
		assert.NoError(t, err)
		assert.Equal(t, config.Distributor.DistributorRing.InstanceInterfaceNames, []string{"distributoriface"})
		assert.Equal(t, config.QueryScheduler.SchedulerRing.InstanceInterfaceNames, []string{"scheduleriface"})
		assert.Equal(t, config.Ruler.Ring.InstanceInterfaceNames, []string{"ruleriface"})
		assert.Equal(t, config.Ingester.LifecyclerConfig.InfNames, []string{"eth0", "en0", defaultIface})
	})
}

func TestLoopbackAppendingToFrontendV2(t *testing.T) {
	defaultIface, err := loki_net.LoopbackInterfaceName()
	assert.NoError(t, err)
	assert.NotEmpty(t, defaultIface)

	t.Run("by default, loopback should be in FrontendV2 interface names", func(t *testing.T) {
		config, _, err := configWrapperFromYAML(t, minimalConfig, []string{})
		assert.NoError(t, err)
		assert.Equal(t, []string{"eth0", "en0", defaultIface}, config.Frontend.FrontendV2.InfNames)
	})

	t.Run("loopback shouldn't be in FrontendV2 interface names if set by user", func(t *testing.T) {
		yamlContent := `frontend:
  instance_interface_names:
  - otheriface`

		config, _, err := configWrapperFromYAML(t, yamlContent, []string{})
		assert.NoError(t, err)
		assert.Equal(t, []string{"otheriface"}, config.Frontend.FrontendV2.InfNames)
	})
}

func Test_tokensFile(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *ConfigWrapper
		file    string
		want    string
		wantErr bool
	}{
		{"persist_tokens false, path_prefix empty", &ConfigWrapper{Config: Config{Common: common.Config{PathPrefix: "", PersistTokens: false}}}, "ingester.tokens", "", false},
		{"persist_tokens true, path_prefix empty", &ConfigWrapper{Config: Config{Common: common.Config{PathPrefix: "", PersistTokens: true}}}, "ingester.tokens", "", true},
		{"persist_tokens true, path_prefix set", &ConfigWrapper{Config: Config{Common: common.Config{PathPrefix: "/loki", PersistTokens: true}}}, "ingester.tokens", "/loki/ingester.tokens", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tokensFile(tt.cfg, tt.file)
			if (err != nil) != tt.wantErr {
				t.Errorf("tokensFile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("tokensFile() got = %v, want %v", got, tt.want)
			}
		})
	}
}
