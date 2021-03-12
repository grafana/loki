package distributor

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/gogo/protobuf/proto"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/prometheus/pkg/timestamp"

	"github.com/cortexproject/cortex/pkg/cortexpb"
	"github.com/cortexproject/cortex/pkg/ring/kv"
	"github.com/cortexproject/cortex/pkg/ring/kv/codec"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/services"
)

var (
	errNegativeUpdateTimeoutJitterMax = errors.New("HA tracker max update timeout jitter shouldn't be negative")
	errInvalidFailoverTimeout         = "HA Tracker failover timeout (%v) must be at least 1s greater than update timeout - max jitter (%v)"
)

type haTrackerLimits interface {
	// MaxHAClusters returns max number of clusters that HA tracker should track for a user.
	// Samples from additional clusters are rejected.
	MaxHAClusters(user string) int
}

// ProtoReplicaDescFactory makes new InstanceDescs
func ProtoReplicaDescFactory() proto.Message {
	return NewReplicaDesc()
}

// NewReplicaDesc returns an empty *distributor.ReplicaDesc.
func NewReplicaDesc() *ReplicaDesc {
	return &ReplicaDesc{}
}

// HATrackerConfig contains the configuration require to
// create a HA Tracker.
type HATrackerConfig struct {
	EnableHATracker bool `yaml:"enable_ha_tracker"`
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

	KVStore kv.Config `yaml:"kvstore" doc:"description=Backend storage to use for the ring. Please be aware that memberlist is not supported by the HA tracker since gossip propagation is too slow for HA purposes."`
}

// RegisterFlags adds the flags required to config this to the given FlagSet.
func (cfg *HATrackerConfig) RegisterFlags(f *flag.FlagSet) {
	f.BoolVar(&cfg.EnableHATracker, "distributor.ha-tracker.enable", false, "Enable the distributors HA tracker so that it can accept samples from Prometheus HA replicas gracefully (requires labels).")
	f.DurationVar(&cfg.UpdateTimeout, "distributor.ha-tracker.update-timeout", 15*time.Second, "Update the timestamp in the KV store for a given cluster/replica only after this amount of time has passed since the current stored timestamp.")
	f.DurationVar(&cfg.UpdateTimeoutJitterMax, "distributor.ha-tracker.update-timeout-jitter-max", 5*time.Second, "Maximum jitter applied to the update timeout, in order to spread the HA heartbeats over time.")
	f.DurationVar(&cfg.FailoverTimeout, "distributor.ha-tracker.failover-timeout", 30*time.Second, "If we don't receive any samples from the accepted replica for a cluster in this amount of time we will failover to the next replica we receive a sample from. This value must be greater than the update timeout")

	// We want the ability to use different Consul instances for the ring and
	// for HA cluster tracking. We also customize the default keys prefix, in
	// order to not clash with the ring key if they both share the same KVStore
	// backend (ie. run on the same consul cluster).
	cfg.KVStore.RegisterFlagsWithPrefix("distributor.ha-tracker.", "ha-tracker/", f)
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

func GetReplicaDescCodec() codec.Proto {
	return codec.NewProtoCodec("replicaDesc", ProtoReplicaDescFactory)
}

// Track the replica we're accepting samples from
// for each HA cluster we know about.
type haTracker struct {
	services.Service

	logger              log.Logger
	cfg                 HATrackerConfig
	client              kv.Client
	updateTimeoutJitter time.Duration
	limits              haTrackerLimits

	electedLock sync.RWMutex
	elected     map[string]ReplicaDesc // Replicas we are accepting samples from. Key = "user/cluster".
	clusters    map[string]int         // Number of clusters with elected replicas that a single user has. Key = user.

	electedReplicaChanges         *prometheus.CounterVec
	electedReplicaTimestamp       *prometheus.GaugeVec
	electedReplicaPropagationTime prometheus.Histogram
	kvCASCalls                    *prometheus.CounterVec

	cleanupRuns               prometheus.Counter
	replicasMarkedForDeletion prometheus.Counter
	deletedReplicas           prometheus.Counter
	markingOrDeletionsFailed  prometheus.Counter
}

// NewClusterTracker returns a new HA cluster tracker using either Consul
// or in-memory KV store. Tracker must be started via StartAsync().
func newHATracker(cfg HATrackerConfig, limits haTrackerLimits, reg prometheus.Registerer, logger log.Logger) (*haTracker, error) {
	var jitter time.Duration
	if cfg.UpdateTimeoutJitterMax > 0 {
		jitter = time.Duration(rand.Int63n(int64(2*cfg.UpdateTimeoutJitterMax))) - cfg.UpdateTimeoutJitterMax
	}

	t := &haTracker{
		logger:              logger,
		cfg:                 cfg,
		updateTimeoutJitter: jitter,
		limits:              limits,
		elected:             map[string]ReplicaDesc{},
		clusters:            map[string]int{},

		electedReplicaChanges: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "cortex_ha_tracker_elected_replica_changes_total",
			Help: "The total number of times the elected replica has changed for a user ID/cluster.",
		}, []string{"user", "cluster"}),
		electedReplicaTimestamp: promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
			Name: "cortex_ha_tracker_elected_replica_timestamp_seconds",
			Help: "The timestamp stored for the currently elected replica, from the KVStore.",
		}, []string{"user", "cluster"}),
		electedReplicaPropagationTime: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:    "cortex_ha_tracker_elected_replica_change_propagation_time_seconds",
			Help:    "The time it for the distributor to update the replica change.",
			Buckets: prometheus.DefBuckets,
		}),
		kvCASCalls: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "cortex_ha_tracker_kv_store_cas_total",
			Help: "The total number of CAS calls to the KV store for a user ID/cluster.",
		}, []string{"user", "cluster"}),

		cleanupRuns: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "cortex_ha_tracker_replicas_cleanup_started_total",
			Help: "Number of elected replicas cleanup loops started.",
		}),
		replicasMarkedForDeletion: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "cortex_ha_tracker_replicas_cleanup_marked_for_deletion_total",
			Help: "Number of elected replicas marked for deletion.",
		}),
		deletedReplicas: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "cortex_ha_tracker_replicas_cleanup_deleted_total",
			Help: "Number of elected replicas deleted from KV store.",
		}),
		markingOrDeletionsFailed: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "cortex_ha_tracker_replicas_cleanup_delete_failed_total",
			Help: "Number of elected replicas that failed to be marked for deletion, or deleted.",
		}),
	}

	if cfg.EnableHATracker {
		client, err := kv.NewClient(
			cfg.KVStore,
			GetReplicaDescCodec(),
			kv.RegistererWithKVName(reg, "distributor-hatracker"),
		)
		if err != nil {
			return nil, err
		}
		t.client = client
	}

	t.Service = services.NewBasicService(nil, t.loop, nil)
	return t, nil
}

