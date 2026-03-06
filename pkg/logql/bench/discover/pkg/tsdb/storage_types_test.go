package tsdb

import (
	"flag"
	"testing"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client/aws"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/gcp"
	storageawscommon "github.com/grafana/loki/v3/pkg/storage/common/aws"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/stretchr/testify/require"
)

func TestStorageConfigRegisterFlags(t *testing.T) {
	cfg := StorageConfig{}
	fs := flag.NewFlagSet("storage-config", flag.ContinueOnError)
	cfg.RegisterFlags(fs)

	require.NoError(t, fs.Parse([]string{
		"--storage-type=fs",
		"--bucket=/tmp/loki-index",
		"--tenant=t-1",
		"--local.chunk-directory=/tmp/loki-index",
	}))

	require.Equal(t, StorageType("fs"), cfg.StorageType)
	require.Equal(t, "/tmp/loki-index", cfg.Bucket)
	require.Equal(t, "t-1", cfg.Tenant)
	require.Equal(t, "/tmp/loki-index", cfg.Filesystem.Directory)
}

func TestNormalizeAndValidate(t *testing.T) {
	t.Run("normalizes filesystem alias and maps bucket", func(t *testing.T) {
		cfg := StorageConfig{
			StorageType: StorageType("fs"),
			Bucket:      "/tmp/index",
			Tenant:      "tenant-a",
		}

		require.NoError(t, cfg.NormalizeAndValidate())
		require.Equal(t, StorageTypeFilesystem, cfg.StorageType)
		require.Equal(t, "/tmp/index", cfg.Filesystem.Directory)
		require.Equal(t, defaultStoragePrefix, cfg.Prefix)
	})

	t.Run("maps s3 bucket alias", func(t *testing.T) {
		cfg := StorageConfig{
			StorageType: StorageTypeS3,
			Bucket:      "index-bucket",
			Tenant:      "tenant-a",
			S3: aws.S3Config{
				SignatureVersion: aws.SignatureVersionV4,
				StorageClass:     storageawscommon.StorageClassStandard,
			},
		}

		require.NoError(t, cfg.NormalizeAndValidate())
		require.Equal(t, "index-bucket", cfg.S3.BucketNames)
	})

	t.Run("maps gcs backend field into bucket alias", func(t *testing.T) {
		cfg := StorageConfig{
			StorageType: StorageTypeGCS,
			Tenant:      "tenant-a",
			GCS:         gcp.GCSConfig{BucketName: "gcs-bucket"},
		}

		require.NoError(t, cfg.NormalizeAndValidate())
		require.Equal(t, "gcs-bucket", cfg.Bucket)
	})

	t.Run("maps azure bucket alias and overrides default container", func(t *testing.T) {
		cfg := StorageConfig{
			StorageType: StorageTypeAzure,
			Bucket:      "tenant-index",
			Tenant:      "tenant-a",
		}

		cfg.Azure.Environment = "AzureGlobal"
		cfg.Azure.ContainerName = constants.Loki
		require.NoError(t, cfg.NormalizeAndValidate())
		require.Equal(t, "tenant-index", cfg.Azure.ContainerName)
	})

	t.Run("fails on unsupported storage type", func(t *testing.T) {
		cfg := StorageConfig{StorageType: StorageType("swift"), Bucket: "x", Tenant: "t"}
		err := cfg.NormalizeAndValidate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "unsupported --storage-type")
	})

	t.Run("fails when bucket missing", func(t *testing.T) {
		cfg := StorageConfig{StorageType: StorageTypeS3, Tenant: "t"}
		err := cfg.NormalizeAndValidate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "--bucket is required")
	})

	t.Run("fails on conflicting bucket alias", func(t *testing.T) {
		cfg := StorageConfig{StorageType: StorageTypeS3, Tenant: "t", Bucket: "one"}
		cfg.S3.BucketNames = "two"
		err := cfg.NormalizeAndValidate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "conflicting storage location")
	})

	t.Run("fails when tenant missing", func(t *testing.T) {
		cfg := StorageConfig{StorageType: StorageTypeFilesystem, Bucket: "/tmp/idx"}
		err := cfg.NormalizeAndValidate()
		require.Error(t, err)
		require.Contains(t, err.Error(), "--tenant is required")
	})
}
