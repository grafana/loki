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
	cortex_swift "github.com/cortexproject/cortex/pkg/storage/bucket/swift"

	"github.com/grafana/loki/pkg/storage/chunk/storage"
	"github.com/grafana/loki/pkg/util/cfg"
)

func Test_CommonConfig(t *testing.T) {
	testContext := func(configFileString string, args []string) (error, ConfigWrapper, ConfigWrapper) {
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
			empty := ConfigWrapper{}
			return err, empty, empty
		}

		defaults := ConfigWrapper{}
		freshFlags := flag.NewFlagSet(t.Name(), flag.PanicOnError)
		err = cfg.DefaultUnmarshal(&defaults, args, freshFlags)
		require.NoError(t, err)

		return nil, config, defaults
	}

	//the unmarshaller overwrites default values with 0s when a completely empty
	//config file is passed, so our "empty" config has some non-relevant config in it
	const emptyConfigString = `---
server:
  http_listen_port: 80`

	t.Run("common path prefix config", func(t *testing.T) {
		t.Run("does not override defaults for file paths when not provided", func(t *testing.T) {
			err, config, defaults := testContext(emptyConfigString, nil)
			require.NoError(t, err)

			assert.EqualValues(t, defaults.Ruler.RulePath, config.Ruler.RulePath)
			assert.EqualValues(t, defaults.Ingester.WAL.Dir, config.Ingester.WAL.Dir)
		})

		t.Run("when provided, rewrites all default file paths to use common prefix", func(t *testing.T) {
			configFileString := `---
common:
  path_prefix: /opt/loki`
			err, config, _ := testContext(configFileString, nil)
			require.NoError(t, err)

			assert.EqualValues(t, "/opt/loki/rules", config.Ruler.RulePath)
			assert.EqualValues(t, "/opt/loki/wal", config.Ingester.WAL.Dir)
		})

		t.Run("does not rewrite custom (non-default) paths passed via config file", func(t *testing.T) {
			configFileString := `---
common:
  path_prefix: /opt/loki
ruler:
  rule_path: /etc/ruler/rules`
			err, config, _ := testContext(configFileString, nil)
			require.NoError(t, err)

			assert.EqualValues(t, "/etc/ruler/rules", config.Ruler.RulePath)
			assert.EqualValues(t, "/opt/loki/wal", config.Ingester.WAL.Dir)
		})

		t.Run("does not rewrite custom (non-default) paths passed via the command line", func(t *testing.T) {
			configFileString := `---
common:
  path_prefix: /opt/loki`
			err, config, _ := testContext(configFileString, []string{"-ruler.rule-path", "/etc/ruler/rules"})
			require.NoError(t, err)

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
			err, config, defaults := testContext(emptyConfigString, nil)
			require.NoError(t, err)

			assert.EqualValues(t, defaults.Ingester.LifecyclerConfig.RingConfig.KVStore.Store, config.Ingester.LifecyclerConfig.RingConfig.KVStore.Store)
			assert.EqualValues(t, defaults.Distributor.DistributorRing.KVStore.Store, config.Distributor.DistributorRing.KVStore.Store)
			assert.EqualValues(t, defaults.Ruler.Ring.KVStore.Store, config.Ruler.Ring.KVStore.Store)
		})

		t.Run("when top-level memberlist join_members are provided, all applicable rings are defaulted to use memberlist", func(t *testing.T) {
			configFileString := `---
memberlist:
  join_members:
    - foo.bar.example.com`

			err, config, _ := testContext(configFileString, nil)
			require.NoError(t, err)

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

			err, config, _ := testContext(configFileString, nil)
			require.NoError(t, err)

			assert.EqualValues(t, "etcd", config.Distributor.DistributorRing.KVStore.Store)

			assert.EqualValues(t, memberlistStr, config.Ingester.LifecyclerConfig.RingConfig.KVStore.Store)
			assert.EqualValues(t, memberlistStr, config.Ruler.Ring.KVStore.Store)
		})

		t.Run("explicit ring configs provided via command line are preserved", func(t *testing.T) {
			configFileString := `---
memberlist:
  join_members:
    - foo.bar.example.com`

			err, config, _ := testContext(configFileString, []string{"-ruler.ring.store", "inmemory"})
			require.NoError(t, err)

			assert.EqualValues(t, "inmemory", config.Ruler.Ring.KVStore.Store)

			assert.EqualValues(t, memberlistStr, config.Ingester.LifecyclerConfig.RingConfig.KVStore.Store)
			assert.EqualValues(t, memberlistStr, config.Distributor.DistributorRing.KVStore.Store)
		})
	})

	t.Run("common object store config", func(t *testing.T) {
		//config file structure
		//common:
		//  object_store:
		//    s3: aws.S3Config
		//    gcs: gcp.GCSConfig
		//    azure: azure.BlobStorageConfig
		//    swift: openstack.SwiftConfig

		t.Run("does not automatically configure cloud object storage", func(t *testing.T) {
			err, config, defaults := testContext(emptyConfigString, nil)
			require.NoError(t, err)

			assert.EqualValues(t, defaults.Ruler.StoreConfig.Type, config.Ruler.StoreConfig.Type)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.Azure, config.Ruler.StoreConfig.Azure)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.GCS, config.Ruler.StoreConfig.GCS)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.S3, config.Ruler.StoreConfig.S3)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.Swift, config.Ruler.StoreConfig.Swift)

			assert.EqualValues(t, defaults.StorageConfig.AWSStorageConfig, config.StorageConfig.AWSStorageConfig)
			assert.EqualValues(t, defaults.StorageConfig.AzureStorageConfig, config.StorageConfig.AzureStorageConfig)
			assert.EqualValues(t, defaults.StorageConfig.GCSConfig, config.StorageConfig.GCSConfig)
			assert.EqualValues(t, defaults.StorageConfig.Swift, config.StorageConfig.Swift)
		})

		t.Run("returns an error if multiple object storage configs are provided", func(t *testing.T) {
			multipleConfig := `common:
  object_store:
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

			err, _, _ := testContext(multipleConfig, nil)
			assert.Error(t, err)
		})

		t.Run("when common s3 object_store config is provided, ruler and storage config are defaulted to use it", func(t *testing.T) {
			s3Config := `common:
  object_store:
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

			err, config, defaults := testContext(s3Config, nil)
			require.NoError(t, err)

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

			//should remain empty
			assert.EqualValues(t, defaults.StorageConfig.AzureStorageConfig, config.StorageConfig.AzureStorageConfig)
			assert.EqualValues(t, defaults.StorageConfig.GCSConfig, config.StorageConfig.GCSConfig)
			assert.EqualValues(t, defaults.StorageConfig.Swift, config.StorageConfig.Swift)
		})

		t.Run("when common gcs object_store config is provided, ruler and storage config are defaulted to use it", func(t *testing.T) {
			gcsConfig := `common:
  object_store:
    gcs:
      bucket_name: foobar
      chunk_buffer_size: 27
      request_timeout: 5m
      enable_opencensus: true`

			err, config, defaults := testContext(gcsConfig, nil)
			require.NoError(t, err)

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

			//should remain empty
			assert.EqualValues(t, defaults.StorageConfig.AzureStorageConfig, config.StorageConfig.AzureStorageConfig)
			assert.EqualValues(t, defaults.StorageConfig.AWSStorageConfig.S3Config, config.StorageConfig.AWSStorageConfig.S3Config)
			assert.EqualValues(t, defaults.StorageConfig.Swift, config.StorageConfig.Swift)
		})

		t.Run("when common azure object_store config is provided, ruler and storage config are defaulted to use it", func(t *testing.T) {
			azureConfig := `common:
  object_store:
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

			err, config, defaults := testContext(azureConfig, nil)
			require.NoError(t, err)

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

			//should remain empty
			assert.EqualValues(t, defaults.StorageConfig.GCSConfig, config.StorageConfig.GCSConfig)
			assert.EqualValues(t, defaults.StorageConfig.AWSStorageConfig.S3Config, config.StorageConfig.AWSStorageConfig.S3Config)
			assert.EqualValues(t, defaults.StorageConfig.Swift, config.StorageConfig.Swift)
		})

		t.Run("when common swift object_store config is provided, ruler and storage config are defaulted to use it", func(t *testing.T) {
			swiftConfig := `common:
  object_store:
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

			err, config, defaults := testContext(swiftConfig, nil)
			require.NoError(t, err)

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

			//should remain empty
			assert.EqualValues(t, defaults.StorageConfig.GCSConfig, config.StorageConfig.GCSConfig)
			assert.EqualValues(t, defaults.StorageConfig.AWSStorageConfig.S3Config, config.StorageConfig.AWSStorageConfig.S3Config)
			assert.EqualValues(t, defaults.StorageConfig.AzureStorageConfig, config.StorageConfig.AzureStorageConfig)
		})

		t.Run("explicit ruler storage object storage configuration provided via config file is preserved", func(t *testing.T) {
			specificRulerConfig := `common:
  object_store:
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
			err, config, defaults := testContext(specificRulerConfig, nil)
			require.NoError(t, err)

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
  object_store:
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

			err, config, defaults := testContext(specificRulerConfig, nil)
			require.NoError(t, err)

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
  object_store:
    s3:
      s3: s3://foo-bucket/example
      access_key_id: abc123
      secret_access_key: def789`,
					expected: storage.StorageTypeS3,
				},
				{
					configString: `common:
  object_store:
    gcs:
      bucket_name: foobar`,
					expected: storage.StorageTypeGCS,
				},
				{
					configString: `common:
  object_store:
    azure:
      account_name: 3rd_planet
      account_key: water`,
					expected: storage.StorageTypeAzure,
				},
				{
					configString: `common:
  object_store:
    swift:
      username: steve
      password: supersecret`,
					expected: storage.StorageTypeSwift,
				},
			} {
				err, config, _ := testContext(tt.configString, nil)
				require.NoError(t, err)

				assert.Equal(t, tt.expected, config.CompactorConfig.SharedStoreType)
			}
		})

    //TODO: test that compactor shared store type is not overriden if specified
    //TODO: add comments to PR about things that could be moved to dskit
	})
}

// Can't use a totally empty yaml file or it causes weird behavior in the unmarhsalling
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
