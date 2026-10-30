package kfake

import (
	"crypto/tls"
	"net"
	"time"

	"github.com/twmb/franz-go/pkg/kmsg"
	"github.com/twmb/franz-go/pkg/kversion"
)

// Opt is an option to configure a client.
type Opt interface {
	apply(*cfg)
}

type opt struct{ fn func(*cfg) }

func (opt opt) apply(cfg *cfg) { opt.fn(cfg) }

type seedTopics struct {
	p  int32
	ts []string
}

type cfg struct {
	nbrokers        int
	ports           []int
	logger          Logger
	clusterID       string
	allowAutoTopic  bool
	defaultNumParts int
	seedTopics      []seedTopics

	minSessionTimeout time.Duration
	maxSessionTimeout time.Duration

	enableSASL bool
	sasls      map[struct{ m, u string }]string // cleared after client initialization
	tls        *tls.Config

	listenFn func(network, address string) (net.Listener, error)

	sleepOutOfOrder bool

	maxVersions *kversion.Versions

	enableACLs    bool
	superusers    map[string]struct{}
	seedACLs      []acl
	brokerConfigs map[string]string

	dataDir    string
	syncWrites bool

	// injectFS, if non-nil, overrides the filesystem used for
	// persistence. This allows tests to share a memFS across
	// cluster restarts without touching the real disk.
	injectFS fs
}

// NumBrokers sets the number of brokers to start in the fake cluster.
func NumBrokers(n int) Opt {
	return opt{func(cfg *cfg) { cfg.nbrokers = n }}
}

// Ports sets the ports to listen on, overriding randomly choosing NumBrokers
// amount of ports.
func Ports(ports ...int) Opt {
	return opt{func(cfg *cfg) { cfg.ports = ports }}
}

// ListenFn sets the listerner function to use, overriding [net.Listen]
func ListenFn(fn func(network, address string) (net.Listener, error)) Opt {
	return opt{func(cfg *cfg) { cfg.listenFn = fn }}
}

// WithLogger sets the logger to use.
func WithLogger(logger Logger) Opt {
	return opt{func(cfg *cfg) { cfg.logger = logger }}
}

// ClusterID sets the cluster ID to return in metadata responses.
func ClusterID(clusterID string) Opt {
	return opt{func(cfg *cfg) { cfg.clusterID = clusterID }}
}

// AllowAutoTopicCreation allows metadata requests to create topics if the
// metadata request has its AllowAutoTopicCreation field set to true.
func AllowAutoTopicCreation() Opt {
	return opt{func(cfg *cfg) { cfg.allowAutoTopic = true }}
}

// DefaultNumPartitions sets the number of partitions to create by default for
// auto created topics / CreateTopics with -1 partitions, overriding the
// default of 10.
func DefaultNumPartitions(n int) Opt {
	return opt{func(cfg *cfg) { cfg.defaultNumParts = n }}
}

// GroupMinSessionTimeout sets the cluster's minimum session timeout allowed
// for groups, overriding the default 6 seconds.
func GroupMinSessionTimeout(d time.Duration) Opt {
	return opt{func(cfg *cfg) { cfg.minSessionTimeout = d }}
}

// GroupMaxSessionTimeout sets the cluster's maximum session timeout allowed
// for groups, overriding the default 5 minutes.
func GroupMaxSessionTimeout(d time.Duration) Opt {
	return opt{func(cfg *cfg) { cfg.maxSessionTimeout = d }}
}

// EnableSASL enables SASL authentication for the cluster. If you do not
// configure a bootstrap user / pass, the default superuser is "admin" /
// "admin" with the SCRAM-SHA-256 SASL mechanisms.
func EnableSASL() Opt {
	return opt{func(cfg *cfg) { cfg.enableSASL = true }}
}

// Superuser seeds the cluster with a superuser. The method must be either
// PLAIN, SCRAM-SHA-256, or SCRAM-SHA-512.
// Note that PLAIN superusers cannot be deleted.
// SCRAM superusers can be modified with AlterUserScramCredentials.
// If you delete all SASL users, the kfake cluster will be unusable.
//
// Superusers bypass all ACL checks when ACLs are enabled.
func Superuser(method, user, pass string) Opt {
	return opt{func(cfg *cfg) {
		cfg.sasls[struct{ m, u string }{method, user}] = pass
		if cfg.superusers == nil {
			cfg.superusers = make(map[string]struct{})
		}
		cfg.superusers[user] = struct{}{}
	}}
}

// TLS enables TLS for the cluster, using the provided TLS config for
// listening.
func TLS(c *tls.Config) Opt {
	return opt{func(cfg *cfg) { cfg.tls = c }}
}

// SeedTopics provides topics to create by default in the cluster. Each topic
// will use the given partitions and use the default internal replication
// factor. If you use a non-positive number for partitions, [DefaultNumPartitions]
// is used. This option can be provided multiple times if you want to seed
// topics with different partition counts. If a topic is provided in multiple
// options, the last specification wins.
func SeedTopics(partitions int32, ts ...string) Opt {
	return opt{func(cfg *cfg) { cfg.seedTopics = append(cfg.seedTopics, seedTopics{partitions, ts}) }}
}

