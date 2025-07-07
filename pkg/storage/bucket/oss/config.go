package oss

import (
	"flag"

	"github.com/grafana/dskit/flagext"
)

// Config holds the configuration for Alibaba Cloud OSS client
type Config struct {
	Endpoint        string         `yaml:"endpoint"`
	Bucket          string         `yaml:"bucket"`
	AccessKeyID     string         `yaml:"access_key_id"`
	AccessKeySecret flagext.Secret `yaml:"access_key_secret"`
}

// RegisterFlags registers the flags for Alibaba Cloud OSS storage config
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("", f)
}

// RegisterFlagsWithPrefix registers the flags for Alibaba Cloud OSS storage config with prefix
func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(&cfg.Bucket, prefix+"oss.bucketname", "", "Name of OSS bucket.")
	f.StringVar(&cfg.Endpoint, prefix+"oss.endpoint", "", "Endpoint to connect to.")
	f.StringVar(&cfg.AccessKeyID, prefix+"oss.access-key-id", "", "alibabacloud Access Key ID")
	f.Var(&cfg.AccessKeySecret, prefix+"oss.access-key-secret", "alibabacloud Secret Access Key")
}