// Follows pattern used by ring for WatchKey.
func (c *haTracker) loop(ctx context.Context) error {
	if !c.cfg.EnableHATracker {
		// don't do anything, but wait until asked to stop.
		<-ctx.Done()
		return nil
	}

	// Start cleanup loop. It will stop when context is done.
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		c.cleanupOldReplicasLoop(ctx)
	}()

	// The KVStore config we gave when creating c should have contained a prefix,
	// which would have given us a prefixed KVStore client. So, we can pass empty string here.
	c.client.WatchPrefix(ctx, "", func(key string, value interface{}) bool {
		replica := value.(*ReplicaDesc)
		segments := strings.SplitN(key, "/", 2)

		// Valid key would look like cluster/replica, and a key without a / such as `ring` would be invalid.
		if len(segments) != 2 {
			return true
		}

		user := segments[0]
		cluster := segments[1]

		c.electedLock.Lock()
		defer c.electedLock.Unlock()

		if replica.DeletedAt > 0 {
			delete(c.elected, key)
			c.electedReplicaChanges.DeleteLabelValues(user, cluster)
			c.electedReplicaTimestamp.DeleteLabelValues(user, cluster)
			return true
		}

		elected, exists := c.elected[key]
		if replica.Replica != elected.Replica {
			c.electedReplicaChanges.WithLabelValues(user, cluster).Inc()
		}
		if !exists {
			c.clusters[user]++
		}
		c.elected[key] = *replica
		c.electedReplicaTimestamp.WithLabelValues(user, cluster).Set(float64(replica.ReceivedAt / 1000))
		c.electedReplicaPropagationTime.Observe(time.Since(timestamp.Time(replica.ReceivedAt)).Seconds())
		return true
	})

	wg.Wait()
	return nil
}

const (
	cleanupCyclePeriod         = 30 * time.Minute
	cleanupCycleJitterVariance = 0.2 // for 30 minutes, this is ±6 min

	// If we have received last sample for given cluster before this timeout, we will mark selected replica for deletion.
	// If selected replica is marked for deletion for this time, it is deleted completely.
	deletionTimeout = 30 * time.Minute
)