// SleepOutOfOrder allows functions to be handled out of order when control
// functions are sleeping. The functions are be handled internally out of
// order, but responses still wait for the sleeping requests to finish. This
// can be used to set up complicated chains of control where functions only
// advance when you know another request is actively being handled.
func SleepOutOfOrder() Opt {
	return opt{func(cfg *cfg) { cfg.sleepOutOfOrder = true }}
}

// MaxVersions sets the maximum API versions the cluster will advertise and
// accept. This can be used to simulate older Kafka versions. For each request
// key, the cluster will use the minimum of its implemented max version and the
// version specified in the provided Versions. If a key is not present in the
// provided Versions, requests for that key will be rejected.
func MaxVersions(v *kversion.Versions) Opt {
	return opt{func(cfg *cfg) { cfg.maxVersions = v }}
}

// EnableACLs enables ACL checking. When enabled, all requests are checked
// against ACLs. By default, no ACLs exist, so all requests will be denied
// unless you configure superusers (via Superuser option) and then add ACLs
// via CreateACLs as that user.
func EnableACLs() Opt {
	return opt{func(cfg *cfg) { cfg.enableACLs = true }}
}

// ACL defines an ACL entry for seeding the cluster.
type ACL struct {
	// Principal in "User:<name>" format. When used with User(), this field
	// is ignored and forced to "User:<username>".
	Principal string
	// Resource type: kmsg.ACLResourceTypeTopic, Group, Cluster, TransactionalId
	Resource kmsg.ACLResourceType
	// Name of the resource (topic name, group name, "*" for cluster, etc.)
	Name string
	// Pattern type: kmsg.ACLResourcePatternTypeLiteral or Prefixed
	Pattern kmsg.ACLResourcePatternType
	// Operation: kmsg.ACLOperationRead, Write, Create, Delete, Alter, Describe, etc.
	Operation kmsg.ACLOperation
	// Allow true for ALLOW, false for DENY
	Allow bool
	// Host to allow/deny from, defaults to "*" if empty
	Host string
}

// BrokerConfigs sets initial broker-level dynamic configs. Empty values are
// treated as deletes (reset to default). Configs most relevant for testing:
//
//   - group.consumer.heartbeat.interval.ms - heartbeat interval for KIP-848
//     consumer groups (default 5000, lowered to 100 in test binaries)
//   - group.consumer.session.timeout.ms - session timeout for KIP-848
//     consumer groups (default 45000)
//   - group.share.session.timeout.ms - session timeout for share groups (default 45000)
//   - group.share.record.lock.duration.ms - acquisition lock duration for share groups (default 30000)
//   - share.record.lock.sweep.interval.ms - sweep interval for expired locks (default 5000)
//   - group.share.delivery.count.limit - max deliveries before archival (default 5)
//   - message.max.bytes - max produce batch size (default 1048588)
//   - transaction.max.timeout.ms - max transaction timeout (default 900000)
//   - max.incremental.fetch.session.cache.slots - max fetch sessions per broker (default 1000)
//
// Other accepted configs include compression.type, default.replication.factor,
// min.insync.replicas, log.retention.bytes, log.retention.ms, and
// log.message.timestamp.type.
func BrokerConfigs(configs map[string]string) Opt {
	return opt{func(cfg *cfg) {
		if cfg.brokerConfigs == nil {
			cfg.brokerConfigs = make(map[string]string)
		}
		for k, v := range configs {
			cfg.brokerConfigs[k] = v
		}
	}}
}

// DataDir enables disk persistence: the cluster continuously writes state
// to the given directory, and on NewCluster it reloads persisted state from
// that directory if it exists. The directory is created if needed.
func DataDir(dir string) Opt {
	return opt{func(cfg *cfg) { cfg.dataDir = dir }}
}

// SyncWrites enables per-batch fsync for segment and index files.
// Without this, writes rely on the OS page cache and are only explicitly
// synced on segment roll and shutdown, matching real Kafka's default
// (log.flush.interval.messages=MAX_VALUE). This is much faster but
// in-flight batches can be lost on a crash. With SyncWrites, every
// produce is durable immediately at the cost of throughput.
func SyncWrites() Opt {
	return opt{func(c *cfg) { c.syncWrites = true }}
}

// withFS injects a custom filesystem implementation, allowing tests to
// share a memFS across cluster restarts without touching the real disk.
func withFS(f fs) Opt {
	return opt{func(cfg *cfg) { cfg.injectFS = f }}
}

// User adds a SASL user with optional ACLs. Unlike Superuser, this user is
// subject to ACL checks. The method must be PLAIN, SCRAM-SHA-256, or SCRAM-SHA-512.
//
// ACL.Principal is ignored and forced to "User:<user>".
func User(method, user, pass string, acls ...ACL) Opt {
	return opt{func(cfg *cfg) {
		cfg.sasls[struct{ m, u string }{method, user}] = pass
		for _, a := range acls {
			host := a.Host
			if host == "" {
				host = "*"
			}
			perm := kmsg.ACLPermissionTypeDeny
			if a.Allow {
				perm = kmsg.ACLPermissionTypeAllow
			}
			cfg.seedACLs = append(cfg.seedACLs, acl{
				principal:    "User:" + user,
				host:         host,
				resourceType: a.Resource,
				resourceName: a.Name,
				pattern:      a.Pattern,
				operation:    a.Operation,
				permission:   perm,
			})
		}
	}}
}
