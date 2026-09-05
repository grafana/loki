package ruler

import (
	"flag"
	"fmt"
	"slices"
	"time"

	"github.com/pkg/errors"
	commonconfig "github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	promconfig "github.com/prometheus/prometheus/config"
	"go.yaml.in/yaml/v4"

	rulerbase "github.com/grafana/loki/v3/pkg/ruler/base"
	"github.com/grafana/loki/v3/pkg/ruler/storage/cleaner"
	"github.com/grafana/loki/v3/pkg/ruler/storage/instance"
)

type Config struct {
	rulerbase.Config `yaml:",inline"`

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
	Clients             map[string]promconfig.RemoteWriteConfig `yaml:"clients,omitempty" doc:"description=Configure remote write clients. A map with remote client id as key. For details, see https://prometheus.io/docs/prometheus/latest/configuration/configuration/#remote_write Specifying a header with key 'X-Scope-OrgID' under the 'headers' section of RemoteWriteConfig is not permitted. If specified, it will be dropped during config parsing."`
	Enabled             bool                                    `yaml:"enabled"`
	ConfigRefreshPeriod time.Duration                           `yaml:"config_refresh_period"`
	AddOrgIDHeader      bool                                    `yaml:"add_org_id_header" doc:"description=Add an X-Scope-OrgID header in remote write requests with the tenant ID of a Loki tenant that the recording rules are part of."`
}

func (c *RemoteWriteConfig) Validate() error {
	if !c.Enabled {
		return nil
	}

	if len(c.Clients) > 0 {
		for id, clt := range c.Clients {
			if clt.URL == nil {
				return fmt.Errorf("remote-write enabled but client '%s' URL for tenant %s is not configured", clt.Name, id)
			}

			if err := clt.Validate(model.UTF8Validation); err != nil {
				return fmt.Errorf("invalid remote write client for tenant %q: %w", id, err)
			}
		}
	} else {
		return errors.New("remote-write enabled but no clients are configured")
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

	for id, clt := range n.Clients {
		src := c.Clients[id]
		restoreRemoteWriteSecrets(&clt, &src)
		n.Clients[id] = clt
	}

	return n, nil
}

// restoreRemoteWriteSecrets copies every Secret-typed field from src into dst; such fields are obfuscated to "<secret>" on marshal, so Clone must restore them after its YAML round-trip.
func restoreRemoteWriteSecrets(dst, src *promconfig.RemoteWriteConfig) {
	dst.HTTPClientConfig.TLSConfig.Key = src.HTTPClientConfig.TLSConfig.Key
	// dst must not alias src's containers, or a later mergo.Merge of a per-tenant override (see
	// getTenantRemoteWriteConfig) would mutate the shared base config.
	dst.HTTPClientConfig.ProxyConnectHeader = cloneProxyHeader(src.HTTPClientConfig.ProxyConnectHeader)

	if dst.HTTPClientConfig.HTTPHeaders != nil && src.HTTPClientConfig.HTTPHeaders != nil {
		for name, header := range dst.HTTPClientConfig.HTTPHeaders.Headers {
			if srcHeader, ok := src.HTTPClientConfig.HTTPHeaders.Headers[name]; ok {
				header.Secrets = slices.Clone(srcHeader.Secrets)
				dst.HTTPClientConfig.HTTPHeaders.Headers[name] = header
			}
		}
	}

	if dst.HTTPClientConfig.BasicAuth != nil {
		dst.HTTPClientConfig.BasicAuth.Password = src.HTTPClientConfig.BasicAuth.Password
	}
	if dst.HTTPClientConfig.Authorization != nil {
		switch {
		case src.HTTPClientConfig.Authorization != nil:
			dst.HTTPClientConfig.Authorization.Credentials = src.HTTPClientConfig.Authorization.Credentials
		case src.HTTPClientConfig.BearerToken != "":
			// HTTPClientConfig.Validate (run by the YAML round-trip above) migrates a bare bearer_token into authorization.credentials and clears bearer_token.
			dst.HTTPClientConfig.Authorization.Credentials = src.HTTPClientConfig.BearerToken
		}
	}
	if dst.HTTPClientConfig.OAuth2 != nil {
		dst.HTTPClientConfig.OAuth2.ClientSecret = src.HTTPClientConfig.OAuth2.ClientSecret
		dst.HTTPClientConfig.OAuth2.ClientCertificateKey = src.HTTPClientConfig.OAuth2.ClientCertificateKey
		dst.HTTPClientConfig.OAuth2.TLSConfig.Key = src.HTTPClientConfig.OAuth2.TLSConfig.Key
		dst.HTTPClientConfig.OAuth2.ProxyConnectHeader = cloneProxyHeader(src.HTTPClientConfig.OAuth2.ProxyConnectHeader)
	}
	if dst.SigV4Config != nil {
		dst.SigV4Config.SecretKey = src.SigV4Config.SecretKey
	}
	if dst.AzureADConfig != nil {
		if dst.AzureADConfig.OAuth != nil {
			dst.AzureADConfig.OAuth.ClientSecret = src.AzureADConfig.OAuth.ClientSecret
		}
		if dst.AzureADConfig.Certificate != nil {
			dst.AzureADConfig.Certificate.CertificatePassword = src.AzureADConfig.Certificate.CertificatePassword
		}
	}
}

// cloneProxyHeader deep-copies h so the result shares no backing array with h; maps.Clone alone
// only copies the map structure, leaving each []Secret value aliased to the original.
func cloneProxyHeader(h commonconfig.ProxyHeader) commonconfig.ProxyHeader {
	if h == nil {
		return nil
	}
	out := make(commonconfig.ProxyHeader, len(h))
	for k, v := range h {
		out[k] = slices.Clone(v)
	}
	return out
}

// RegisterFlags adds the flags required to config this to the given FlagSet.
func (c *RemoteWriteConfig) RegisterFlags(f *flag.FlagSet) {
	f.BoolVar(&c.AddOrgIDHeader, "ruler.remote-write.add-org-id-header", true, "Add X-Scope-OrgID header in remote write requests.")
	f.BoolVar(&c.Enabled, "ruler.remote-write.enabled", false, "Enable remote-write functionality.")
	f.DurationVar(&c.ConfigRefreshPeriod, "ruler.remote-write.config-refresh-period", 10*time.Second, "Minimum period to wait between refreshing remote-write reconfigurations. This should be greater than or equivalent to -runtime-config.reload-period.")

	if c.Clients == nil {
		c.Clients = make(map[string]promconfig.RemoteWriteConfig)
	}
}