func (c *haTracker) cleanupOldReplicasLoop(ctx context.Context) {
	tick := time.NewTicker(util.DurationWithJitter(cleanupCyclePeriod, cleanupCycleJitterVariance))
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case t := <-tick.C:
			c.cleanupRuns.Inc()
			c.cleanupOldReplicas(ctx, t.Add(-deletionTimeout))
		}
	}
}

// Replicas marked for deletion before deadline will be deleted.
// Replicas with last-received timestamp before deadline will be marked for deletion.
func (c *haTracker) cleanupOldReplicas(ctx context.Context, deadline time.Time) {
	keys, err := c.client.List(ctx, "")
	if err != nil {
		level.Warn(c.logger).Log("msg", "cleanup: failed to list replica keys", "err", err)
		return
	}

	for _, key := range keys {
		if ctx.Err() != nil {
			return
		}

		val, err := c.client.Get(ctx, key)
		if err != nil {
			level.Warn(c.logger).Log("msg", "cleanup: failed to get replica value", "key", key, "err", err)
			continue
		}

		desc, ok := val.(*ReplicaDesc)
		if !ok {
			level.Error(c.logger).Log("msg", "cleanup: got invalid replica descriptor", "key", key)
			continue
		}

		if desc.DeletedAt > 0 {
			if timestamp.Time(desc.DeletedAt).After(deadline) {
				continue
			}

			// We're blindly deleting a key here. It may happen that value was updated since we have read it few lines above,
			// in which case Distributors will have updated value in memory, but Delete will remove it from KV store anyway.
			// That's not great, but should not be a problem. If KV store sends Watch notification for Delete, distributors will
			// delete it from memory, and recreate on next sample with matching replica.
			//
			// If KV store doesn't send Watch notification for Delete, distributors *with* replica in memory will keep using it,
			// while distributors *without* replica in memory will try to write it to KV store -- which will update *all*
			// watching distributors.
			err = c.client.Delete(ctx, key)
			if err != nil {
				level.Error(c.logger).Log("msg", "cleanup: failed to delete old replica", "key", key, "err", err)
				c.markingOrDeletionsFailed.Inc()
			} else {
				level.Info(c.logger).Log("msg", "cleanup: deleted old replica", "key", key)
				c.deletedReplicas.Inc()
			}
			continue
		}

		// Not marked as deleted yet.
		if desc.DeletedAt == 0 && timestamp.Time(desc.ReceivedAt).Before(deadline) {
			err := c.client.CAS(ctx, key, func(in interface{}) (out interface{}, retry bool, err error) {
				d, ok := in.(*ReplicaDesc)
				if !ok || d == nil || d.DeletedAt > 0 || !timestamp.Time(desc.ReceivedAt).Before(deadline) {
					return nil, false, nil
				}

				d.DeletedAt = timestamp.FromTime(time.Now())
				return d, true, nil
			})

			if err != nil {
				c.markingOrDeletionsFailed.Inc()
				level.Error(c.logger).Log("msg", "cleanup: failed to mark replica as deleted", "key", key, "err", err)
			} else {
				c.replicasMarkedForDeletion.Inc()
				level.Info(c.logger).Log("msg", "cleanup: marked replica as deleted", "key", key)
			}
		}
	}
}

// CheckReplica checks the cluster and replica against the backing KVStore and local cache in the
// tracker c to see if we should accept the incomming sample. It will return an error if the sample
// should not be accepted. Note that internally this function does checks against the stored values
// and may modify the stored data, for example to failover between replicas after a certain period of time.
// replicasNotMatchError is returned (from checkKVStore) if we shouldn't store this sample but are
// accepting samples from another replica for the cluster, so that there isn't a bunch of error's returned
// to customers clients.
func (c *haTracker) checkReplica(ctx context.Context, userID, cluster, replica string, now time.Time) error {
	// If HA tracking isn't enabled then accept the sample
	if !c.cfg.EnableHATracker {
		return nil
	}
	key := fmt.Sprintf("%s/%s", userID, cluster)

	c.electedLock.RLock()
	entry, ok := c.elected[key]
	clusters := c.clusters[userID]
	c.electedLock.RUnlock()

	if ok && now.Sub(timestamp.Time(entry.ReceivedAt)) < c.cfg.UpdateTimeout+c.updateTimeoutJitter {
		if entry.Replica != replica {
			return replicasNotMatchError{replica: replica, elected: entry.Replica}
		}
		return nil
	}

	if !ok {
		// If we don't know about this cluster yet and we have reached the limit for number of clusters, we error out now.
		if limit := c.limits.MaxHAClusters(userID); limit > 0 && clusters+1 > limit {
			return tooManyClustersError{limit: limit}
		}
	}

	err := c.checkKVStore(ctx, key, replica, now)
	c.kvCASCalls.WithLabelValues(userID, cluster).Inc()
	if err != nil {
		// The callback within checkKVStore will return a replicasNotMatchError if the sample is being deduped,
		// otherwise there may have been an actual error CAS'ing that we should log.
		if !errors.Is(err, replicasNotMatchError{}) {
			level.Error(c.logger).Log("msg", "rejecting sample", "err", err)
		}
	}
	return err
}

