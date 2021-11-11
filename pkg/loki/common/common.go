package common

import (
	"flag"

	"github.com/grafana/loki/pkg/storage/chunk/aws"
	"github.com/grafana/loki/pkg/storage/chunk/azure"
	"github.com/grafana/loki/pkg/storage/chunk/gcp"
	"github.com/grafana/loki/pkg/storage/chunk/openstack"
	"github.com/grafana/loki/pkg/util"
)

// Config holds common config that can be shared between multiple other config sections
type Config struct {
	PathPrefix        string          `yaml:"path_prefix"`
	Storage           Storage         `yaml:"storage"`
	PersistTokens     bool            `yaml:"persist_tokens"`
	ReplicationFactor int             `yaml:"replication_factor"`
	Ring              util.RingConfig `yaml:"ring"`
}

func (c *Config) RegisterFlags(_ *flag.FlagSet) {
	throwaway := flag.NewFlagSet("throwaway", flag.PanicOnError)
	throwaway.IntVar(&c.ReplicationFactor, "common.replication-factor", 3, "How many ingesters incoming data should be replicated to.")
	c.Storage.RegisterFlagsWithPrefix("common.storage", throwaway)
	c.Ring.RegisterFlagsWithPrefix("", "collectors/", throwaway)
}

type Storage struct {
	S3       aws.S3Config            `yaml:"s3"`
	GCS      gcp.GCSConfig           `yaml:"gcs"`
	Azure    azure.BlobStorageConfig `yaml:"azure"`
	Swift    openstack.SwiftConfig   `yaml:"swift"`
	FSConfig FilesystemConfig        `yaml:"filesystem"`
}

func (s *Storage) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	s.S3.RegisterFlagsWithPrefix(prefix+".s3", f)
	s.GCS.RegisterFlagsWithPrefix(prefix+".gcs", f)
	s.Azure.RegisterFlagsWithPrefix(prefix+".azure", f)
	s.Swift.RegisterFlagsWithPrefix(prefix+".swift", f)
	s.FSConfig.RegisterFlagsWithPrefix(prefix+".filesystem", f)
}

type FilesystemConfig struct {
	ChunksDirectory string `yaml:"chunks_directory"`
	RulesDirectory  string `yaml:"rules_directory"`
}

func (cfg *FilesystemConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(&cfg.ChunksDirectory, prefix+".chunk-directory", "", "Directory to store chunks in.")
	f.StringVar(&cfg.RulesDirectory, prefix+".rules-directory", "", "Directory to store rules in.")
}
