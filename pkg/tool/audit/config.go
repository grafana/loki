package audit

import (
	"flag"
	"fmt"

	"github.com/grafana/dskit/flagext"
	dskitlog "github.com/grafana/dskit/log"

	"github.com/grafana/loki/v3/pkg/storage"
	lokiStorage "github.com/grafana/loki/v3/pkg/storage/config"
)

type FileConfig struct {
	ConfigFile string
}

func (c *FileConfig) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&c.ConfigFile, "config.file", "config.yaml", "configuration file to load")
}

// Config Loki related storage and schema configs
type Config struct {
	FileConfig    `yaml:",inline"`
	Tenant        string                   `yaml:"tenant,omitempty"`
	SchemaConfig  lokiStorage.SchemaConfig `yaml:"schema_config,omitempty"`
	StorageConfig storage.Config           `yaml:"storage_config,omitempty"`
	LogLevel      dskitlog.Level           `yaml:"log_level"`
	Concurrency   int                      `yaml:"concurrency"`
	WorkingDir    string                   `yaml:"working_dir"`
	Period        string                   `yaml:"period,omitempty"`
}

func (c *Config) RegisterFlags(f *flag.FlagSet) {
	c.FileConfig.RegisterFlags(f)
	c.SchemaConfig.RegisterFlags(f)
	c.StorageConfig.RegisterFlags(f)
	c.LogLevel.RegisterFlags(f)
	f.StringVar(&c.Tenant, "tenant", "", "tenant to analyze data")
	f.IntVar(&c.Concurrency, "concurrency", 100, "amount of files to check concurrently")
	f.StringVar(&c.WorkingDir, "working-dir", ".", "working directory to store downloaded files")
	f.StringVar(&c.Period, "period", "", "the table period in a format like 19959")
}

func (c *Config) Validate() error {
	if err := c.SchemaConfig.Validate(); err != nil {
		return fmt.Errorf("schema config is invalid: %v", err)
	}
	if err := c.StorageConfig.Validate(); err != nil {
		return fmt.Errorf("storage config is invalid: %v", err)
	}
	if len(c.Tenant) <= 0 {
		return fmt.Errorf("tenant argument missing. Use -tenant flag or add 'tenant' to the config file")
	}
	if c.Concurrency <= 0 {
		return fmt.Errorf("concurrency argument needs to be greater than 0")
	}
	if c.Period == "" {
		return fmt.Errorf("period argument missing. Use -period flag or add 'period' to the config file")
	}
	return nil
}

// Clone takes advantage of pass-by-value semantics to return a distinct *Config.
// This is primarily used to parse a different flag set without mutating the original *Config.
func (c *Config) Clone() flagext.Registerer {
	return func(c Config) *Config {
		return &c
	}(*c)
}
