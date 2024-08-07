package loki

import (
	"flag"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/netutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/distributor"
	"github.com/grafana/loki/v3/pkg/loki/common"
	"github.com/grafana/loki/v3/pkg/storage/bucket/swift"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/alibaba"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/aws"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/azure"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/baidubce"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/gcp"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/ibmcloud"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/openstack"
	"github.com/grafana/loki/v3/pkg/util/cfg"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	loki_net "github.com/grafana/loki/v3/pkg/util/net"
	lokiring "github.com/grafana/loki/v3/pkg/util/ring"
)

// Can't use a totally empty yaml file or it causes weird behavior in the unmarshalling.
const minimalConfig = `---
schema_config:
  configs:
    - from: 2021-08-01
      schema: v11`

func configWrapperFromYAML(t *testing.T, configFileString string, args []string) (ConfigWrapper, ConfigWrapper, error) {
	config := ConfigWrapper{}
	fs := flag.NewFlagSet(t.Name(), flag.PanicOnError)

	file, err := os.CreateTemp("", "config.yaml")
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

	// the unmarshaller overwrites default values with 0s when a completely empty
	// config file is passed, so our "empty" config has some non-relevant config in it
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

			assert.EqualValues(t, "/opt/loki/rules-temp", config.Ruler.RulePath)
			assert.EqualValues(t, "/opt/loki/wal", config.Ingester.WAL.Dir)
			assert.EqualValues(t, "/opt/loki/compactor", config.CompactorConfig.WorkingDirectory)
			assert.EqualValues(t, flagext.StringSliceCSV{"/opt/loki/blooms"}, config.StorageConfig.BloomShipperConfig.WorkingDirectory)
		})

		t.Run("accepts paths both with and without trailing slash", func(t *testing.T) {
			configFileString := `---
common:
  path_prefix: /opt/loki/`
			config, _ := testContext(configFileString, nil)

			assert.EqualValues(t, "/opt/loki/rules-temp", config.Ruler.RulePath)
			assert.EqualValues(t, "/opt/loki/wal", config.Ingester.WAL.Dir)
			assert.EqualValues(t, "/opt/loki/compactor", config.CompactorConfig.WorkingDirectory)
			assert.EqualValues(t, flagext.StringSliceCSV{"/opt/loki/blooms"}, config.StorageConfig.BloomShipperConfig.WorkingDirectory)
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

			config, _ := testContext(configFileString, []string{"-ruler.ring.store", "inmemory", "-index-gateway.ring.store", "etcd"})

			assert.EqualValues(t, "inmemory", config.Ruler.Ring.KVStore.Store)
			assert.EqualValues(t, "etcd", config.IndexGateway.Ring.KVStore.Store)

			assert.EqualValues(t, memberlistStr, config.Ingester.LifecyclerConfig.RingConfig.KVStore.Store)
			assert.EqualValues(t, memberlistStr, config.Distributor.DistributorRing.KVStore.Store)
		})
	})

	t.Run("common object store config", func(t *testing.T) {
		// config file structure
		// common:
		//  storage:
		//    azure: azure.BlobStorageConfig
		//    gcs: gcp.GCSConfig
		//    s3: aws.S3Config
		//    swift: openstack.SwiftConfig
		//    filesystem: Filesystem
		//    bos: baidubce.BOSStorageConfig

		t.Run("does not automatically configure cloud object storage", func(t *testing.T) {
			config, defaults := testContext(emptyConfigString, nil)

			assert.EqualValues(t, defaults.Ruler.StoreConfig.Type, config.Ruler.StoreConfig.Type)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.Azure, config.Ruler.StoreConfig.Azure)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.GCS, config.Ruler.StoreConfig.GCS)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.S3, config.Ruler.StoreConfig.S3)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.Swift, config.Ruler.StoreConfig.Swift)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.Local, config.Ruler.StoreConfig.Local)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.BOS, config.Ruler.StoreConfig.BOS)
			assert.EqualValues(t, defaults.StorageConfig.AWSStorageConfig, config.StorageConfig.AWSStorageConfig)
			assert.EqualValues(t, defaults.StorageConfig.AzureStorageConfig, config.StorageConfig.AzureStorageConfig)
			assert.EqualValues(t, defaults.StorageConfig.GCSConfig, config.StorageConfig.GCSConfig)
			assert.EqualValues(t, defaults.StorageConfig.Swift, config.StorageConfig.Swift)
			assert.EqualValues(t, defaults.StorageConfig.FSConfig, config.StorageConfig.FSConfig)
			assert.EqualValues(t, defaults.StorageConfig.BOSStorageConfig, config.StorageConfig.BOSStorageConfig)
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

		t.Run("when common s3 storage config is provided (with empty session token), ruler and storage config are defaulted to use it", func(t *testing.T) {
			s3Config := `common:
  storage:
    s3:
      s3: s3://foo-bucket/example
      endpoint: s3://foo-bucket
      region: us-east1
      access_key_id: abc123
      secret_access_key: def789
      insecure: true
      disable_dualstack: true
      http_config:
        response_header_timeout: 5m`

			config, defaults := testContext(s3Config, nil)

			expected, err := url.Parse("s3://foo-bucket/example")
			require.NoError(t, err)

			assert.Equal(t, "s3", config.Ruler.StoreConfig.Type)

			for _, actual := range []aws.S3Config{
				config.Ruler.StoreConfig.S3,
				config.StorageConfig.AWSStorageConfig.S3Config,
			} {
				require.NotNil(t, actual.S3.URL)
				assert.Equal(t, *expected, *actual.S3.URL)

				assert.Equal(t, false, actual.S3ForcePathStyle)
				assert.Equal(t, "s3://foo-bucket", actual.Endpoint)
				assert.Equal(t, "us-east1", actual.Region)
				assert.Equal(t, "abc123", actual.AccessKeyID)
				assert.Equal(t, "def789", actual.SecretAccessKey.String())
				assert.Equal(t, "", actual.SessionToken.String())
				assert.Equal(t, true, actual.Insecure)
				assert.True(t, actual.DisableDualstack)
				assert.Equal(t, 5*time.Minute, actual.HTTPConfig.ResponseHeaderTimeout)
				assert.Equal(t, false, actual.HTTPConfig.InsecureSkipVerify)

				assert.Equal(t, aws.SignatureVersionV4, actual.SignatureVersion,
					"signature version should equal default value")
				assert.Equal(t, 90*time.Second, actual.HTTPConfig.IdleConnTimeout,
					"idle connection timeout should equal default value")
			}

			// should remain empty
			assert.EqualValues(t, defaults.Ruler.StoreConfig.Azure, config.Ruler.StoreConfig.Azure)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.GCS, config.Ruler.StoreConfig.GCS)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.Swift, config.Ruler.StoreConfig.Swift)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.Local, config.Ruler.StoreConfig.Local)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.BOS, config.Ruler.StoreConfig.BOS)
			// should remain empty
			assert.EqualValues(t, defaults.StorageConfig.AzureStorageConfig, config.StorageConfig.AzureStorageConfig)
			assert.EqualValues(t, defaults.StorageConfig.GCSConfig, config.StorageConfig.GCSConfig)
			assert.EqualValues(t, defaults.StorageConfig.Swift, config.StorageConfig.Swift)
			assert.EqualValues(t, defaults.StorageConfig.FSConfig, config.StorageConfig.FSConfig)
			assert.EqualValues(t, defaults.StorageConfig.BOSStorageConfig, config.StorageConfig.BOSStorageConfig)
		})

		t.Run("when common s3 storage config is provided (with session token), ruler and storage config are defaulted to use it", func(t *testing.T) {
			s3Config := `common:
  storage:
    s3:
      s3: s3://foo-bucket/example
      endpoint: s3://foo-bucket
      region: us-east1
      access_key_id: abc123
      secret_access_key: def789
      session_token: 456abc
      insecure: true
      disable_dualstack: false
      http_config:
        response_header_timeout: 5m`

			config, defaults := testContext(s3Config, nil)

			expected, err := url.Parse("s3://foo-bucket/example")
			require.NoError(t, err)

			assert.Equal(t, "s3", config.Ruler.StoreConfig.Type)

			for _, actual := range []aws.S3Config{
				config.Ruler.StoreConfig.S3,
				config.StorageConfig.AWSStorageConfig.S3Config,
			} {
				require.NotNil(t, actual.S3.URL)
				assert.Equal(t, *expected, *actual.S3.URL)

				assert.Equal(t, false, actual.S3ForcePathStyle)
				assert.Equal(t, "s3://foo-bucket", actual.Endpoint)
				assert.Equal(t, "us-east1", actual.Region)
				assert.Equal(t, "abc123", actual.AccessKeyID)
				assert.Equal(t, "def789", actual.SecretAccessKey.String())
				assert.Equal(t, "456abc", actual.SessionToken.String())
				assert.Equal(t, true, actual.Insecure)
				assert.False(t, actual.DisableDualstack)
				assert.Equal(t, 5*time.Minute, actual.HTTPConfig.ResponseHeaderTimeout)
				assert.Equal(t, false, actual.HTTPConfig.InsecureSkipVerify)

				assert.Equal(t, aws.SignatureVersionV4, actual.SignatureVersion,
					"signature version should equal default value")
				assert.Equal(t, 90*time.Second, actual.HTTPConfig.IdleConnTimeout,
					"idle connection timeout should equal default value")
			}

			// should remain empty
			assert.EqualValues(t, defaults.Ruler.StoreConfig.Azure, config.Ruler.StoreConfig.Azure)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.GCS, config.Ruler.StoreConfig.GCS)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.Swift, config.Ruler.StoreConfig.Swift)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.Local, config.Ruler.StoreConfig.Local)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.BOS, config.Ruler.StoreConfig.BOS)
			// should remain empty
			assert.EqualValues(t, defaults.StorageConfig.AzureStorageConfig, config.StorageConfig.AzureStorageConfig)
			assert.EqualValues(t, defaults.StorageConfig.GCSConfig, config.StorageConfig.GCSConfig)
			assert.EqualValues(t, defaults.StorageConfig.Swift, config.StorageConfig.Swift)
			assert.EqualValues(t, defaults.StorageConfig.FSConfig, config.StorageConfig.FSConfig)
			assert.EqualValues(t, defaults.StorageConfig.BOSStorageConfig, config.StorageConfig.BOSStorageConfig)
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

			for _, actual := range []gcp.GCSConfig{
				config.Ruler.StoreConfig.GCS,
				config.StorageConfig.GCSConfig,
			} {
				assert.Equal(t, "foobar", actual.BucketName)
				assert.Equal(t, 27, actual.ChunkBufferSize)
				assert.Equal(t, 5*time.Minute, actual.RequestTimeout)
				assert.Equal(t, true, actual.EnableOpenCensus, "should get default value for unspecified oc config")
			}

			// should remain empty
			assert.EqualValues(t, defaults.Ruler.StoreConfig.Azure, config.Ruler.StoreConfig.Azure)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.S3, config.Ruler.StoreConfig.S3)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.Swift, config.Ruler.StoreConfig.Swift)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.Local, config.Ruler.StoreConfig.Local)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.BOS, config.Ruler.StoreConfig.BOS)
			// should remain empty
			assert.EqualValues(t, defaults.StorageConfig.AzureStorageConfig, config.StorageConfig.AzureStorageConfig)
			assert.EqualValues(t, defaults.StorageConfig.AWSStorageConfig.S3Config, config.StorageConfig.AWSStorageConfig.S3Config)
			assert.EqualValues(t, defaults.StorageConfig.Swift, config.StorageConfig.Swift)
			assert.EqualValues(t, defaults.StorageConfig.FSConfig, config.StorageConfig.FSConfig)
			assert.EqualValues(t, defaults.StorageConfig.BOSStorageConfig, config.StorageConfig.BOSStorageConfig)
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

			for _, actual := range []azure.BlobStorageConfig{
				config.Ruler.StoreConfig.Azure,
				config.StorageConfig.AzureStorageConfig,
			} {
				assert.Equal(t, "AzureGlobal", actual.Environment,
					"should equal default environment since unspecified in config")

				assert.Equal(t, "milkyway", actual.ContainerName)
				assert.Equal(t, "3rd_planet", actual.StorageAccountName)
				assert.Equal(t, "water", actual.StorageAccountKey.String())
				assert.Equal(t, 27, actual.DownloadBufferSize)
				assert.Equal(t, 42, actual.UploadBufferSize)
				assert.Equal(t, 13, actual.UploadBufferCount)
				assert.Equal(t, 5*time.Minute, actual.RequestTimeout)
				assert.Equal(t, 3, actual.MaxRetries)
				assert.Equal(t, 10*time.Second, actual.MinRetryDelay)
				assert.Equal(t, 10*time.Minute, actual.MaxRetryDelay)
			}

			// should remain empty
			assert.EqualValues(t, defaults.Ruler.StoreConfig.GCS, config.Ruler.StoreConfig.GCS)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.S3, config.Ruler.StoreConfig.S3)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.Swift, config.Ruler.StoreConfig.Swift)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.Local, config.Ruler.StoreConfig.Local)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.BOS, config.Ruler.StoreConfig.BOS)

			// should remain empty
			assert.EqualValues(t, defaults.StorageConfig.GCSConfig, config.StorageConfig.GCSConfig)
			assert.EqualValues(t, defaults.StorageConfig.AWSStorageConfig.S3Config, config.StorageConfig.AWSStorageConfig.S3Config)
			assert.EqualValues(t, defaults.StorageConfig.Swift, config.StorageConfig.Swift)
			assert.EqualValues(t, defaults.StorageConfig.FSConfig, config.StorageConfig.FSConfig)
			assert.EqualValues(t, defaults.StorageConfig.BOSStorageConfig, config.StorageConfig.BOSStorageConfig)
		})

		t.Run("when common bos storage config is provided, ruler and storage config are defaulted to use it", func(t *testing.T) {
			bosConfig := `common:
  storage:
    bos:
      bucket_name: arcosx
      endpoint: bj.bcebos.com
      access_key_id: baidu
      secret_access_key: bce`

			config, defaults := testContext(bosConfig, nil)

			assert.Equal(t, "bos", config.Ruler.StoreConfig.Type)

			for _, actual := range []baidubce.BOSStorageConfig{
				config.Ruler.StoreConfig.BOS,
				config.StorageConfig.BOSStorageConfig,
			} {
				assert.Equal(t, "arcosx", actual.BucketName)
				assert.Equal(t, "bj.bcebos.com", actual.Endpoint)
				assert.Equal(t, "baidu", actual.AccessKeyID)
				assert.Equal(t, "bce", actual.SecretAccessKey.String())
			}

			// should remain empty
			assert.EqualValues(t, defaults.Ruler.StoreConfig.Azure, config.Ruler.StoreConfig.Azure)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.GCS, config.Ruler.StoreConfig.GCS)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.S3, config.Ruler.StoreConfig.S3)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.Swift, config.Ruler.StoreConfig.Swift)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.Local, config.Ruler.StoreConfig.Local)

			// should remain empty
			assert.EqualValues(t, defaults.StorageConfig.AzureStorageConfig, config.StorageConfig.AzureStorageConfig)
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

			for _, actual := range []swift.Config{
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

			// should remain empty
			assert.EqualValues(t, defaults.Ruler.StoreConfig.GCS, config.Ruler.StoreConfig.GCS)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.S3, config.Ruler.StoreConfig.S3)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.Azure, config.Ruler.StoreConfig.Azure)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.Local, config.Ruler.StoreConfig.Local)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.BOS, config.Ruler.StoreConfig.BOS)
			// should remain empty
			assert.EqualValues(t, defaults.StorageConfig.GCSConfig, config.StorageConfig.GCSConfig)
			assert.EqualValues(t, defaults.StorageConfig.AWSStorageConfig.S3Config, config.StorageConfig.AWSStorageConfig.S3Config)
			assert.EqualValues(t, defaults.StorageConfig.AzureStorageConfig, config.StorageConfig.AzureStorageConfig)
			assert.EqualValues(t, defaults.StorageConfig.FSConfig, config.StorageConfig.FSConfig)
			assert.EqualValues(t, defaults.StorageConfig.BOSStorageConfig, config.StorageConfig.BOSStorageConfig)
		})

		t.Run("when common filesystem/local config is provided, ruler and storage config are defaulted to use it", func(t *testing.T) {
			fsConfig := `common:
  storage:
    filesystem:
      chunks_directory: /tmp/chunks
      rules_directory: /tmp/rules`

			config, defaults := testContext(fsConfig, nil)

			assert.Equal(t, "local", config.Ruler.StoreConfig.Type)

			assert.Equal(t, "/tmp/rules", config.Ruler.StoreConfig.Local.Directory)
			assert.Equal(t, "/tmp/chunks", config.StorageConfig.FSConfig.Directory)

			// should remain empty
			assert.EqualValues(t, defaults.Ruler.StoreConfig.GCS, config.Ruler.StoreConfig.GCS)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.S3, config.Ruler.StoreConfig.S3)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.Azure, config.Ruler.StoreConfig.Azure)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.Swift, config.Ruler.StoreConfig.Swift)
			assert.EqualValues(t, defaults.Ruler.StoreConfig.BOS, config.Ruler.StoreConfig.BOS)
			// should remain empty
			assert.EqualValues(t, defaults.StorageConfig.GCSConfig, config.StorageConfig.GCSConfig)
			assert.EqualValues(t, defaults.StorageConfig.AWSStorageConfig.S3Config, config.StorageConfig.AWSStorageConfig.S3Config)
			assert.EqualValues(t, defaults.StorageConfig.AzureStorageConfig, config.StorageConfig.AzureStorageConfig)
			assert.EqualValues(t, defaults.StorageConfig.Swift, config.StorageConfig.Swift)
			assert.EqualValues(t, defaults.StorageConfig.BOSStorageConfig, config.StorageConfig.BOSStorageConfig)
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
			assert.Equal(t, "def789", config.Ruler.StoreConfig.S3.SecretAccessKey.String())

			// should be set by common config
			assert.EqualValues(t, "foobar", config.StorageConfig.GCSConfig.BucketName)
			assert.EqualValues(t, 27, config.StorageConfig.GCSConfig.ChunkBufferSize)
			assert.EqualValues(t, 5*time.Minute, config.StorageConfig.GCSConfig.RequestTimeout)

			// should remain empty
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
			assert.Equal(t, "def789", config.StorageConfig.AWSStorageConfig.S3Config.SecretAccessKey.String())

			// should be set by common config
			assert.EqualValues(t, "foobar", config.Ruler.StoreConfig.GCS.BucketName)
			assert.EqualValues(t, 27, config.Ruler.StoreConfig.GCS.ChunkBufferSize)
			assert.EqualValues(t, 5*time.Minute, config.Ruler.StoreConfig.GCS.RequestTimeout)

			// should remain empty
			assert.EqualValues(t, defaults.Ruler.StoreConfig.S3, config.Ruler.StoreConfig.S3)
		})

		t.Run("named storage config provided via config file is preserved", func(t *testing.T) {
			namedStoresConfig := `common:
  storage:
    s3:
      endpoint: s3://common-bucket
      region: us-east1
      access_key_id: abc123
      secret_access_key: def789
storage_config:
  named_stores:
    aws:
      store-1:
        endpoint: s3://foo-bucket
        region: us-west1
        access_key_id: 123abc
        secret_access_key: 789def
      store-2:
        endpoint: s3://bar-bucket
        region: us-west2
        access_key_id: 456def
        secret_access_key: 789abc`
			config, _ := testContext(namedStoresConfig, nil)

			// should be set by common config
			assert.Equal(t, "s3://common-bucket", config.StorageConfig.AWSStorageConfig.S3Config.Endpoint)
			assert.Equal(t, "us-east1", config.StorageConfig.AWSStorageConfig.S3Config.Region)
			assert.Equal(t, "abc123", config.StorageConfig.AWSStorageConfig.S3Config.AccessKeyID)
			assert.Equal(t, "def789", config.StorageConfig.AWSStorageConfig.S3Config.SecretAccessKey.String())

			assert.Equal(t, "s3://foo-bucket", config.StorageConfig.NamedStores.AWS["store-1"].S3Config.Endpoint)
			assert.Equal(t, "us-west1", config.StorageConfig.NamedStores.AWS["store-1"].S3Config.Region)
			assert.Equal(t, "123abc", config.StorageConfig.NamedStores.AWS["store-1"].S3Config.AccessKeyID)
			assert.Equal(t, "789def", config.StorageConfig.NamedStores.AWS["store-1"].S3Config.SecretAccessKey.String())

			assert.Equal(t, "s3://bar-bucket", config.StorageConfig.NamedStores.AWS["store-2"].S3Config.Endpoint)
			assert.Equal(t, "us-west2", config.StorageConfig.NamedStores.AWS["store-2"].S3Config.Region)
			assert.Equal(t, "456def", config.StorageConfig.NamedStores.AWS["store-2"].S3Config.AccessKeyID)
			assert.Equal(t, "789abc", config.StorageConfig.NamedStores.AWS["store-2"].S3Config.SecretAccessKey.String())
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

			// from common config
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

			// from common config
			assert.EqualValues(t, 27, config.StorageConfig.GCSConfig.ChunkBufferSize)
			assert.EqualValues(t, 5*time.Minute, config.StorageConfig.GCSConfig.RequestTimeout)
		})
	})

	t.Run("boltdb shipper apply common path prefix", func(t *testing.T) {
		t.Run("if path prefix provided in common config, default active_index_directory and cache_location", func(t *testing.T) {

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
			config, _ := testContext(boltdbSchemaConfig, nil)

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
			config, _ := testContext(boltdbSchemaConfig, nil)

			assert.Equal(t, "/opt/loki/boltdb-shipper-active", config.StorageConfig.BoltDBShipperConfig.ActiveIndexDirectory)
			assert.Equal(t, "/opt/loki/boltdb-shipper-cache", config.StorageConfig.BoltDBShipperConfig.CacheLocation)

		})
	})

	t.Run("ingester final sleep config", func(t *testing.T) {
		t.Run("defaults to 0s", func(t *testing.T) {
			config, _ := testContext(emptyConfigString, nil)

			assert.Equal(t, 0*time.Second, config.Ingester.LifecyclerConfig.FinalSleep)
		})

		t.Run("honors values from config file and command line", func(t *testing.T) {
			config, _ := testContext(emptyConfigString, []string{"--ingester.final-sleep", "5s"})
			assert.Equal(t, 5*time.Second, config.Ingester.LifecyclerConfig.FinalSleep)

			const finalSleepConfig = `---
ingester:
  lifecycler:
    final_sleep: 12s`

			config, _ = testContext(finalSleepConfig, nil)
			assert.Equal(t, 12*time.Second, config.Ingester.LifecyclerConfig.FinalSleep)
		})
	})

	t.Run("embedded-cache setting is applied to result caches", func(t *testing.T) {
		configFileString := `---
query_range:
  results_cache:
    cache:
      embedded_cache:
        enabled: true`

		config, _ := testContext(configFileString, nil)
		assert.True(t, config.QueryRange.ResultsCacheConfig.CacheConfig.EmbeddedCache.Enabled)
	})

	t.Run("querier worker grpc client behavior", func(t *testing.T) {
		newConfigBothClientsSet := `---
frontend_worker:
  query_frontend_grpc_client:
    tls_server_name: query-frontend
  query_scheduler_grpc_client:
    tls_server_name: query-scheduler
`

		oldConfig := `---
frontend_worker:
  grpc_client_config:
    tls_server_name: query-frontend
`

		mixedConfig := `---
frontend_worker:
  grpc_client_config:
    tls_server_name: query-frontend-old
  query_frontend_grpc_client:
    tls_server_name: query-frontend-new
  query_scheduler_grpc_client:
    tls_server_name: query-scheduler
`
		t.Run("new configs are used", func(t *testing.T) {
			asserts := func(config ConfigWrapper) {
				require.EqualValues(t, "query-frontend", config.Worker.NewQueryFrontendGRPCClientConfig.TLS.ServerName)
				require.EqualValues(t, "query-scheduler", config.Worker.QuerySchedulerGRPCClientConfig.TLS.ServerName)
				// we never want to use zero values by default.
				require.NotEqualValues(t, 0, config.Worker.NewQueryFrontendGRPCClientConfig.MaxRecvMsgSize)
				require.NotEqualValues(t, 0, config.Worker.QuerySchedulerGRPCClientConfig.MaxRecvMsgSize)
			}

			yamlConfig, _, err := configWrapperFromYAML(t, newConfigBothClientsSet, nil)
			require.NoError(t, err)
			asserts(yamlConfig)

			// repeat the test using only cli flags.
			cliFlags := []string{
				"-querier.frontend-grpc-client.tls-server-name=query-frontend",
				"-querier.scheduler-grpc-client.tls-server-name=query-scheduler",
			}
			cliConfig, _, err := configWrapperFromYAML(t, emptyConfigString, cliFlags)
			require.NoError(t, err)
			asserts(cliConfig)
		})

		t.Run("old config works the same way", func(t *testing.T) {
			asserts := func(config ConfigWrapper) {
				require.EqualValues(t, "query-frontend", config.Worker.NewQueryFrontendGRPCClientConfig.TLS.ServerName)
				require.EqualValues(t, "query-frontend", config.Worker.QuerySchedulerGRPCClientConfig.TLS.ServerName)

				// we never want to use zero values by default.
				require.NotEqualValues(t, 0, config.Worker.NewQueryFrontendGRPCClientConfig.MaxRecvMsgSize)
				require.NotEqualValues(t, 0, config.Worker.QuerySchedulerGRPCClientConfig.MaxRecvMsgSize)
			}

			yamlConfig, _, err := configWrapperFromYAML(t, oldConfig, nil)
			require.NoError(t, err)
			asserts(yamlConfig)

			// repeat the test using only cli flags.
			cliFlags := []string{
				"-querier.frontend-client.tls-server-name=query-frontend",
			}
			cliConfig, _, err := configWrapperFromYAML(t, emptyConfigString, cliFlags)
			require.NoError(t, err)
			asserts(cliConfig)
		})

		t.Run("mixed frontend clients throws an error", func(t *testing.T) {
			_, _, err := configWrapperFromYAML(t, mixedConfig, nil)
			require.Error(t, err)

			// repeat the test using only cli flags.
			_, _, err = configWrapperFromYAML(t, emptyConfigString, []string{
				"-querier.frontend-client.tls-server-name=query-frontend",
				"-querier.frontend-grpc-client.tls-server-name=query-frontend",
			})
			require.Error(t, err)

			// repeat the test mixing the YAML with cli flags.
			_, _, err = configWrapperFromYAML(t, newConfigBothClientsSet, []string{
				"-querier.frontend-client.tls-server-name=query-frontend",
			})
			require.Error(t, err)
		})

		t.Run("mix correct cli flags with YAML configs", func(t *testing.T) {
			config, _, err := configWrapperFromYAML(t, newConfigBothClientsSet, []string{
				"-querier.scheduler-grpc-client.tls-enabled=true",
			})
			require.NoError(t, err)

			require.EqualValues(t, "query-frontend", config.Worker.NewQueryFrontendGRPCClientConfig.TLS.ServerName)
			require.EqualValues(t, "query-scheduler", config.Worker.QuerySchedulerGRPCClientConfig.TLS.ServerName)
			// we never want to use zero values by default.
			require.NotEqualValues(t, 0, config.Worker.NewQueryFrontendGRPCClientConfig.MaxRecvMsgSize)
			require.NotEqualValues(t, 0, config.Worker.QuerySchedulerGRPCClientConfig.MaxRecvMsgSize)
			require.True(t, config.Worker.QuerySchedulerGRPCClientConfig.TLSEnabled)
		})
	})
}

