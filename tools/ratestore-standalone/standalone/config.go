package standalone

import (
	"flag"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/kv/memberlist"
	dskit_log "github.com/grafana/dskit/log"
	"github.com/grafana/dskit/ring"

	"github.com/grafana/loki/v3/pkg/distributor"
	ingester_client "github.com/grafana/loki/v3/pkg/ingester/client"
	"github.com/grafana/loki/v3/pkg/validation"
)

const (
	poolName         = "rate-store-standalone"
	ingesterRingName = "ingester"
	metricsNamespace = "rate_store_standalone"
)

type Config struct {
	LogLevel         dskit_log.Level             `yaml:"log_level,omitempty"`
	HTTPListenPort   int                         `yaml:"http_listen_port,omitempty"`
	IngesterClient   ingester_client.Config      `yaml:"ingester_client,omitempty"`
	LifecyclerConfig ring.LifecyclerConfig       `yaml:"lifecycler,omitempty"`
	MemberlistKV     memberlist.KVConfig         `yaml:"memberlist,omitempty"`
	RateStore        distributor.RateStoreConfig `yaml:"rate_store,omitempty"`
	LimitsConfig     validation.Limits           `yaml:"limits_config,omitempty"`
	TenantLimits     validation.TenantLimits     `yaml:"tenant_limits,omitempty"`
}

func (c *Config) RegisterFlags(fs *flag.FlagSet, logger log.Logger) {
	fs.IntVar(&c.HTTPListenPort, "http-listen-port", 3100, "HTTP Listener port")
	c.RateStore.RegisterFlagsWithPrefix("rate-store-standalone", fs)
	c.LogLevel.RegisterFlags(fs)
	c.IngesterClient.RegisterFlags(fs)
	c.LifecyclerConfig.RegisterFlagsWithPrefix("", fs, logger)
	c.MemberlistKV.RegisterFlags(fs)
	c.LimitsConfig.RegisterFlags(fs)

	// Always export metrics with custom prefix
	c.RateStore.Standalone = true
	// Set replication factor to 1 for standalone mode
	c.LifecyclerConfig.RingConfig.ReplicationFactor = 1
	// Set internal to true for ingester client
	c.IngesterClient.Internal = true
}

type tenantLimits struct {
	limits map[string]*validation.Limits
}

func newTenantLimits(limits map[string]*validation.Limits) *tenantLimits {
	return &tenantLimits{
		limits: limits,
	}
}

func (l *tenantLimits) TenantLimits(userID string) *validation.Limits {
	return l.limits[userID]
}

func (l *tenantLimits) AllByUserID() map[string]*validation.Limits { return l.limits }
