package common

import (
	"github.com/grafana/loki/pkg/storage/chunk/aws"
	"github.com/grafana/loki/pkg/storage/chunk/azure"
	"github.com/grafana/loki/pkg/storage/chunk/gcp"
	"github.com/grafana/loki/pkg/storage/chunk/openstack"
)

// Config holds common config that can be shared between multiple other config sections
type Config struct {
	PathPrefix  string      `yaml:"path_prefix"`
	ObjectStore ObjectStore `yaml:"object_store"`
}

type ObjectStore struct {
	S3    *aws.S3Config            `yaml:"s3"`
	GCS   *gcp.GCSConfig           `yaml:"gcs"`
	Azure *azure.BlobStorageConfig `yaml:"azure"`
	Swift *openstack.SwiftConfig   `yaml:"swift"`
}
