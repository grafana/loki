package ring

import (
	"flag"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/go-kit/log"

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/netutil"
	"github.com/grafana/dskit/ring"

	util_flagext "github.com/grafana/loki/v3/pkg/util/flagext"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

// RingConfig masks the ring lifecycler config which contains
// many options not really required by the distributors ring. This config
// is used to strip down the config to the minimum, and avoid confusion
// to the user.
type RingConfig struct { // nolint:revive
	KVStore              kv.Config     `yaml:"kvstore"`
	HeartbeatPeriod      time.Duration `yaml:"heartbeat_period"`
	HeartbeatTimeout     time.Duration `yaml:"heartbeat_timeout"`
	TokensFilePath       string        `yaml:"tokens_file_path"`
	ZoneAwarenessEnabled bool          `yaml:"zone_awareness_enabled"`
	NumTokens            int           `yaml:"num_tokens"`
	ReplicationFactor    int           `yaml:"replication_factor"`

	// Instance details
	InstanceID             string   `yaml:"instance_id" doc:"default=<hostname>"`
	InstanceInterfaceNames []string `yaml:"instance_interface_names" doc:"default=[<private network interfaces>]"`
	InstancePort           int      `yaml:"instance_port"`
	InstanceAddr           string   `yaml:"instance_addr"`
	InstanceZone           string   `yaml:"instance_availability_zone"`
	EnableIPv6             bool     `yaml:"instance_enable_ipv6"`

	// Injected internally
	ListenPort int `yaml:"-"`

	ObservePeriod time.Duration `yaml:"-"`
}

// RegisterFlagsWithPrefix adds the flags required to config this to the given FlagSet
// storePrefix is used to set the path in the KVStore and should end with a /
func (cfg *RingConfig) RegisterFlagsWithPrefix(flagsPrefix, storePrefix string, fs *flag.FlagSet, skip ...string) {
	f := util_flagext.NewFlagSetWithSkip(fs, skip)

	hostname, err := os.Hostname()
	if err != nil {
		level.Error(util_log.Logger).Log("msg", "failed to get hostname", "err", err)
		os.Exit(1)
	}

	// Ring flags
	cfg.KVStore.RegisterFlagsWithPrefix(flagsPrefix+"ring.", storePrefix, f.ToFlagSet())
	f.DurationVar(&cfg.HeartbeatPeriod, flagsPrefix+"ring.heartbeat-period", 15*time.Second, "Period at which to heartbeat to the ring. 0 = disabled.")
	f.DurationVar(&cfg.HeartbeatTimeout, flagsPrefix+"ring.heartbeat-timeout", time.Minute, "The heartbeat timeout after which compactors are considered unhealthy within the ring. 0 = never (timeout disabled).")
	f.StringVar(&cfg.TokensFilePath, flagsPrefix+"ring.tokens-file-path", "", "File path where tokens are stored. If empty, tokens are not stored at shutdown and restored at startup.")
	f.BoolVar(&cfg.ZoneAwarenessEnabled, flagsPrefix+"ring.zone-awareness-enabled", false, "True to enable zone-awareness and replicate blocks across different availability zones.")
	f.IntVar(&cfg.NumTokens, flagsPrefix+"ring.num-tokens", 128, "Number of tokens to own in the ring.")
	f.IntVar(&cfg.ReplicationFactor, flagsPrefix+"ring.replication-factor", 3, "Factor for data replication.")

	// Instance flags
	cfg.InstanceInterfaceNames = netutil.PrivateNetworkInterfacesWithFallback([]string{"eth0", "en0"}, util_log.Logger)
	f.Var((*flagext.StringSlice)(&cfg.InstanceInterfaceNames), flagsPrefix+"ring.instance-interface-names", "Name of network interface to read address from.")
	f.StringVar(&cfg.InstanceAddr, flagsPrefix+"ring.instance-addr", "", "IP address to advertise in the ring.")
	f.IntVar(&cfg.InstancePort, flagsPrefix+"ring.instance-port", 0, "Port to advertise in the ring (defaults to server.grpc-listen-port).")
	f.StringVar(&cfg.InstanceID, flagsPrefix+"ring.instance-id", hostname, "Instance ID to register in the ring.")
	f.StringVar(&cfg.InstanceZone, flagsPrefix+"ring.instance-availability-zone", "", "The availability zone where this instance is running. Required if zone-awareness is enabled.")
	f.BoolVar(&cfg.EnableIPv6, flagsPrefix+"ring.instance-enable-ipv6", false, "Enable using a IPv6 instance address.")
}

// ToLifecyclerConfig returns a LifecyclerConfig based on the compactor ring config.
func (cfg *RingConfig) ToLifecyclerConfig(numTokens int, logger log.Logger) (ring.BasicLifecyclerConfig, error) {
	instanceAddr, err := ring.GetInstanceAddr(cfg.InstanceAddr, cfg.InstanceInterfaceNames, logger, cfg.EnableIPv6)
	if err != nil {
		return ring.BasicLifecyclerConfig{
			RingTokenGenerator: ring.NewRandomTokenGenerator(),
		}, err
	}

	instancePort := ring.GetInstancePort(cfg.InstancePort, cfg.ListenPort)

	return ring.BasicLifecyclerConfig{
		ID:                  cfg.InstanceID,
		Addr:                net.JoinHostPort(instanceAddr, strconv.Itoa(instancePort)),
		Zone:                cfg.InstanceZone,
		HeartbeatPeriod:     cfg.HeartbeatPeriod,
		TokensObservePeriod: 0,
		NumTokens:           numTokens,
		RingTokenGenerator:  ring.NewRandomTokenGenerator(),
	}, nil
}

func CortexLifecyclerConfigToRingConfig(cfg ring.LifecyclerConfig) RingConfig {
	return RingConfig{
		KVStore: kv.Config{
			Store:       cfg.RingConfig.KVStore.Store,
			Prefix:      cfg.RingConfig.KVStore.Prefix,
			StoreConfig: cfg.RingConfig.KVStore.StoreConfig,
		},
		HeartbeatPeriod:        cfg.HeartbeatPeriod,
		HeartbeatTimeout:       cfg.RingConfig.HeartbeatTimeout,
		TokensFilePath:         cfg.TokensFilePath,
		ZoneAwarenessEnabled:   cfg.RingConfig.ZoneAwarenessEnabled,
		InstanceID:             cfg.ID,
		InstanceInterfaceNames: cfg.InfNames,
		InstancePort:           cfg.Port,
		InstanceAddr:           cfg.Addr,
		InstanceZone:           cfg.Zone,
		ListenPort:             cfg.ListenPort,
		ObservePeriod:          cfg.ObservePeriod,
	}
}

func (cfg *RingConfig) ToRingConfig(replicationFactor int) ring.Config {
	rc := ring.Config{}
	flagext.DefaultValues(&rc)

	rc.KVStore = cfg.KVStore
	rc.HeartbeatTimeout = cfg.HeartbeatTimeout
	rc.ZoneAwarenessEnabled = cfg.ZoneAwarenessEnabled
	rc.ReplicationFactor = replicationFactor

	return rc
}
