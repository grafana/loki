package common

import (
	"flag"

	"github.com/grafana/loki/pkg/storage/chunk/aws"
	"github.com/grafana/loki/pkg/storage/chunk/azure"
	"github.com/grafana/loki/pkg/storage/chunk/gcp"
	"github.com/grafana/loki/pkg/storage/chunk/local"
	"github.com/grafana/loki/pkg/storage/chunk/openstack"
)

// Config holds common config that can be shared between multiple other config sections
type Config struct {
	PathPrefix    string  `yaml:"path_prefix"`
	Storage       Storage `yaml:"storage"`
	PersistTokens bool    `yaml:"persist_tokens"`
}

func (c *Config) RegisterFlags(f *flag.FlagSet) {
	c.Storage.RegisterFlagsWithPrefix("common.storage", f)
}

type Storage struct {
	S3       aws.S3Config            `yaml:"s3"`
	GCS      gcp.GCSConfig           `yaml:"gcs"`
	Azure    azure.BlobStorageConfig `yaml:"azure"`
	Swift    openstack.SwiftConfig   `yaml:"swift"`
	FSConfig local.FSConfig          `yaml:"filesystem"`
}

func (s *Storage) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	s.S3.RegisterFlagsWithPrefix(prefix+".s3", f)
	s.GCS.RegisterFlagsWithPrefix(prefix+".gcs", f)
	s.Azure.RegisterFlagsWithPrefix(prefix+".azure", f)
	s.Swift.RegisterFlagsWithPrefix(prefix+".swift", f)
	s.FSConfig.RegisterFlagsWithPrefix(prefix+".filesystem", f)
}
