package ruler

import (
	"flag"
	"fmt"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/config"
	"gopkg.in/yaml.v2"

	ruler "github.com/grafana/loki/v3/pkg/ruler/base"
	"github.com/grafana/loki/v3/pkg/ruler/storage/cleaner"
	"github.com/grafana/loki/v3/pkg/ruler/storage/instance"
)

type Config struct {
	ruler.Config `yaml:",inline"`

	WAL instance.Config `yaml:"wal,omitempty"`
	// we cannot define this in the WAL config since it creates an import cycle

	WALCleaner  cleaner.Config    `yaml:"wal_cleaner,omitempty"`
	RemoteWrite RemoteWriteConfig `yaml:"remote_write,omitempty" doc:"description=Remote-write configuration to send rule samples to a Prometheus remote-write endpoint."`

	Evaluation EvaluationConfig `yaml:"evaluation,omitempty" doc:"description=Configuration for rule evaluation."`
}

func (c *Config) RegisterFlags(f *flag.FlagSet) {
	c.Config.RegisterFlags(f)
	c.RemoteWrite.RegisterFlags(f)
	c.WAL.RegisterFlags(f)
	c.WALCleaner.RegisterFlags(f)
	c.Evaluation.RegisterFlags(f)
}

// Validate overrides the embedded cortex variant which expects a cortex limits struct. Instead, copy the relevant bits over.
func (c *Config) Validate() error {
	if err := c.StoreConfig.Validate(); err != nil {
		return fmt.Errorf("invalid ruler store config: %w", err)
	}

	if err := c.RemoteWrite.Validate(); err != nil {
		return fmt.Errorf("invalid ruler remote-write config: %w", err)
	}

	if err := c.WALCleaner.Validate(); err != nil {
		return fmt.Errorf("invalid ruler wal cleaner config: %w", err)
	}

	return nil
}

type RemoteWriteConfig struct {
	Client              *config.RemoteWriteConfig           `yaml:"client,omitempty" doc:"deprecated|description=Use 'clients' instead. Configure remote write client."`
	Clients             map[string]config.RemoteWriteConfig `yaml:"clients,omitempty" doc:"description=Configure remote write clients. A map with remote client id as key. For details, see https://prometheus.io/docs/prometheus/latest/configuration/configuration/#remote_write Specifying a header with key 'X-Scope-OrgID' under the 'headers' section of RemoteWriteConfig is not permitted. If specified, it will be dropped during config parsing."`
	Enabled             bool                                `yaml:"enabled"`
	ConfigRefreshPeriod time.Duration                       `yaml:"config_refresh_period"`
	AddOrgIDHeader      bool                                `yaml:"add_org_id_header" doc:"description=Add an X-Scope-OrgID header in remote write requests with the tenant ID of a Loki tenant that the recording rules are part of."`
}

func (c *RemoteWriteConfig) Validate() error {
	if !c.Enabled {
		return nil
	}

	if (c.Client == nil || c.Client.URL == nil) && len(c.Clients) == 0 {
		return errors.New("remote-write enabled but no clients URL are configured")
	}

	if len(c.Clients) > 0 {
		for id, clt := range c.Clients {
			if clt.URL == nil {
				return fmt.Errorf("remote-write enabled but client '%s' URL for tenant %s is not configured", clt.Name, id)
			}
		}
	}

	return nil
}

func (c *RemoteWriteConfig) Clone() (*RemoteWriteConfig, error) {
	out, err := yaml.Marshal(c)
	if err != nil {
		return nil, err
	}

	var n *RemoteWriteConfig
	err = yaml.Unmarshal(out, &n)
	if err != nil {
		return nil, err
	}

	// BasicAuth.Password has a type of Secret (github.com/prometheus/common/config/config.go),
	// so when its value is marshaled it is obfuscated as "<secret>".
	// Here we copy the original password into the cloned config.
	if n.Client != nil && n.Client.HTTPClientConfig.BasicAuth != nil {
		n.Client.HTTPClientConfig.BasicAuth.Password = c.Client.HTTPClientConfig.BasicAuth.Password
	}

	for id := range n.Clients {
		if n.Clients[id].HTTPClientConfig.BasicAuth != nil {
			n.Clients[id].HTTPClientConfig.BasicAuth.Password = c.Clients[id].HTTPClientConfig.BasicAuth.Password
		}
	}

	return n, nil
}

// RegisterFlags adds the flags required to config this to the given FlagSet.
func (c *RemoteWriteConfig) RegisterFlags(f *flag.FlagSet) {
	f.BoolVar(&c.AddOrgIDHeader, "ruler.remote-write.add-org-id-header", true, "Add X-Scope-OrgID header in remote write requests.")
	f.BoolVar(&c.Enabled, "ruler.remote-write.enabled", false, "Enable remote-write functionality.")
	f.DurationVar(&c.ConfigRefreshPeriod, "ruler.remote-write.config-refresh-period", 10*time.Second, "Minimum period to wait between refreshing remote-write reconfigurations. This should be greater than or equivalent to -limits.per-user-override-period.")

	if c.Clients == nil {
		c.Clients = make(map[string]config.RemoteWriteConfig)
	}
}
