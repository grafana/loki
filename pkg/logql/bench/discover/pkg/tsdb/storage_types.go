package tsdb

import (
	"flag"
	"fmt"
	"strings"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client/aws"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/azure"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/gcp"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

const defaultStoragePrefix = "index/"

type StorageType string

const (
	StorageTypeS3         StorageType = "s3"
	StorageTypeGCS        StorageType = "gcs"
	StorageTypeAzure      StorageType = "azure"
	StorageTypeFilesystem StorageType = "filesystem"
)

type StorageConfig struct {
	StorageType StorageType
	Bucket      string
	Tenant      string
	Prefix      string

	S3         aws.S3Config
	GCS        gcp.GCSConfig
	Azure      azure.BlobStorageConfig
	Filesystem local.FSConfig
}

func (c *StorageConfig) RegisterFlags(fs *flag.FlagSet) {
	fs.StringVar((*string)(&c.StorageType), "storage-type", "", "Object storage backend for TSDB index access. Supported: s3, gcs, azure, filesystem.")
	fs.StringVar(&c.Bucket, "bucket", "", "Primary object storage location alias. Maps to backend-specific bucket/container/directory settings.")
	fs.StringVar(&c.Tenant, "tenant", c.Tenant, "Tenant ID used for TSDB path selection and API mode X-Scope-OrgID header.")

	c.S3.RegisterFlagsWithPrefix("", fs)
	c.GCS.RegisterFlagsWithPrefix("", fs)
	c.Azure.RegisterFlagsWithPrefix("", fs)
	c.Filesystem.RegisterFlagsWithPrefix("", fs)
}

func (c *StorageConfig) NormalizeAndValidate() error {
	c.StorageType = normalizeStorageType(c.StorageType)
	if c.StorageType == "" {
		return fmt.Errorf("--storage-type is required")
	}

	if c.Tenant == "" {
		return fmt.Errorf("--tenant is required")
	}

	if c.Prefix == "" {
		c.Prefix = defaultStoragePrefix
	}

	switch c.StorageType {
	case StorageTypeS3:
		if err := c.normalizeStringAlias(&c.S3.BucketNames, "--s3.buckets"); err != nil {
			return err
		}
		if err := c.S3.Validate(); err != nil {
			return fmt.Errorf("invalid s3 config: %w", err)
		}
	case StorageTypeGCS:
		if err := c.normalizeStringAlias(&c.GCS.BucketName, "--gcs.bucketname"); err != nil {
			return err
		}
	case StorageTypeAzure:
		if err := c.normalizeAzureContainerAlias(); err != nil {
			return err
		}
		if err := c.Azure.Validate(); err != nil {
			return fmt.Errorf("invalid azure config: %w", err)
		}
	case StorageTypeFilesystem:
		if err := c.normalizeStringAlias(&c.Filesystem.Directory, "--local.chunk-directory"); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported --storage-type %q (supported: s3, gcs, azure, filesystem)", c.StorageType)
	}

	return nil
}

func normalizeStorageType(t StorageType) StorageType {
	cleaned := strings.ToLower(strings.TrimSpace(string(t)))
	switch cleaned {
	case "s3":
		return StorageTypeS3
	case "gcs":
		return StorageTypeGCS
	case "azure", "blob":
		return StorageTypeAzure
	case "filesystem", "fs", "local", "file":
		return StorageTypeFilesystem
	default:
		return StorageType(cleaned)
	}
}

func (c *StorageConfig) normalizeStringAlias(backendField *string, backendFlag string) error {
	c.Bucket = strings.TrimSpace(c.Bucket)
	*backendField = strings.TrimSpace(*backendField)

	if c.Bucket == "" && *backendField == "" {
		return fmt.Errorf("--bucket is required for --storage-type=%s", c.StorageType)
	}

	if c.Bucket == "" {
		c.Bucket = *backendField
		return nil
	}

	if *backendField == "" {
		*backendField = c.Bucket
		return nil
	}

	if *backendField != c.Bucket {
		return fmt.Errorf("conflicting storage location: --bucket=%q but %s=%q", c.Bucket, backendFlag, *backendField)
	}

	return nil
}

func (c *StorageConfig) normalizeAzureContainerAlias() error {
	c.Bucket = strings.TrimSpace(c.Bucket)
	c.Azure.ContainerName = strings.TrimSpace(c.Azure.ContainerName)

	if c.Bucket == "" && c.Azure.ContainerName == "" {
		return fmt.Errorf("--bucket is required for --storage-type=%s", c.StorageType)
	}

	if c.Bucket == "" {
		c.Bucket = c.Azure.ContainerName
		return nil
	}

	if c.Azure.ContainerName == "" || c.Azure.ContainerName == constants.Loki {
		c.Azure.ContainerName = c.Bucket
		return nil
	}

	if c.Azure.ContainerName != c.Bucket {
		return fmt.Errorf("conflicting storage location: --bucket=%q but --azure.container-name=%q", c.Bucket, c.Azure.ContainerName)
	}

	return nil
}
