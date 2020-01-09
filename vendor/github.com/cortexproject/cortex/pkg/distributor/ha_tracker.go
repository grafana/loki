package distributor

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/weaveworks/common/httpgrpc"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/golang/protobuf/proto"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/prometheus/pkg/timestamp"
	"github.com/weaveworks/common/mtime"

	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/cortexproject/cortex/pkg/ring/kv"
	"github.com/cortexproject/cortex/pkg/ring/kv/codec"
	"github.com/cortexproject/cortex/pkg/util"
)

var (
	electedReplicaChanges = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cortex",
		Name:      "ha_tracker_elected_replica_changes_total",
		Help:      "The total number of times the elected replica has changed for a user ID/cluster.",
	}, []string{"user", "cluster"})
	electedReplicaTimestamp = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "cortex",
		Name:      "ha_tracker_elected_replica_timestamp_seconds",
		Help:      "The timestamp stored for the currently elected replica, from the KVStore.",
	}, []string{"user", "cluster"})
	electedReplicaPropagationTime = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "cortex",
		Name:      "ha_tracker_elected_replica_change_propagation_time_seconds",
		Help:      "The time it for the distributor to update the replica change.",
		Buckets:   prometheus.DefBuckets,
	})
	kvCASCalls = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cortex",
		Name:      "ha_tracker_kv_store_cas_total",
		Help:      "The total number of CAS calls to the KV store for a user ID/cluster.",
	}, []string{"user", "cluster"})

	errNegativeUpdateTimeoutJitterMax = errors.New("HA tracker max update timeout jitter shouldn't be negative")
	errInvalidFailoverTimeout         = "HA Tracker failover timeout (%v) must be at least 1s greater than update timeout - max jitter (%v)"
)

// ProtoReplicaDescFactory makes new InstanceDescs
func ProtoReplicaDescFactory() proto.Message {
	return NewReplicaDesc()
}

// NewReplicaDesc returns an empty *distributor.ReplicaDesc.
func NewReplicaDesc() *ReplicaDesc {
	return &ReplicaDesc{}
}

// Track the replica we're accepting samples from
// for each HA cluster we know about.
type haTracker struct {
	logger              log.Logger
	cfg                 HATrackerConfig
	client              kv.Client
	updateTimeoutJitter time.Duration

	// Replicas we are accepting samples from.
	electedLock sync.RWMutex
	elected     map[string]ReplicaDesc
	done        chan struct{}
	cancel      context.CancelFunc
}

// HATrackerConfig contains the configuration require to
// create a HA Tracker.
type HATrackerConfig struct {
	EnableHATracker bool `yaml:"enable_ha_tracker,omitempty"`
	// We should only update the timestamp if the difference
	// between the stored timestamp and the time we received a sample at
	// is more than this duration.
	UpdateTimeout          time.Duration `yaml:"ha_tracker_update_timeout"`
	UpdateTimeoutJitterMax time.Duration `yaml:"ha_tracker_update_timeout_jitter_max"`
	// We should only failover to accepting samples from a replica
	// other than the replica written in the KVStore if the difference
	// between the stored timestamp and the time we received a sample is
	// more than this duration
	FailoverTimeout time.Duration `yaml:"ha_tracker_failover_timeout"`

	KVStore kv.Config
}

// RegisterFlags adds the flags required to config this to the given FlagSet.
func (cfg *HATrackerConfig) RegisterFlags(f *flag.FlagSet) {
	f.BoolVar(&cfg.EnableHATracker,
		"distributor.ha-tracker.enable",
		false,
		"Enable the distributors HA tracker so that it can accept samples from Prometheus HA replicas gracefully (requires labels).")
	f.DurationVar(&cfg.UpdateTimeout,
		"distributor.ha-tracker.update-timeout",
		15*time.Second,
		"Update the timestamp in the KV store for a given cluster/replica only after this amount of time has passed since the current stored timestamp.")
	f.DurationVar(&cfg.UpdateTimeoutJitterMax,
		"distributor.ha-tracker.update-timeout-jitter-max",
		5*time.Second,
		"To spread the HA deduping heartbeats out over time.")
	f.DurationVar(&cfg.FailoverTimeout,
		"distributor.ha-tracker.failover-timeout",
		30*time.Second,
		"If we don't receive any samples from the accepted replica for a cluster in this amount of time we will failover to the next replica we receive a sample from. This value must be greater than the update timeout")
	// We want the ability to use different Consul instances for the ring and for HA cluster tracking.
	cfg.KVStore.RegisterFlagsWithPrefix("distributor.ha-tracker.", f)
}

// Validate config and returns error on failure
func (cfg *HATrackerConfig) Validate() error {
	if cfg.UpdateTimeoutJitterMax < 0 {
		return errNegativeUpdateTimeoutJitterMax
	}

	minFailureTimeout := cfg.UpdateTimeout + cfg.UpdateTimeoutJitterMax + time.Second
	if cfg.FailoverTimeout < minFailureTimeout {
		return fmt.Errorf(errInvalidFailoverTimeout, cfg.FailoverTimeout, minFailureTimeout)
	}

	return nil
}

// NewClusterTracker returns a new HA cluster tracker using either Consul
// or in-memory KV store.
func newClusterTracker(cfg HATrackerConfig) (*haTracker, error) {
	codec := codec.Proto{Factory: ProtoReplicaDescFactory}

	var jitter time.Duration
	if cfg.UpdateTimeoutJitterMax > 0 {
		jitter = time.Duration(rand.Int63n(int64(2*cfg.UpdateTimeoutJitterMax))) - cfg.UpdateTimeoutJitterMax
	}

	ctx, cancel := context.WithCancel(context.Background())
	t := haTracker{
		logger:              util.Logger,
		cfg:                 cfg,
		updateTimeoutJitter: jitter,
		done:                make(chan struct{}),
		elected:             map[string]ReplicaDesc{},
		cancel:              cancel,
	}

	if cfg.EnableHATracker {
		client, err := kv.NewClient(cfg.KVStore, codec)
		if err != nil {
			return nil, err
		}
		t.client = client
		go t.loop(ctx)
	}
	return &t, nil
}

