package storegateway

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/go-kit/kit/log/level"

	"github.com/cortexproject/cortex/pkg/ring"
	"github.com/cortexproject/cortex/pkg/ring/kv"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/flagext"
)

const (
	// RingKey is the key under which we store the store gateways ring in the KVStore.
	RingKey = "store-gateway"

	// RingNameForServer is the name of the ring used by the store gateway server.
	RingNameForServer = "store-gateway"

	// RingNameForClient is the name of the ring used by the store gateway client (we need
	// a different name to avoid clashing Prometheus metrics when running in single-binary).
	RingNameForClient = "store-gateway-client"

	// We use a safe default instead of exposing to config option to the user
	// in order to simplify the config.
	RingNumTokens = 512
)

// RingConfig masks the ring lifecycler config which contains
// many options not really required by the store gateways ring. This config
// is used to strip down the config to the minimum, and avoid confusion
// to the user.
type RingConfig struct {
	KVStore           kv.Config     `yaml:"kvstore" doc:"description=The key-value store used to share the hash ring across multiple instances. This option needs be set both on the store-gateway and querier when running in microservices mode."`
	HeartbeatPeriod   time.Duration `yaml:"heartbeat_period"`
	HeartbeatTimeout  time.Duration `yaml:"heartbeat_timeout"`
	ReplicationFactor int           `yaml:"replication_factor"`
	TokensFilePath    string        `yaml:"tokens_file_path"`

	// Instance details
	InstanceID             string   `yaml:"instance_id" doc:"hidden"`
	InstanceInterfaceNames []string `yaml:"instance_interface_names" doc:"hidden"`
	InstancePort           int      `yaml:"instance_port" doc:"hidden"`
	InstanceAddr           string   `yaml:"instance_addr" doc:"hidden"`

	// Injected internally
	ListenPort      int           `yaml:"-"`
	RingCheckPeriod time.Duration `yaml:"-"`
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *RingConfig) RegisterFlags(f *flag.FlagSet) {
	hostname, err := os.Hostname()
	if err != nil {
		level.Error(util.Logger).Log("msg", "failed to get hostname", "err", err)
		os.Exit(1)
	}

	// Ring flags
	cfg.KVStore.RegisterFlagsWithPrefix("experimental.store-gateway.sharding-ring.", "collectors/", f)
	f.DurationVar(&cfg.HeartbeatPeriod, "experimental.store-gateway.sharding-ring.heartbeat-period", 15*time.Second, "Period at which to heartbeat to the ring.")
	f.DurationVar(&cfg.HeartbeatTimeout, "experimental.store-gateway.sharding-ring.heartbeat-timeout", time.Minute, "The heartbeat timeout after which store gateways are considered unhealthy within the ring."+sharedOptionWithQuerier)
	f.IntVar(&cfg.ReplicationFactor, "experimental.store-gateway.replication-factor", 3, "The replication factor to use when sharding blocks."+sharedOptionWithQuerier)
	f.StringVar(&cfg.TokensFilePath, "experimental.store-gateway.tokens-file-path", "", "File path where tokens are stored. If empty, tokens are not stored at shutdown and restored at startup.")

	// Instance flags
	cfg.InstanceInterfaceNames = []string{"eth0", "en0"}
	f.Var((*flagext.StringSlice)(&cfg.InstanceInterfaceNames), "experimental.store-gateway.sharding-ring.instance-interface", "Name of network interface to read address from.")
	f.StringVar(&cfg.InstanceAddr, "experimental.store-gateway.sharding-ring.instance-addr", "", "IP address to advertise in the ring.")
	f.IntVar(&cfg.InstancePort, "experimental.store-gateway.sharding-ring.instance-port", 0, "Port to advertise in the ring (defaults to server.grpc-listen-port).")
	f.StringVar(&cfg.InstanceID, "experimental.store-gateway.sharding-ring.instance-id", hostname, "Instance ID to register in the ring.")

	// Defaults for internal settings.
	cfg.RingCheckPeriod = 5 * time.Second
}

func (cfg *RingConfig) ToRingConfig() ring.Config {
	rc := ring.Config{}
	flagext.DefaultValues(&rc)

	rc.KVStore = cfg.KVStore
	rc.HeartbeatTimeout = cfg.HeartbeatTimeout
	rc.ReplicationFactor = cfg.ReplicationFactor

	return rc
}

func (cfg *RingConfig) ToLifecyclerConfig() (ring.BasicLifecyclerConfig, error) {
	instanceAddr, err := ring.GetInstanceAddr(cfg.InstanceAddr, cfg.InstanceInterfaceNames)
	if err != nil {
		return ring.BasicLifecyclerConfig{}, err
	}

	instancePort := ring.GetInstancePort(cfg.InstancePort, cfg.ListenPort)

	return ring.BasicLifecyclerConfig{
		ID:                  cfg.InstanceID,
		Addr:                fmt.Sprintf("%s:%d", instanceAddr, instancePort),
		HeartbeatPeriod:     cfg.HeartbeatPeriod,
		TokensObservePeriod: 0,
		NumTokens:           RingNumTokens,
	}, nil
}

func hasRingTopologyChanged(before, after ring.ReplicationSet) bool {
	beforeInstances := before.Ingesters
	afterInstances := after.Ingesters

	if len(beforeInstances) != len(afterInstances) {
		return true
	}

	sort.Sort(ring.ByAddr(beforeInstances))
	sort.Sort(ring.ByAddr(afterInstances))

	for i := 0; i < len(beforeInstances); i++ {
		b := beforeInstances[i]
		a := afterInstances[i]

		// Exclude the heartbeat timestamp from the comparison.
		b.Timestamp = 0
		a.Timestamp = 0

		if !b.Equal(a) {
			return true
		}
	}

	return false
}