const defaultResulsCacheString = `---
query_range:
  results_cache:
    cache:
      memcached_client:
        host: memcached.host.org`

func TestDefaultEmbeddedCacheBehavior(t *testing.T) {
	t.Run("for the chunk cache config", func(t *testing.T) {
		t.Run("no embedded cache enabled by default if Redis is set", func(t *testing.T) {
			configFileString := `---
chunk_store_config:
  chunk_cache_config:
    redis:
      endpoint: endpoint.redis.org`

			config, _, _ := configWrapperFromYAML(t, configFileString, nil)
			assert.EqualValues(t, "endpoint.redis.org", config.ChunkStoreConfig.ChunkCacheConfig.Redis.Endpoint)
			assert.False(t, config.ChunkStoreConfig.ChunkCacheConfig.EmbeddedCache.Enabled)
		})

		t.Run("no embedded cache enabled by default if Memcache is set", func(t *testing.T) {
			configFileString := `---
chunk_store_config:
  chunk_cache_config:
    memcached_client:
      host: host.memcached.org`

			config, _, _ := configWrapperFromYAML(t, configFileString, nil)
			assert.EqualValues(t, "host.memcached.org", config.ChunkStoreConfig.ChunkCacheConfig.MemcacheClient.Host)
			assert.False(t, config.ChunkStoreConfig.ChunkCacheConfig.EmbeddedCache.Enabled)
		})

		t.Run("embedded cache is enabled by default if no other cache is set", func(t *testing.T) {
			config, _, _ := configWrapperFromYAML(t, minimalConfig, nil)
			assert.True(t, config.ChunkStoreConfig.ChunkCacheConfig.EmbeddedCache.Enabled)
		})
	})

	t.Run("for the write dedupe cache config", func(t *testing.T) {
		t.Run("no embedded cache enabled by default if Redis is set", func(t *testing.T) {
			configFileString := `---
chunk_store_config:
  write_dedupe_cache_config:
    redis:
      endpoint: endpoint.redis.org`

			config, _, _ := configWrapperFromYAML(t, configFileString, nil)
			assert.EqualValues(t, "endpoint.redis.org", config.ChunkStoreConfig.WriteDedupeCacheConfig.Redis.Endpoint)
			assert.False(t, config.ChunkStoreConfig.WriteDedupeCacheConfig.EmbeddedCache.Enabled)
		})

		t.Run("no embedded cache enabled by default if Memcache is set", func(t *testing.T) {
			configFileString := `---
chunk_store_config:
  write_dedupe_cache_config:
    memcached_client:
      host: host.memcached.org`

			config, _, _ := configWrapperFromYAML(t, configFileString, nil)
			assert.EqualValues(t, "host.memcached.org", config.ChunkStoreConfig.WriteDedupeCacheConfig.MemcacheClient.Host)
			assert.False(t, config.ChunkStoreConfig.WriteDedupeCacheConfig.EmbeddedCache.Enabled)
		})

		t.Run("no embedded cache is enabled by default even if no other cache is set", func(t *testing.T) {
			config, _, _ := configWrapperFromYAML(t, minimalConfig, nil)
			assert.False(t, config.ChunkStoreConfig.WriteDedupeCacheConfig.EmbeddedCache.Enabled)
		})
	})

	t.Run("for the index queries cache config", func(t *testing.T) {
		t.Run("no embedded cache enabled by default if Redis is set", func(t *testing.T) {
			configFileString := `---
schema_config:
  configs:
    - from: 2020-10-24
      store: boltdb-shipper
      object_store: filesystem
      schema: v12
      index:
        prefix: index_
        period: 24h
storage_config:
  index_queries_cache_config:
    redis:
      endpoint: endpoint.redis.org`

			config, _, _ := configWrapperFromYAML(t, configFileString, nil)
			assert.EqualValues(t, "endpoint.redis.org", config.StorageConfig.IndexQueriesCacheConfig.Redis.Endpoint)
			assert.False(t, config.StorageConfig.IndexQueriesCacheConfig.EmbeddedCache.Enabled)
		})

		t.Run("no embedded cache enabled by default if Memcache is set", func(t *testing.T) {
			configFileString := `---
schema_config:
  configs:
    - from: 2020-10-24
      store: boltdb-shipper
      object_store: filesystem
      schema: v12
      index:
        prefix: index_
        period: 24h
storage_config:
  index_queries_cache_config:
    memcached_client:
      host: host.memcached.org`

			config, _, _ := configWrapperFromYAML(t, configFileString, nil)

			assert.EqualValues(t, "host.memcached.org", config.StorageConfig.IndexQueriesCacheConfig.MemcacheClient.Host)
			assert.False(t, config.StorageConfig.IndexQueriesCacheConfig.EmbeddedCache.Enabled)
		})

		t.Run("no embedded cache is enabled by default even if no other cache is set", func(t *testing.T) {
			config, _, _ := configWrapperFromYAML(t, minimalConfig, nil)
			assert.False(t, config.StorageConfig.IndexQueriesCacheConfig.EmbeddedCache.Enabled)
		})
	})

	t.Run("for the query range results cache config", func(t *testing.T) {
		t.Run("no embedded cache enabled by default if Redis is set", func(t *testing.T) {
			configFileString := `---
query_range:
  results_cache:
    cache:
      redis:
        endpoint: endpoint.redis.org`

			config, _, _ := configWrapperFromYAML(t, configFileString, nil)
			assert.EqualValues(t, config.QueryRange.ResultsCacheConfig.CacheConfig.Redis.Endpoint, "endpoint.redis.org")
			assert.False(t, config.QueryRange.ResultsCacheConfig.CacheConfig.EmbeddedCache.Enabled)
		})

		t.Run("no embedded cache enabled by default if Memcache is set", func(t *testing.T) {
			config, _, _ := configWrapperFromYAML(t, defaultResulsCacheString, nil)
			assert.EqualValues(t, "memcached.host.org", config.QueryRange.ResultsCacheConfig.CacheConfig.MemcacheClient.Host)
			assert.False(t, config.QueryRange.ResultsCacheConfig.CacheConfig.EmbeddedCache.Enabled)
		})

		t.Run("embedded cache is enabled by default if no other cache is set", func(t *testing.T) {
			config, _, _ := configWrapperFromYAML(t, minimalConfig, nil)
			assert.True(t, config.QueryRange.ResultsCacheConfig.CacheConfig.EmbeddedCache.Enabled)
		})
	})

	t.Run("for the index stats results cache config", func(t *testing.T) {
		t.Run("no embedded cache enabled by default if Redis is set", func(t *testing.T) {
			configFileString := `---
query_range:
  index_stats_results_cache:
    cache:
      redis:
        endpoint: endpoint.redis.org`

			config, _, _ := configWrapperFromYAML(t, configFileString, nil)
			assert.EqualValues(t, config.QueryRange.StatsCacheConfig.CacheConfig.Redis.Endpoint, "endpoint.redis.org")
			assert.EqualValues(t, "frontend.index-stats-results-cache.", config.QueryRange.StatsCacheConfig.CacheConfig.Prefix)
			assert.False(t, config.QueryRange.StatsCacheConfig.CacheConfig.EmbeddedCache.Enabled)
		})

		t.Run("no embedded cache enabled by default if Memcache is set", func(t *testing.T) {
			configFileString := `---
query_range:
  index_stats_results_cache:
    cache:
      memcached_client:
        host: memcached.host.org`

			config, _, _ := configWrapperFromYAML(t, configFileString, nil)
			assert.EqualValues(t, "memcached.host.org", config.QueryRange.StatsCacheConfig.CacheConfig.MemcacheClient.Host)
			assert.EqualValues(t, "frontend.index-stats-results-cache.", config.QueryRange.StatsCacheConfig.CacheConfig.Prefix)
			assert.False(t, config.QueryRange.StatsCacheConfig.CacheConfig.EmbeddedCache.Enabled)
		})

		t.Run("embedded cache is enabled by default if no other cache is set", func(t *testing.T) {
			config, _, _ := configWrapperFromYAML(t, minimalConfig, nil)
			assert.EqualValues(t, "frontend.index-stats-results-cache.", config.QueryRange.StatsCacheConfig.CacheConfig.Prefix)
			assert.True(t, config.QueryRange.StatsCacheConfig.CacheConfig.EmbeddedCache.Enabled)
		})

		t.Run("gets results cache config if not configured directly", func(t *testing.T) {
			config, _, _ := configWrapperFromYAML(t, defaultResulsCacheString, nil)
			assert.EqualValues(t, "memcached.host.org", config.QueryRange.StatsCacheConfig.CacheConfig.MemcacheClient.Host)
			assert.EqualValues(t, "frontend.index-stats-results-cache.", config.QueryRange.StatsCacheConfig.CacheConfig.Prefix)
			assert.False(t, config.QueryRange.StatsCacheConfig.CacheConfig.EmbeddedCache.Enabled)
		})
	})

	t.Run("for the volume results cache config", func(t *testing.T) {
		t.Run("no embedded cache enabled by default if Redis is set", func(t *testing.T) {
			configFileString := `---
query_range:
  volume_results_cache:
    cache:
      redis:
        endpoint: endpoint.redis.org`

			config, _, _ := configWrapperFromYAML(t, configFileString, nil)
			assert.EqualValues(t, config.QueryRange.VolumeCacheConfig.CacheConfig.Redis.Endpoint, "endpoint.redis.org")
			assert.EqualValues(t, "frontend.volume-results-cache.", config.QueryRange.VolumeCacheConfig.CacheConfig.Prefix)
			assert.False(t, config.QueryRange.VolumeCacheConfig.CacheConfig.EmbeddedCache.Enabled)
		})

		t.Run("no embedded cache enabled by default if Memcache is set", func(t *testing.T) {
			configFileString := `---
query_range:
  volume_results_cache:
    cache:
      memcached_client:
        host: memcached.host.org`

			config, _, _ := configWrapperFromYAML(t, configFileString, nil)
			assert.EqualValues(t, "memcached.host.org", config.QueryRange.VolumeCacheConfig.CacheConfig.MemcacheClient.Host)
			assert.EqualValues(t, "frontend.volume-results-cache.", config.QueryRange.VolumeCacheConfig.CacheConfig.Prefix)
			assert.False(t, config.QueryRange.VolumeCacheConfig.CacheConfig.EmbeddedCache.Enabled)
		})

		t.Run("embedded cache is enabled by default if no other cache is set", func(t *testing.T) {
			config, _, _ := configWrapperFromYAML(t, minimalConfig, nil)
			assert.EqualValues(t, "frontend.volume-results-cache.", config.QueryRange.VolumeCacheConfig.CacheConfig.Prefix)
			assert.True(t, config.QueryRange.VolumeCacheConfig.CacheConfig.EmbeddedCache.Enabled)
		})

		t.Run("gets results cache config if not configured directly", func(t *testing.T) {
			config, _, _ := configWrapperFromYAML(t, defaultResulsCacheString, nil)
			assert.EqualValues(t, "memcached.host.org", config.QueryRange.VolumeCacheConfig.CacheConfig.MemcacheClient.Host)
			assert.EqualValues(t, "frontend.volume-results-cache.", config.QueryRange.VolumeCacheConfig.CacheConfig.Prefix)
			assert.False(t, config.QueryRange.VolumeCacheConfig.CacheConfig.EmbeddedCache.Enabled)
		})
	})

	t.Run("for the series results cache config", func(t *testing.T) {
		t.Run("no embedded cache enabled by default if Redis is set", func(t *testing.T) {
			configFileString := `---
query_range:
  series_results_cache:
    cache:
      redis:
        endpoint: endpoint.redis.org`

			config, _, _ := configWrapperFromYAML(t, configFileString, nil)
			assert.EqualValues(t, "endpoint.redis.org", config.QueryRange.SeriesCacheConfig.CacheConfig.Redis.Endpoint)
			assert.EqualValues(t, "frontend.series-results-cache.", config.QueryRange.SeriesCacheConfig.CacheConfig.Prefix)
			assert.False(t, config.QueryRange.SeriesCacheConfig.CacheConfig.EmbeddedCache.Enabled)
		})

		t.Run("no embedded cache enabled by default if Memcache is set", func(t *testing.T) {
			configFileString := `---
query_range:
  series_results_cache:
    cache:
      memcached_client:
        host: memcached.host.org`

			config, _, _ := configWrapperFromYAML(t, configFileString, nil)
			assert.EqualValues(t, "memcached.host.org", config.QueryRange.SeriesCacheConfig.CacheConfig.MemcacheClient.Host)
			assert.EqualValues(t, "frontend.series-results-cache.", config.QueryRange.SeriesCacheConfig.CacheConfig.Prefix)
			assert.False(t, config.QueryRange.SeriesCacheConfig.CacheConfig.EmbeddedCache.Enabled)
		})

		t.Run("embedded cache is enabled by default if no other cache is set", func(t *testing.T) {
			config, _, _ := configWrapperFromYAML(t, minimalConfig, nil)
			assert.True(t, config.QueryRange.SeriesCacheConfig.CacheConfig.EmbeddedCache.Enabled)
			assert.EqualValues(t, "frontend.series-results-cache.", config.QueryRange.SeriesCacheConfig.CacheConfig.Prefix)
		})

		t.Run("gets results cache config if not configured directly", func(t *testing.T) {
			config, _, _ := configWrapperFromYAML(t, defaultResulsCacheString, nil)
			assert.EqualValues(t, "memcached.host.org", config.QueryRange.SeriesCacheConfig.CacheConfig.MemcacheClient.Host)
			assert.EqualValues(t, "frontend.series-results-cache.", config.QueryRange.SeriesCacheConfig.CacheConfig.Prefix)
			assert.False(t, config.QueryRange.SeriesCacheConfig.CacheConfig.EmbeddedCache.Enabled)
		})
	})

	t.Run("for the instant-metric results cache config", func(t *testing.T) {
		t.Run("no embedded cache enabled by default if Redis is set", func(t *testing.T) {
			configFileString := `---
query_range:
  instant_metric_results_cache:
    cache:
      redis:
        endpoint: endpoint.redis.org`

			config, _, _ := configWrapperFromYAML(t, configFileString, nil)
			assert.EqualValues(t, "endpoint.redis.org", config.QueryRange.InstantMetricCacheConfig.CacheConfig.Redis.Endpoint)
			assert.EqualValues(t, "frontend.instant-metric-results-cache.", config.QueryRange.InstantMetricCacheConfig.CacheConfig.Prefix)
			assert.False(t, config.QueryRange.InstantMetricCacheConfig.CacheConfig.EmbeddedCache.Enabled)
		})

		t.Run("no embedded cache enabled by default if Memcache is set", func(t *testing.T) {
			configFileString := `---
query_range:
  instant_metric_results_cache:
    cache:
      memcached_client:
        host: memcached.host.org`

			config, _, _ := configWrapperFromYAML(t, configFileString, nil)
			assert.EqualValues(t, "memcached.host.org", config.QueryRange.InstantMetricCacheConfig.CacheConfig.MemcacheClient.Host)
			assert.EqualValues(t, "frontend.instant-metric-results-cache.", config.QueryRange.InstantMetricCacheConfig.CacheConfig.Prefix)
			assert.False(t, config.QueryRange.InstantMetricCacheConfig.CacheConfig.EmbeddedCache.Enabled)
		})

		t.Run("embedded cache is enabled by default if no other cache is set", func(t *testing.T) {
			config, _, _ := configWrapperFromYAML(t, minimalConfig, nil)
			assert.True(t, config.QueryRange.InstantMetricCacheConfig.CacheConfig.EmbeddedCache.Enabled)
			assert.EqualValues(t, "frontend.instant-metric-results-cache.", config.QueryRange.InstantMetricCacheConfig.CacheConfig.Prefix)
		})

		t.Run("gets results cache config if not configured directly", func(t *testing.T) {
			config, _, _ := configWrapperFromYAML(t, defaultResulsCacheString, nil)
			assert.EqualValues(t, "memcached.host.org", config.QueryRange.InstantMetricCacheConfig.CacheConfig.MemcacheClient.Host)
			assert.EqualValues(t, "frontend.instant-metric-results-cache.", config.QueryRange.InstantMetricCacheConfig.CacheConfig.Prefix)
			assert.False(t, config.QueryRange.InstantMetricCacheConfig.CacheConfig.EmbeddedCache.Enabled)
		})
	})

	t.Run("for the labels results cache config", func(t *testing.T) {
		t.Run("no embedded cache enabled by default if Redis is set", func(t *testing.T) {
			configFileString := `---
query_range:
  label_results_cache:
    cache:
      redis:
        endpoint: endpoint.redis.org`

			config, _, _ := configWrapperFromYAML(t, configFileString, nil)
			assert.EqualValues(t, "endpoint.redis.org", config.QueryRange.LabelsCacheConfig.CacheConfig.Redis.Endpoint)
			assert.EqualValues(t, "frontend.label-results-cache.", config.QueryRange.LabelsCacheConfig.CacheConfig.Prefix)
			assert.False(t, config.QueryRange.LabelsCacheConfig.CacheConfig.EmbeddedCache.Enabled)
		})

		t.Run("no embedded cache enabled by default if Memcache is set", func(t *testing.T) {
			configFileString := `---
query_range:
  label_results_cache:
    cache:
      memcached_client:
        host: memcached.host.org`

			config, _, _ := configWrapperFromYAML(t, configFileString, nil)
			assert.EqualValues(t, "memcached.host.org", config.QueryRange.LabelsCacheConfig.CacheConfig.MemcacheClient.Host)
			assert.EqualValues(t, "frontend.label-results-cache.", config.QueryRange.LabelsCacheConfig.CacheConfig.Prefix)
			assert.False(t, config.QueryRange.LabelsCacheConfig.CacheConfig.EmbeddedCache.Enabled)
		})

		t.Run("embedded cache is enabled by default if no other cache is set", func(t *testing.T) {
			config, _, _ := configWrapperFromYAML(t, minimalConfig, nil)
			assert.True(t, config.QueryRange.LabelsCacheConfig.CacheConfig.EmbeddedCache.Enabled)
			assert.EqualValues(t, "frontend.label-results-cache.", config.QueryRange.LabelsCacheConfig.CacheConfig.Prefix)
		})

		t.Run("gets results cache config if not configured directly", func(t *testing.T) {
			config, _, _ := configWrapperFromYAML(t, defaultResulsCacheString, nil)
			assert.EqualValues(t, "memcached.host.org", config.QueryRange.LabelsCacheConfig.CacheConfig.MemcacheClient.Host)
			assert.EqualValues(t, "frontend.label-results-cache.", config.QueryRange.LabelsCacheConfig.CacheConfig.Prefix)
			assert.False(t, config.QueryRange.LabelsCacheConfig.CacheConfig.EmbeddedCache.Enabled)
		})
	})
}