// Follows pattern used by ring for WatchKey.
func (c *haTracker) loop(ctx context.Context) {
	defer close(c.done)
	// The KVStore config we gave when creating c should have contained a prefix,
	// which would have given us a prefixed KVStore client. So, we can pass empty string here.
	c.client.WatchPrefix(ctx, "", func(key string, value interface{}) bool {
		replica := value.(*ReplicaDesc)
		c.electedLock.Lock()
		defer c.electedLock.Unlock()
		chunks := strings.SplitN(key, "/", 2)

		// The prefix has already been stripped, so a valid key would look like cluster/replica,
		// and a key without a / such as `ring` would be invalid.
		if len(chunks) != 2 {
			return true
		}

		if replica.Replica != c.elected[key].Replica {
			electedReplicaChanges.WithLabelValues(chunks[0], chunks[1]).Inc()
		}
		c.elected[key] = *replica
		electedReplicaTimestamp.WithLabelValues(chunks[0], chunks[1]).Set(float64(replica.ReceivedAt / 1000))
		electedReplicaPropagationTime.Observe(time.Since(timestamp.Time(replica.ReceivedAt)).Seconds())
		return true
	})
}

// Stop ends calls the trackers cancel function, which will end the loop for WatchPrefix.
func (c *haTracker) stop() {
	if c.cfg.EnableHATracker {
		c.cancel()
		<-c.done
	}
}

// CheckReplica checks the cluster and replica against the backing KVStore and local cache in the
// tracker c to see if we should accept the incomming sample. It will return an error if the sample
// should not be accepted. Note that internally this function does checks against the stored values
// and may modify the stored data, for example to failover between replicas after a certain period of time.
// A 202 response code is returned (from checkKVstore) if we shouldn't store this sample but are
// accepting samples from another replica for the cluster, so that there isn't a bunch of error's returned
// to customers clients.
func (c *haTracker) checkReplica(ctx context.Context, userID, cluster, replica string) error {
	// If HA tracking isn't enabled then accept the sample
	if !c.cfg.EnableHATracker {
		return nil
	}
	key := fmt.Sprintf("%s/%s", userID, cluster)
	now := mtime.Now()
	c.electedLock.RLock()
	entry, ok := c.elected[key]
	c.electedLock.RUnlock()
	if ok && now.Sub(timestamp.Time(entry.ReceivedAt)) < c.cfg.UpdateTimeout+c.updateTimeoutJitter {
		if entry.Replica != replica {
			return replicasNotMatchError(replica, entry.Replica)
		}
		return nil
	}

	err := c.checkKVStore(ctx, key, replica, now)
	kvCASCalls.WithLabelValues(userID, cluster).Inc()
	if err != nil {
		// The callback within checkKVStore will return a 202 if the sample is being deduped,
		// otherwise there may have been an actual error CAS'ing that we should log.
		if resp, ok := httpgrpc.HTTPResponseFromError(err); ok && resp.GetCode() != 202 {
			level.Error(util.Logger).Log("msg", "rejecting sample", "error", err)
		}
	}
	return err
}

func (c *haTracker) checkKVStore(ctx context.Context, key, replica string, now time.Time) error {
	return c.client.CAS(ctx, key, func(in interface{}) (out interface{}, retry bool, err error) {
		if desc, ok := in.(*ReplicaDesc); ok {

			// We don't need to CAS and update the timestamp in the KV store if the timestamp we've received
			// this sample at is less than updateTimeout amount of time since the timestamp in the KV store.
			if desc.Replica == replica && now.Sub(timestamp.Time(desc.ReceivedAt)) < c.cfg.UpdateTimeout+c.updateTimeoutJitter {
				return nil, false, nil
			}

			// We shouldn't failover to accepting a new replica if the timestamp we've received this sample at
			// is less than failOver timeout amount of time since the timestamp in the KV store.
			if desc.Replica != replica && now.Sub(timestamp.Time(desc.ReceivedAt)) < c.cfg.FailoverTimeout {
				// Return a 202.
				return nil, false, replicasNotMatchError(replica, desc.Replica)
			}
		}

		// There was either invalid or no data for the key, so we now accept samples
		// from this replica. Invalid could mean that the timestamp in the KV store was
		// out of date based on the update and failover timeouts when compared to now.
		return &ReplicaDesc{
			Replica: replica, ReceivedAt: timestamp.FromTime(now),
		}, true, nil
	})
}

func replicasNotMatchError(replica, elected string) error {
	return httpgrpc.Errorf(http.StatusAccepted, "replicas did not mach, rejecting sample: replica=%s, elected=%s", replica, elected)
}

// Modifies the labels parameter in place, removing labels that match
// the replica or cluster label and returning their values. Returns an error
// if we find one but not both of the labels.
func findHALabels(replicaLabel, clusterLabel string, labels []client.LabelAdapter) (string, string) {
	var cluster, replica string
	var pair client.LabelAdapter

	for _, pair = range labels {
		if pair.Name == replicaLabel {
			replica = string(pair.Value)
		}
		if pair.Name == clusterLabel {
			cluster = string(pair.Value)
		}
	}

	return cluster, replica
}
