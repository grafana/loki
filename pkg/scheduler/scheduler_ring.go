package scheduler

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/go-kit/kit/log/level"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/kv"

	"github.com/cortexproject/cortex/pkg/ring"
	util_log "github.com/cortexproject/cortex/pkg/util/log"
)

const (
	// RingKey is the key under which we store the store gateways ring in the KVStore.
	RingKey = "scheduler"

	// RingNameForServer is the name of the ring used by the store gateway server.
	RingNameForServer = "scheduler"

	// RingNameForClient is the name of the ring used by the store gateway client (we need
	// a different name to avoid clashing Prometheus metrics when running in single-binary).
	RingNameForClient = "scheduler-client"

	// We use a safe default instead of exposing to config option to the user
	// in order to simplify the config.
	RingNumTokens = 512
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

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *RingConfig) RegisterFlags(f *flag.FlagSet) {
	hostname, err := os.Hostname()
	if err != nil {
		level.Error(util_log.Logger).Log("msg", "failed to get hostname", "err", err)
		os.Exit(1)
	}

	// Ring flags
	cfg.KVStore.RegisterFlagsWithPrefix("scheduler.ring.", "schedulers/", f)
	f.DurationVar(&cfg.HeartbeatPeriod, "scheduler.ring.heartbeat-period", 15*time.Second, "Period at which to heartbeat to the ring. 0 = disabled.")
	f.DurationVar(&cfg.HeartbeatTimeout, "scheduler.ring.heartbeat-timeout", time.Minute, "The heartbeat timeout after which schedulers are considered unhealthy within the ring. 0 = never (timeout disabled).")
	f.StringVar(&cfg.TokensFilePath, "scheduler.ring.tokens-file-path", "", "File path where tokens are stored. If empty, tokens are not stored at shutdown and restored at startup.")
	f.BoolVar(&cfg.ZoneAwarenessEnabled, "scheduler.ring.zone-awareness-enabled", false, "True to enable zone-awareness and replicate blocks across different availability zones.")

	// Instance flags
	cfg.InstanceInterfaceNames = []string{"eth0", "en0"}
	f.Var((*flagext.StringSlice)(&cfg.InstanceInterfaceNames), "scheduler.ring.instance-interface-names", "Name of network interface to read address from.")
	f.StringVar(&cfg.InstanceAddr, "scheduler.ring.instance-addr", "", "IP address to advertise in the ring.")
	f.IntVar(&cfg.InstancePort, "scheduler.ring.instance-port", 0, "Port to advertise in the ring (defaults to server.grpc-listen-port).")
	f.StringVar(&cfg.InstanceID, "scheduler.ring.instance-id", hostname, "Instance ID to register in the ring.")
	f.StringVar(&cfg.InstanceZone, "scheduler.ring.instance-availability-zone", "", "The availability zone where this instance is running. Required if zone-awareness is enabled.")
}

// ToLifecyclerConfig returns a LifecyclerConfig based on the scheduler ring config.
func (cfg *RingConfig) ToLifecyclerConfig() (ring.BasicLifecyclerConfig, error) {
	instanceAddr, err := ring.GetInstanceAddr(cfg.InstanceAddr, cfg.InstanceInterfaceNames)
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
		NumTokens:           RingNumTokens,
	}, nil
}

func (cfg *RingConfig) ToRingConfig() ring.Config {
	rc := ring.Config{}
	flagext.DefaultValues(&rc)

	rc.KVStore = cfg.KVStore
	rc.HeartbeatTimeout = cfg.HeartbeatTimeout
	rc.ZoneAwarenessEnabled = cfg.ZoneAwarenessEnabled
	rc.ReplicationFactor = 2

	return rc
}
