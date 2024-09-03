package partitionring

import (
	"flag"
	"time"

	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/ring"
)

type Config struct {
	KVStore kv.Config `yaml:"kvstore" doc:"description=The key-value store used to share the hash ring across multiple instances. This option needs be set on ingesters, distributors, queriers, and rulers when running in microservices mode."`

	// MinOwnersCount maps to ring.PartitionInstanceLifecyclerConfig's WaitOwnersCountOnPending.
	MinOwnersCount int `yaml:"min_partition_owners_count"`

	// MinOwnersDuration maps to ring.PartitionInstanceLifecyclerConfig's WaitOwnersDurationOnPending.
	MinOwnersDuration time.Duration `yaml:"min_partition_owners_duration"`

	// DeleteInactivePartitionAfter maps to ring.PartitionInstanceLifecyclerConfig's DeleteInactivePartitionAfterDuration.
	DeleteInactivePartitionAfter time.Duration `yaml:"delete_inactive_partition_after"`

	// lifecyclerPollingInterval is the lifecycler polling interval. This setting is used to lower it in tests.
	lifecyclerPollingInterval time.Duration
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	// Ring flags
	cfg.KVStore.Store = "memberlist" // Override default value.
	cfg.KVStore.RegisterFlagsWithPrefix("ingester.partition-ring.", "collectors/", f)

	f.IntVar(&cfg.MinOwnersCount, "ingester.partition-ring.min-partition-owners-count", 1, "Minimum number of owners to wait before a PENDING partition gets switched to ACTIVE.")
	f.DurationVar(&cfg.MinOwnersDuration, "ingester.partition-ring.min-partition-owners-duration", 10*time.Second, "How long the minimum number of owners are enforced before a PENDING partition gets switched to ACTIVE.")
	f.DurationVar(&cfg.DeleteInactivePartitionAfter, "ingester.partition-ring.delete-inactive-partition-after", 13*time.Hour, "How long to wait before an INACTIVE partition is eligible for deletion. The partition is deleted only if it has been in INACTIVE state for at least the configured duration and it has no owners registered. A value of 0 disables partitions deletion.")
}

func (cfg *Config) ToLifecyclerConfig(partitionID int32, instanceID string) ring.PartitionInstanceLifecyclerConfig {
	return ring.PartitionInstanceLifecyclerConfig{
		PartitionID:                          partitionID,
		InstanceID:                           instanceID,
		WaitOwnersCountOnPending:             cfg.MinOwnersCount,
		WaitOwnersDurationOnPending:          cfg.MinOwnersDuration,
		DeleteInactivePartitionAfterDuration: cfg.DeleteInactivePartitionAfter,
		PollingInterval:                      cfg.lifecyclerPollingInterval,
	}
}