func TestDefaultUnmarshal(t *testing.T) {
	t.Run("with a minimal config file and no command line args, defaults are use", func(t *testing.T) {
		file, err := os.CreateTemp("", "config.yaml")
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
		assert.Equal(t, 3100, config.Server.HTTPListenPort)
		assert.Equal(t, 9095, config.Server.GRPCListenPort)
	})
}

func Test_applyIngesterRingConfig(t *testing.T) {
	t.Run("Attempt to catch changes to a RingConfig", func(t *testing.T) {
		msgf := "%s has changed, this is a crude attempt to catch mapping errors missed in config_wrapper.applyIngesterRingConfig when a ring config changes. Please add a new mapping and update the expected value in this test."

		assert.Equal(t, 9,
			reflect.TypeOf(distributor.RingConfig{}).NumField(),
			fmt.Sprintf(msgf, reflect.TypeOf(distributor.RingConfig{}).String()))
		assert.Equal(t, 15,
			reflect.TypeOf(lokiring.RingConfig{}).NumField(),
			fmt.Sprintf(msgf, reflect.TypeOf(lokiring.RingConfig{}).String()))
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
		assert.Equal(t, "", config.IndexGateway.Ring.TokensFilePath)
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
		assert.Equal(t, "/loki/indexgateway.tokens", config.IndexGateway.Ring.TokensFilePath)
	})

	t.Run("ingester config not applied to other rings if actual values set", func(t *testing.T) {
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
index_gateway:
  ring:
    tokens_file_path: /looki/tookens
common:
  persist_tokens: true
  path_prefix: /loki
`
		config, _, err := configWrapperFromYAML(t, yamlContent, []string{})
		assert.NoError(t, err)

		assert.Equal(t, "/loki/toookens", config.Ingester.LifecyclerConfig.TokensFilePath)
		assert.Equal(t, "/foo/tokens", config.CompactorConfig.CompactorRing.TokensFilePath)
		assert.Equal(t, "/sched/tokes", config.QueryScheduler.SchedulerRing.TokensFilePath)
		assert.Equal(t, "/looki/tookens", config.IndexGateway.Ring.TokensFilePath)
	})

	t.Run("ingester ring configuration is used for other rings when no common ring or memberlist config is provided", func(t *testing.T) {
		yamlContent := `
ingester:
  lifecycler:
    ring:
      kvstore:
        store: etcd`

		config, _, err := configWrapperFromYAML(t, yamlContent, []string{})
		assert.NoError(t, err)

		assert.Equal(t, "etcd", config.Distributor.DistributorRing.KVStore.Store)
		assert.Equal(t, "etcd", config.Ingester.LifecyclerConfig.RingConfig.KVStore.Store)
		assert.Equal(t, "etcd", config.Ruler.Ring.KVStore.Store)
		assert.Equal(t, "etcd", config.QueryScheduler.SchedulerRing.KVStore.Store)
		assert.Equal(t, "etcd", config.CompactorConfig.CompactorRing.KVStore.Store)
		assert.Equal(t, "etcd", config.IndexGateway.Ring.KVStore.Store)
	})

	t.Run("memberlist configuration takes precedence over copying ingester config", func(t *testing.T) {
		yamlContent := `
memberlist:
  join_members:
    - 127.0.0.1
ingester:
  lifecycler:
    ring:
      kvstore:
        store: etcd`

		config, _, err := configWrapperFromYAML(t, yamlContent, []string{})
		assert.NoError(t, err)

		assert.Equal(t, "etcd", config.Ingester.LifecyclerConfig.RingConfig.KVStore.Store)

		assert.Equal(t, "memberlist", config.Distributor.DistributorRing.KVStore.Store)
		assert.Equal(t, "memberlist", config.Ruler.Ring.KVStore.Store)
		assert.Equal(t, "memberlist", config.QueryScheduler.SchedulerRing.KVStore.Store)
		assert.Equal(t, "memberlist", config.CompactorConfig.CompactorRing.KVStore.Store)
		assert.Equal(t, "memberlist", config.IndexGateway.Ring.KVStore.Store)
	})
}

func TestRingInterfaceNames(t *testing.T) {
	defaultIface, err := loki_net.LoopbackInterfaceName()
	assert.NoError(t, err)
	assert.NotEmpty(t, defaultIface)

	t.Run("by default, loopback is available for all ring interfaces", func(t *testing.T) {
		config, _, err := configWrapperFromYAML(t, minimalConfig, []string{})
		assert.NoError(t, err)

		assert.Contains(t, config.Common.Ring.InstanceInterfaceNames, defaultIface)
		assert.Contains(t, config.Ingester.LifecyclerConfig.InfNames, defaultIface)
		assert.Contains(t, config.Distributor.DistributorRing.InstanceInterfaceNames, defaultIface)
		assert.Contains(t, config.QueryScheduler.SchedulerRing.InstanceInterfaceNames, defaultIface)
		assert.Contains(t, config.Ruler.Ring.InstanceInterfaceNames, defaultIface)
	})

	t.Run("if ingester interface is set, it overrides other rings default interfaces", func(t *testing.T) {
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
		expectedInterfaces := netutil.PrivateNetworkInterfacesWithFallback([]string{"eth0", "en0"}, util_log.Logger)
		expectedInterfaces = append(expectedInterfaces, defaultIface)
		assert.Equal(t, config.Ingester.LifecyclerConfig.InfNames, expectedInterfaces)
	})
}

func TestLoopbackAppendingToFrontendV2(t *testing.T) {
	defaultIface, err := loki_net.LoopbackInterfaceName()
	assert.NoError(t, err)
	assert.NotEmpty(t, defaultIface)

	t.Run("when using common or ingester ring configs, loopback should be added to interface names", func(t *testing.T) {
		config, _, err := configWrapperFromYAML(t, minimalConfig, []string{})
		assert.NoError(t, err)
		expectedInterfaces := netutil.PrivateNetworkInterfacesWithFallback([]string{"eth0", "en0"}, util_log.Logger)
		expectedInterfaces = append(expectedInterfaces, defaultIface)
		assert.Equal(t, expectedInterfaces, config.Frontend.FrontendV2.InfNames)
		assert.Equal(t, expectedInterfaces, config.Ingester.LifecyclerConfig.InfNames)
		assert.Equal(t, expectedInterfaces, config.Common.Ring.InstanceInterfaceNames)
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

func TestCommonRingConfigSection(t *testing.T) {
	t.Run("if only common ring is provided, reuse it for all rings", func(t *testing.T) {
		yamlContent := `common:
  ring:
    kvstore:
      store: etcd`

		config, _, err := configWrapperFromYAML(t, yamlContent, nil)
		assert.NoError(t, err)
		assert.Equal(t, "etcd", config.Distributor.DistributorRing.KVStore.Store)
		assert.Equal(t, "etcd", config.Ingester.LifecyclerConfig.RingConfig.KVStore.Store)
		assert.Equal(t, "etcd", config.Ruler.Ring.KVStore.Store)
		assert.Equal(t, "etcd", config.QueryScheduler.SchedulerRing.KVStore.Store)
		assert.Equal(t, "etcd", config.CompactorConfig.CompactorRing.KVStore.Store)
		assert.Equal(t, "etcd", config.IndexGateway.Ring.KVStore.Store)
	})

	t.Run("if common ring is provided, reuse it for all rings that aren't explicitly set", func(t *testing.T) {
		yamlContent := `common:
  ring:
    kvstore:
      store: etcd
ingester:
  lifecycler:
    ring:
      kvstore:
        store: inmemory`

		config, _, err := configWrapperFromYAML(t, yamlContent, nil)
		assert.NoError(t, err)
		assert.Equal(t, "inmemory", config.Ingester.LifecyclerConfig.RingConfig.KVStore.Store)

		assert.Equal(t, "etcd", config.Distributor.DistributorRing.KVStore.Store)
		assert.Equal(t, "etcd", config.Ruler.Ring.KVStore.Store)
		assert.Equal(t, "etcd", config.QueryScheduler.SchedulerRing.KVStore.Store)
		assert.Equal(t, "etcd", config.CompactorConfig.CompactorRing.KVStore.Store)
		assert.Equal(t, "etcd", config.IndexGateway.Ring.KVStore.Store)
	})

	t.Run("if only ingester ring is provided, reuse it for all rings", func(t *testing.T) {
		yamlContent := `ingester:
  lifecycler:
    ring:
      kvstore:
        store: etcd`
		config, _, err := configWrapperFromYAML(t, yamlContent, nil)
		assert.NoError(t, err)
		assert.Equal(t, "etcd", config.Distributor.DistributorRing.KVStore.Store)
		assert.Equal(t, "etcd", config.Ingester.LifecyclerConfig.RingConfig.KVStore.Store)
		assert.Equal(t, "etcd", config.Ruler.Ring.KVStore.Store)
		assert.Equal(t, "etcd", config.QueryScheduler.SchedulerRing.KVStore.Store)
		assert.Equal(t, "etcd", config.CompactorConfig.CompactorRing.KVStore.Store)
		assert.Equal(t, "etcd", config.IndexGateway.Ring.KVStore.Store)
	})

	t.Run("if a ring is explicitly configured, don't override any part of it with ingester config", func(t *testing.T) {
		yamlContent := `
distributor:
  ring:
    kvstore:
      store: etcd
ingester:
  lifecycler:
    heartbeat_period: 5m
    ring:
      kvstore:
        store: inmemory`

		config, defaults, err := configWrapperFromYAML(t, yamlContent, nil)
		assert.NoError(t, err)
		assert.Equal(t, "etcd", config.Distributor.DistributorRing.KVStore.Store)
		assert.Equal(t, defaults.Distributor.DistributorRing.HeartbeatPeriod, config.Distributor.DistributorRing.HeartbeatPeriod)

		assert.Equal(t, "inmemory", config.Ingester.LifecyclerConfig.RingConfig.KVStore.Store)
		assert.Equal(t, 5*time.Minute, config.Ingester.LifecyclerConfig.HeartbeatPeriod)

		assert.Equal(t, "inmemory", config.Ruler.Ring.KVStore.Store)
		assert.Equal(t, 5*time.Minute, config.Ruler.Ring.HeartbeatPeriod)

		assert.Equal(t, "inmemory", config.QueryScheduler.SchedulerRing.KVStore.Store)
		assert.Equal(t, 5*time.Minute, config.QueryScheduler.SchedulerRing.HeartbeatPeriod)

		assert.Equal(t, "inmemory", config.CompactorConfig.CompactorRing.KVStore.Store)
		assert.Equal(t, 5*time.Minute, config.CompactorConfig.CompactorRing.HeartbeatPeriod)

		assert.Equal(t, "inmemory", config.IndexGateway.Ring.KVStore.Store)
		assert.Equal(t, 5*time.Minute, config.IndexGateway.Ring.HeartbeatPeriod)
	})

	t.Run("if a ring is explicitly configured, merge common config with unconfigured parts of explicitly configured ring", func(t *testing.T) {
		yamlContent := `
common:
  ring:
    heartbeat_period: 5m
    kvstore:
      store: inmemory
distributor:
  ring:
    kvstore:
      store: etcd`

		config, _, err := configWrapperFromYAML(t, yamlContent, nil)
		assert.NoError(t, err)

		assert.Equal(t, "etcd", config.Distributor.DistributorRing.KVStore.Store)
		assert.Equal(t, 5*time.Minute, config.Distributor.DistributorRing.HeartbeatPeriod)

		assert.Equal(t, "inmemory", config.Ingester.LifecyclerConfig.RingConfig.KVStore.Store)
		assert.Equal(t, 5*time.Minute, config.Ingester.LifecyclerConfig.HeartbeatPeriod)

		assert.Equal(t, "inmemory", config.Ruler.Ring.KVStore.Store)
		assert.Equal(t, 5*time.Minute, config.Ruler.Ring.HeartbeatPeriod)

		assert.Equal(t, "inmemory", config.QueryScheduler.SchedulerRing.KVStore.Store)
		assert.Equal(t, 5*time.Minute, config.QueryScheduler.SchedulerRing.HeartbeatPeriod)

		assert.Equal(t, "inmemory", config.CompactorConfig.CompactorRing.KVStore.Store)
		assert.Equal(t, 5*time.Minute, config.CompactorConfig.CompactorRing.HeartbeatPeriod)

		assert.Equal(t, "inmemory", config.IndexGateway.Ring.KVStore.Store)
		assert.Equal(t, 5*time.Minute, config.IndexGateway.Ring.HeartbeatPeriod)
	})

	t.Run("ring configs provided via command line take precedence", func(t *testing.T) {
		yamlContent := `common:
  ring:
    kvstore:
      store: consul`
		config, _, err := configWrapperFromYAML(t, yamlContent, []string{
			"--distributor.ring.store", "etcd",
		})
		assert.NoError(t, err)
		assert.Equal(t, "etcd", config.Distributor.DistributorRing.KVStore.Store)
		assert.Equal(t, "consul", config.Ingester.LifecyclerConfig.RingConfig.KVStore.Store)
		assert.Equal(t, "consul", config.Ruler.Ring.KVStore.Store)
		assert.Equal(t, "consul", config.QueryScheduler.SchedulerRing.KVStore.Store)
		assert.Equal(t, "consul", config.CompactorConfig.CompactorRing.KVStore.Store)
		assert.Equal(t, "consul", config.IndexGateway.Ring.KVStore.Store)
	})

	t.Run("common ring config take precedence over common memberlist config", func(t *testing.T) {
		yamlContent := `memberlist:
  join_members:
    - 127.0.0.1
common:
  ring:
    kvstore:
      store: etcd`
		config, _, err := configWrapperFromYAML(t, yamlContent, nil)
		assert.NoError(t, err)
		assert.Equal(t, "etcd", config.Distributor.DistributorRing.KVStore.Store)
		assert.Equal(t, "etcd", config.Ingester.LifecyclerConfig.RingConfig.KVStore.Store)
		assert.Equal(t, "etcd", config.Ruler.Ring.KVStore.Store)
		assert.Equal(t, "etcd", config.QueryScheduler.SchedulerRing.KVStore.Store)
		assert.Equal(t, "etcd", config.CompactorConfig.CompactorRing.KVStore.Store)
		assert.Equal(t, "etcd", config.IndexGateway.Ring.KVStore.Store)
	})
}

func Test_applyChunkRetain(t *testing.T) {
	t.Run("chunk retain is unchanged if no index queries cache is defined", func(t *testing.T) {
		yamlContent := ``
		config, defaults, err := configWrapperFromYAML(t, yamlContent, nil)
		assert.NoError(t, err)
		assert.Equal(t, defaults.Ingester.RetainPeriod, config.Ingester.RetainPeriod)
	})

	t.Run("chunk retain is set to IndexCacheValidity + 1 minute", func(t *testing.T) {
		yamlContent := `
schema_config:
  configs:
    - from: 2020-10-24
      store: boltdb-shipper
      object_store: filesystem
      schema: v12
      index:
        prefix: index_
        period: 24h
storage_config:
  index_cache_validity: 10m
  index_queries_cache_config:
    memcached:
      batch_size: 256
      parallelism: 10
    memcached_client:
      consistent_hash: true
      host: memcached-index-queries.loki-bigtable.svc.cluster.local
      service: memcached-client
`
		config, _, err := configWrapperFromYAML(t, yamlContent, nil)
		assert.NoError(t, err)
		assert.Equal(t, 11*time.Minute, config.Ingester.RetainPeriod)
	})

	t.Run("chunk retain is not changed for tsdb index type", func(t *testing.T) {
		yamlContent := `
schema_config:
  configs:
    - from: 2020-10-24
      store: tsdb
      object_store: filesystem
      schema: v12
      index:
        prefix: index_
        period: 24h
storage_config:
  index_cache_validity: 10m
  index_queries_cache_config:
    memcached:
      batch_size: 256
      parallelism: 10
    memcached_client:
      consistent_hash: true
      host: memcached-index-queries.loki-bigtable.svc.cluster.local
      service: memcached-client
`
		config, _, err := configWrapperFromYAML(t, yamlContent, nil)
		assert.NoError(t, err)
		assert.Equal(t, time.Duration(0), config.Ingester.RetainPeriod)
	})
}

func Test_replicationFactor(t *testing.T) {
	t.Run("replication factor is applied when using memberlist", func(t *testing.T) {
		yamlContent := `memberlist:
  join_members:
    - foo.bar.example.com
common:
  replication_factor: 2`
		config, _, err := configWrapperFromYAML(t, yamlContent, nil)
		assert.NoError(t, err)
		assert.Equal(t, 2, config.Ingester.LifecyclerConfig.RingConfig.ReplicationFactor)
		assert.Equal(t, 2, config.IndexGateway.Ring.ReplicationFactor)
	})
}

func Test_IndexGatewayRingReplicationFactor(t *testing.T) {
	t.Run("default replication factor is 3", func(t *testing.T) {
		const emptyConfigString = `---
server:
  http_listen_port: 80`
		config, _, err := configWrapperFromYAML(t, emptyConfigString, nil)
		assert.NoError(t, err)
		assert.Equal(t, 3, config.IndexGateway.Ring.ReplicationFactor)
	})

	t.Run("explicit replication factor for the index gateway should override all other definitions", func(t *testing.T) {
		yamlContent := `ingester:
  lifecycler:
    ring:
      replication_factor: 15
common:
  replication_factor: 30
index_gateway:
  ring:
    replication_factor: 7`

		config, _, err := configWrapperFromYAML(t, yamlContent, nil)
		assert.NoError(t, err)
		assert.Equal(t, 7, config.IndexGateway.Ring.ReplicationFactor)
	})
}

func Test_instanceAddr(t *testing.T) {
	t.Run("common instance addr isn't applied when addresses are explicitly set", func(t *testing.T) {
		yamlContent := `distributor:
  ring:
    instance_addr: mydistributor
ingester:
  lifecycler:
    address: myingester
memberlist:
  advertise_addr: mymemberlist
ruler:
  ring:
    instance_addr: myruler
query_scheduler:
  scheduler_ring:
    instance_addr: myscheduler
frontend:
  address: myqueryfrontend
compactor:
  compactor_ring:
    instance_addr: mycompactor
index_gateway:
  ring:
    instance_addr: myindexgateway
common:
  instance_addr: 99.99.99.99
  ring:
    instance_addr: mycommonring`
		config, _, err := configWrapperFromYAML(t, yamlContent, nil)
		assert.NoError(t, err)
		assert.Equal(t, "mydistributor", config.Distributor.DistributorRing.InstanceAddr)
		assert.Equal(t, "myingester", config.Ingester.LifecyclerConfig.Addr)
		assert.Equal(t, "mymemberlist", config.MemberlistKV.AdvertiseAddr)
		assert.Equal(t, "myruler", config.Ruler.Ring.InstanceAddr)
		assert.Equal(t, "myscheduler", config.QueryScheduler.SchedulerRing.InstanceAddr)
		assert.Equal(t, "myqueryfrontend", config.Frontend.FrontendV2.Addr)
		assert.Equal(t, "mycompactor", config.CompactorConfig.CompactorRing.InstanceAddr)
		assert.Equal(t, "myindexgateway", config.IndexGateway.Ring.InstanceAddr)
	})

	t.Run("common instance addr is applied when addresses are not explicitly set", func(t *testing.T) {
		yamlContent := `common:
  instance_addr: 99.99.99.99`
		config, _, err := configWrapperFromYAML(t, yamlContent, nil)
		assert.NoError(t, err)
		assert.Equal(t, "99.99.99.99", config.Distributor.DistributorRing.InstanceAddr)
		assert.Equal(t, "99.99.99.99", config.Ingester.LifecyclerConfig.Addr)
		assert.Equal(t, "99.99.99.99", config.MemberlistKV.AdvertiseAddr)
		assert.Equal(t, "99.99.99.99", config.Ruler.Ring.InstanceAddr)
		assert.Equal(t, "99.99.99.99", config.QueryScheduler.SchedulerRing.InstanceAddr)
		assert.Equal(t, "99.99.99.99", config.Frontend.FrontendV2.Addr)
		assert.Equal(t, "99.99.99.99", config.CompactorConfig.CompactorRing.InstanceAddr)
		assert.Equal(t, "99.99.99.99", config.IndexGateway.Ring.InstanceAddr)
	})

	t.Run("common instance addr doesn't supersede instance addr from common ring", func(t *testing.T) {
		yamlContent := `common:
  instance_addr: 99.99.99.99
  ring:
    instance_addr: 22.22.22.22`

		config, _, err := configWrapperFromYAML(t, yamlContent, nil)
		assert.NoError(t, err)
		assert.Equal(t, "22.22.22.22", config.Distributor.DistributorRing.InstanceAddr)
		assert.Equal(t, "22.22.22.22", config.Ingester.LifecyclerConfig.Addr)
		assert.Equal(t, "99.99.99.99", config.MemberlistKV.AdvertiseAddr) /// not a ring.
		assert.Equal(t, "22.22.22.22", config.Ruler.Ring.InstanceAddr)
		assert.Equal(t, "22.22.22.22", config.QueryScheduler.SchedulerRing.InstanceAddr)
		assert.Equal(t, "99.99.99.99", config.Frontend.FrontendV2.Addr) // not a ring.
		assert.Equal(t, "22.22.22.22", config.CompactorConfig.CompactorRing.InstanceAddr)
		assert.Equal(t, "22.22.22.22", config.IndexGateway.Ring.InstanceAddr)
	})
}

func Test_instanceInterfaceNames(t *testing.T) {
	t.Run("common instance net interfaces aren't applied when explicitly set at other sections", func(t *testing.T) {
		yamlContent := `distributor:
  ring:
    instance_interface_names:
    - mydistributor
ingester:
  lifecycler:
    interface_names:
    - myingester
ruler:
  ring:
    instance_interface_names:
    - myruler
query_scheduler:
  scheduler_ring:
    instance_interface_names:
    - myscheduler
index_gateway:
  ring:
    instance_interface_names:
    - myindexgateway
frontend:
  instance_interface_names:
  - myfrontend
compactor:
  compactor_ring:
    instance_interface_names:
    - mycompactor
common:
  instance_interface_names:
  - mycommoninf
  ring:
    instance_interface_names:
    - mycommonring`
		config, _, err := configWrapperFromYAML(t, yamlContent, nil)
		assert.NoError(t, err)
		assert.Equal(t, []string{"mydistributor"}, config.Distributor.DistributorRing.InstanceInterfaceNames)
		assert.Equal(t, []string{"myingester"}, config.Ingester.LifecyclerConfig.InfNames)
		assert.Equal(t, []string{"myruler"}, config.Ruler.Ring.InstanceInterfaceNames)
		assert.Equal(t, []string{"myscheduler"}, config.QueryScheduler.SchedulerRing.InstanceInterfaceNames)
		assert.Equal(t, []string{"myfrontend"}, config.Frontend.FrontendV2.InfNames)
		assert.Equal(t, []string{"mycompactor"}, config.CompactorConfig.CompactorRing.InstanceInterfaceNames)
		assert.Equal(t, []string{"myindexgateway"}, config.IndexGateway.Ring.InstanceInterfaceNames)
	})

	t.Run("common instance net interfaces is applied when others net interfaces are not explicitly set", func(t *testing.T) {
		yamlContent := `common:
  instance_interface_names:
  - commoninterface`
		config, _, err := configWrapperFromYAML(t, yamlContent, nil)
		assert.NoError(t, err)
		assert.Equal(t, []string{"commoninterface"}, config.Distributor.DistributorRing.InstanceInterfaceNames)
		assert.Equal(t, []string{"commoninterface"}, config.Ingester.LifecyclerConfig.InfNames)
		assert.Equal(t, []string{"commoninterface"}, config.Ruler.Ring.InstanceInterfaceNames)
		assert.Equal(t, []string{"commoninterface"}, config.QueryScheduler.SchedulerRing.InstanceInterfaceNames)
		assert.Equal(t, []string{"commoninterface"}, config.Frontend.FrontendV2.InfNames)
		assert.Equal(t, []string{"commoninterface"}, config.CompactorConfig.CompactorRing.InstanceInterfaceNames)
		assert.Equal(t, []string{"commoninterface"}, config.IndexGateway.Ring.InstanceInterfaceNames)
	})

	t.Run("common instance net interface doesn't supersede net interface from common ring", func(t *testing.T) {
		yamlContent := `common:
  instance_interface_names:
  - ringsshouldntusethis
  ring:
    instance_interface_names:
    - ringsshouldusethis`

		config, _, err := configWrapperFromYAML(t, yamlContent, nil)
		assert.NoError(t, err)
		assert.Equal(t, []string{"ringsshouldusethis"}, config.Distributor.DistributorRing.InstanceInterfaceNames)
		assert.Equal(t, []string{"ringsshouldusethis"}, config.Ingester.LifecyclerConfig.InfNames)
		assert.Equal(t, []string{"ringsshouldusethis"}, config.Ruler.Ring.InstanceInterfaceNames)
		assert.Equal(t, []string{"ringsshouldusethis"}, config.QueryScheduler.SchedulerRing.InstanceInterfaceNames)
		assert.Equal(t, []string{"ringsshouldntusethis"}, config.Frontend.FrontendV2.InfNames) // not a ring.
		assert.Equal(t, []string{"ringsshouldusethis"}, config.CompactorConfig.CompactorRing.InstanceInterfaceNames)
		assert.Equal(t, []string{"ringsshouldusethis"}, config.IndexGateway.Ring.InstanceInterfaceNames)
	})

	t.Run("common instance net interface doesn't get overwritten by common ring config", func(t *testing.T) {
		yamlContent := `common:
  instance_interface_names:
  - interface
  ring:
    kvstore:
      store: inmemory`

		config, _, err := configWrapperFromYAML(t, yamlContent, nil)
		assert.NoError(t, err)
		assert.Equal(t, []string{"interface"}, config.Distributor.DistributorRing.InstanceInterfaceNames)
		assert.Equal(t, []string{"interface"}, config.Ingester.LifecyclerConfig.InfNames)
		assert.Equal(t, []string{"interface"}, config.Ruler.Ring.InstanceInterfaceNames)
		assert.Equal(t, []string{"interface"}, config.QueryScheduler.SchedulerRing.InstanceInterfaceNames)
		assert.Equal(t, []string{"interface"}, config.Frontend.FrontendV2.InfNames)
		assert.Equal(t, []string{"interface"}, config.CompactorConfig.CompactorRing.InstanceInterfaceNames)
	})

	t.Run("common instance net interface doesn't supersede net interface from common ring with additional config", func(t *testing.T) {
		yamlContent := `common:
  instance_interface_names:
  - ringsshouldntusethis
  ring:
    instance_interface_names:
    - ringsshouldusethis
    kvstore:
      store: inmemory`

		config, _, err := configWrapperFromYAML(t, yamlContent, nil)
		assert.NoError(t, err)
		assert.Equal(t, []string{"ringsshouldusethis"}, config.Distributor.DistributorRing.InstanceInterfaceNames)
		assert.Equal(t, []string{"ringsshouldusethis"}, config.Ingester.LifecyclerConfig.InfNames)
		assert.Equal(t, []string{"ringsshouldusethis"}, config.Ruler.Ring.InstanceInterfaceNames)
		assert.Equal(t, []string{"ringsshouldusethis"}, config.QueryScheduler.SchedulerRing.InstanceInterfaceNames)
		assert.Equal(t, []string{"ringsshouldntusethis"}, config.Frontend.FrontendV2.InfNames) // not a ring.
		assert.Equal(t, []string{"ringsshouldusethis"}, config.CompactorConfig.CompactorRing.InstanceInterfaceNames)
	})
}

func TestNamedStores_applyDefaults(t *testing.T) {
	namedStoresConfig := `storage_config:
  named_stores:
    aws:
      store-1:
        s3: "s3.test"
        storage_class: GLACIER
        dynamodb:
          dynamodb_url: "dynamo.test"
    azure:
      store-2:
        environment: AzureGermanCloud
        account_name: foo
        container_name: bar
    bos:
      store-3:
        bucket_name: foobar
    gcs:
      store-4:
        bucket_name: foobar
        enable_http2: false
    cos:
      store-5:
        endpoint: cos.test
        http_config:
          idle_conn_timeout: 30s
    filesystem:
      store-6:
        directory: foobar
    swift:
      store-7:
        container_name: foobar
        request_timeout: 30s
    alibabacloud:
      store-8:
        bucket: foobar
        endpoint: oss.test
`
	// make goconst happy
	bucketName := "foobar"

	config, defaults, err := configWrapperFromYAML(t, namedStoresConfig, nil)
	require.NoError(t, err)

	nsCfg := config.StorageConfig.NamedStores

	t.Run("aws", func(t *testing.T) {
		assert.Len(t, config.StorageConfig.NamedStores.AWS, 1)

		// expect the defaults to be set on named store config
		expected := defaults.StorageConfig.AWSStorageConfig
		assert.NoError(t, expected.DynamoDB.Set("dynamo.test"))
		assert.NoError(t, expected.S3.Set("s3.test"))
		// override defaults
		expected.StorageClass = "GLACIER"

		assert.Equal(t, expected, (aws.StorageConfig)(nsCfg.AWS["store-1"]))
	})

	t.Run("azure", func(t *testing.T) {
		assert.Len(t, config.StorageConfig.NamedStores.Azure, 1)

		expected := defaults.StorageConfig.AzureStorageConfig
		expected.StorageAccountName = "foo"
		expected.ContainerName = "bar"
		// override defaults
		expected.Environment = "AzureGermanCloud"

		assert.Equal(t, expected, (azure.BlobStorageConfig)(nsCfg.Azure["store-2"]))
	})

	t.Run("bos", func(t *testing.T) {
		assert.Len(t, config.StorageConfig.NamedStores.BOS, 1)

		expected := defaults.StorageConfig.BOSStorageConfig
		expected.BucketName = bucketName

		assert.Equal(t, expected, (baidubce.BOSStorageConfig)(nsCfg.BOS["store-3"]))
	})

	t.Run("gcs", func(t *testing.T) {
		assert.Len(t, config.StorageConfig.NamedStores.GCS, 1)

		expected := defaults.StorageConfig.GCSConfig
		expected.BucketName = bucketName
		// override defaults
		expected.EnableHTTP2 = false

		assert.Equal(t, expected, (gcp.GCSConfig)(nsCfg.GCS["store-4"]))
	})

	t.Run("cos", func(t *testing.T) {
		assert.Len(t, config.StorageConfig.NamedStores.COS, 1)

		expected := defaults.StorageConfig.COSConfig
		expected.Endpoint = "cos.test"
		// override defaults
		expected.HTTPConfig.IdleConnTimeout = 30 * time.Second

		assert.Equal(t, expected, (ibmcloud.COSConfig)(nsCfg.COS["store-5"]))
	})

	t.Run("filesystem", func(t *testing.T) {
		assert.Len(t, config.StorageConfig.NamedStores.Filesystem, 1)

		expected := defaults.StorageConfig.FSConfig
		expected.Directory = bucketName

		assert.Equal(t, expected, (local.FSConfig)(nsCfg.Filesystem["store-6"]))
	})

	t.Run("swift", func(t *testing.T) {
		assert.Len(t, config.StorageConfig.NamedStores.Swift, 1)

		expected := defaults.StorageConfig.Swift
		expected.ContainerName = bucketName
		// override defaults
		expected.RequestTimeout = 30 * time.Second

		assert.Equal(t, expected, (openstack.SwiftConfig)(nsCfg.Swift["store-7"]))
	})

	t.Run("alibabacloud", func(t *testing.T) {
		assert.Len(t, config.StorageConfig.NamedStores.AlibabaCloud, 1)

		expected := defaults.StorageConfig.AlibabaStorageConfig
		expected.Bucket = bucketName
		expected.Endpoint = "oss.test"

		assert.Equal(t, expected, (alibaba.OssConfig)(nsCfg.AlibabaCloud["store-8"]))
	})
}
