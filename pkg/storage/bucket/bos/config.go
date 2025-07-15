package bos

import (
	"flag"

	"github.com/grafana/dskit/flagext"
)

// Config holds the configuration for Baidu Cloud BOS client
type Config struct {
	Bucket    string         `yaml:"bucket"`
	Endpoint  string         `yaml:"endpoint"`
	AccessKey string         `yaml:"access_key"`
	SecretKey flagext.Secret `yaml:"secret_key"`
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("", f)
}

func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(&cfg.Bucket, prefix+"bos.bucket", "", "Name of BOS bucket.")
	f.StringVar(&cfg.Endpoint, prefix+"bos.endpoint", "", "BOS endpoint to connect to.")
	f.StringVar(&cfg.AccessKey, prefix+"bos.access-key", "", "Baidu Cloud Engine (BCE) Access Key ID.")
	f.Var(&cfg.SecretKey, prefix+"bos.secret-key", "Baidu Cloud Engine (BCE) Secret Access Key.")
}
