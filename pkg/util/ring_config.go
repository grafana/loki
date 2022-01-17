package util

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/go-kit/log"

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/kv"

	util_log "github.com/cortexproject/cortex/pkg/util/log"
	"github.com/grafana/dskit/ring"
)

// RingConfig masks the ring lifecycler config which contains
// many options not really required by the distributors ring. This config
// is used to strip down the config to the minimum, and avoid confusion
// to the user.
type RingConfig struct {
	KVStore              kv.Config     `yaml:"kvstore"`
	HeartbeatPeriod      time.Duration `yaml:"heartbeat_period"`
	HeartbeatTimeout     time.Duration `yaml:"heartbeat_timeout"`
	TokensFilePath       string        `yaml:"tokens_file_path"`
	ZoneAwarenessEnabled bool          `yaml:"zone_awareness_enabled"`

	// Instance details
	InstanceID             string   `yaml:"instance_id"`
	InstanceInterfaceNames []string `yaml:"instance_interface_names"`
	InstancePort           int      `yaml:"instance_port"`
	InstanceAddr           string   `yaml:"instance_addr"`
	InstanceZone           string   `yaml:"instance_availability_zone"`

	// Injected internally
	ListenPort int `yaml:"-"`

	ObservePeriod time.Duration `yaml:"-"`
}

// RegisterFlagsWithPrefix adds the flags required to config this to the given FlagSet
// storePrefix is used to set the path in the KVStore and should end with a /
func (cfg *RingConfig) RegisterFlagsWithPrefix(flagsPrefix, storePrefix string, f *flag.FlagSet) {
	hostname, err := os.Hostname()
	if err != nil {
		level.Error(util_log.Logger).Log("msg", "failed to get hostname", "err", err)
		os.Exit(1)
	}

	// Ring flags
	cfg.KVStore.RegisterFlagsWithPrefix(flagsPrefix+"ring.", storePrefix, f)
	f.DurationVar(&cfg.HeartbeatPeriod, flagsPrefix+"ring.heartbeat-period", 15*time.Second, "Period at which to heartbeat to the ring. 0 = disabled.")
	f.DurationVar(&cfg.HeartbeatTimeout, flagsPrefix+"ring.heartbeat-timeout", time.Minute, "The heartbeat timeout after which compactors are considered unhealthy within the ring. 0 = never (timeout disabled).")
	f.StringVar(&cfg.TokensFilePath, flagsPrefix+"ring.tokens-file-path", "", "File path where tokens are stored. If empty, tokens are not stored at shutdown and restored at startup.")
	f.BoolVar(&cfg.ZoneAwarenessEnabled, flagsPrefix+"ring.zone-awareness-enabled", false, "True to enable zone-awareness and replicate blocks across different availability zones.")

	// Instance flags
	cfg.InstanceInterfaceNames = []string{"eth0", "en0"}
	f.Var((*flagext.StringSlice)(&cfg.InstanceInterfaceNames), flagsPrefix+"ring.instance-interface-names", "Name of network interface to read address from.")
	f.StringVar(&cfg.InstanceAddr, flagsPrefix+"ring.instance-addr", "", "IP address to advertise in the ring.")
	f.IntVar(&cfg.InstancePort, flagsPrefix+"ring.instance-port", 0, "Port to advertise in the ring (defaults to server.grpc-listen-port).")
	f.StringVar(&cfg.InstanceID, flagsPrefix+"ring.instance-id", hostname, "Instance ID to register in the ring.")
	f.StringVar(&cfg.InstanceZone, flagsPrefix+"ring.instance-availability-zone", "", "The availability zone where this instance is running. Required if zone-awareness is enabled.")
}

// ToLifecyclerConfig returns a LifecyclerConfig based on the compactor ring config.
func (cfg *RingConfig) ToLifecyclerConfig(numTokens int, logger log.Logger) (ring.BasicLifecyclerConfig, error) {
	instanceAddr, err := ring.GetInstanceAddr(cfg.InstanceAddr, cfg.InstanceInterfaceNames, logger)
	if err != nil {
		return ring.BasicLifecyclerConfig{}, err
	}

	instancePort := ring.GetInstancePort(cfg.InstancePort, cfg.ListenPort)

	return ring.BasicLifecyclerConfig{
		ID:                  cfg.InstanceID,
		Addr:                fmt.Sprintf("%s:%d", instanceAddr, instancePort),
		Zone:                cfg.InstanceZone,
		HeartbeatPeriod:     cfg.HeartbeatPeriod,
		TokensObservePeriod: 0,
		NumTokens:           numTokens,
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