func (c *haTracker) checkKVStore(ctx context.Context, key, replica string, now time.Time) error {
	return c.client.CAS(ctx, key, func(in interface{}) (out interface{}, retry bool, err error) {
		if desc, ok := in.(*ReplicaDesc); ok && desc.DeletedAt == 0 {
			// We don't need to CAS and update the timestamp in the KV store if the timestamp we've received
			// this sample at is less than updateTimeout amount of time since the timestamp in the KV store.
			if desc.Replica == replica && now.Sub(timestamp.Time(desc.ReceivedAt)) < c.cfg.UpdateTimeout+c.updateTimeoutJitter {
				return nil, false, nil
			}

			// We shouldn't failover to accepting a new replica if the timestamp we've received this sample at
			// is less than failover timeout amount of time since the timestamp in the KV store.
			if desc.Replica != replica && now.Sub(timestamp.Time(desc.ReceivedAt)) < c.cfg.FailoverTimeout {
				return nil, false, replicasNotMatchError{replica: replica, elected: desc.Replica}
			}
		}

		// There was either invalid or no data for the key, so we now accept samples
		// from this replica. Invalid could mean that the timestamp in the KV store was
		// out of date based on the update and failover timeouts when compared to now.
		return &ReplicaDesc{
			Replica:    replica,
			ReceivedAt: timestamp.FromTime(now),
			DeletedAt:  0,
		}, true, nil
	})
}

type replicasNotMatchError struct {
	replica, elected string
}

func (e replicasNotMatchError) Error() string {
	return fmt.Sprintf("replicas did not mach, rejecting sample: replica=%s, elected=%s", e.replica, e.elected)
}

// Needed for errors.Is to work properly.
func (e replicasNotMatchError) Is(err error) bool {
	_, ok1 := err.(replicasNotMatchError)
	_, ok2 := err.(*replicasNotMatchError)
	return ok1 || ok2
}

// IsOperationAborted returns whether the error has been caused by an operation intentionally aborted.
func (e replicasNotMatchError) IsOperationAborted() bool {
	return true
}

type tooManyClustersError struct {
	limit int
}

func (e tooManyClustersError) Error() string {
	return fmt.Sprintf("too many HA clusters (limit: %d)", e.limit)
}

// Needed for errors.Is to work properly.
func (e tooManyClustersError) Is(err error) bool {
	_, ok1 := err.(tooManyClustersError)
	_, ok2 := err.(*tooManyClustersError)
	return ok1 || ok2
}

func findHALabels(replicaLabel, clusterLabel string, labels []cortexpb.LabelAdapter) (string, string) {
	var cluster, replica string
	var pair cortexpb.LabelAdapter

	for _, pair = range labels {
		if pair.Name == replicaLabel {
			replica = pair.Value
		}
		if pair.Name == clusterLabel {
			cluster = pair.Value
		}
	}

	return cluster, replica
}

func (c *haTracker) cleanupHATrackerMetricsForUser(userID string) {
	filter := map[string]string{"user": userID}

	if err := util.DeleteMatchingLabels(c.electedReplicaChanges, filter); err != nil {
		level.Warn(c.logger).Log("msg", "failed to remove cortex_ha_tracker_elected_replica_changes_total metric for user", "user", userID, "err", err)
	}
	if err := util.DeleteMatchingLabels(c.electedReplicaTimestamp, filter); err != nil {
		level.Warn(c.logger).Log("msg", "failed to remove cortex_ha_tracker_elected_replica_timestamp_seconds metric for user", "user", userID, "err", err)
	}
	if err := util.DeleteMatchingLabels(c.kvCASCalls, filter); err != nil {
		level.Warn(c.logger).Log("msg", "failed to remove cortex_ha_tracker_kv_store_cas_total metric for user", "user", userID, "err", err)
	}
}
