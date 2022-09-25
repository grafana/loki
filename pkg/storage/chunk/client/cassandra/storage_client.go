package cassandra

import (
	"bytes"
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"io/ioutil"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log/level"
	"github.com/gocql/gocql"
	"github.com/grafana/dskit/flagext"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"golang.org/x/sync/semaphore"

	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/series/index"
	util_log "github.com/grafana/loki/pkg/util/log"
)

// Config for a StorageClient
type Config struct {
	Name                     string
	Addresses                string              `yaml:"addresses"`
	Port                     int                 `yaml:"port"`
	Keyspace                 string              `yaml:"keyspace"`
	Consistency              string              `yaml:"consistency"`
	ReplicationFactor        int                 `yaml:"replication_factor"`
	DisableInitialHostLookup bool                `yaml:"disable_initial_host_lookup"`
	SSL                      bool                `yaml:"SSL"`
	HostVerification         bool                `yaml:"host_verification"`
	HostSelectionPolicy      string              `yaml:"host_selection_policy"`
	CAPath                   string              `yaml:"CA_path"`
	CertPath                 string              `yaml:"tls_cert_path"`
	KeyPath                  string              `yaml:"tls_key_path"`
	Auth                     bool                `yaml:"auth"`
	Username                 string              `yaml:"username"`
	Password                 flagext.Secret      `yaml:"password"`
	PasswordFile             string              `yaml:"password_file"`
	CustomAuthenticators     flagext.StringSlice `yaml:"custom_authenticators"`
	Timeout                  time.Duration       `yaml:"timeout"`
	ConnectTimeout           time.Duration       `yaml:"connect_timeout"`
	ReconnectInterval        time.Duration       `yaml:"reconnect_interval"`
	Retries                  int                 `yaml:"max_retries"`
	MaxBackoff               time.Duration       `yaml:"retry_max_backoff"`
	MinBackoff               time.Duration       `yaml:"retry_min_backoff"`
	QueryConcurrency         int                 `yaml:"query_concurrency"`
	NumConnections           int                 `yaml:"num_connections"`
	ConvictHosts             bool                `yaml:"convict_hosts_on_failure"`
	TableOptions             string              `yaml:"table_options"`
}

const (
	HostPolicyRoundRobin = "round-robin"
	HostPolicyTokenAware = "token-aware"
)

// RegisterFlags adds the flags required to config this to the given FlagSet,
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	name := cfg.Name
	f.StringVar(&cfg.Addresses, name+"cassandra.addresses", "", "Comma-separated hostnames or IPs of Cassandra instances.")
	f.IntVar(&cfg.Port, name+"cassandra.port", 9042, "Port that Cassandra is running on")
	f.StringVar(&cfg.Keyspace, name+"cassandra.keyspace", "", "Keyspace to use in Cassandra.")
	f.StringVar(&cfg.Consistency, name+"cassandra.consistency", "QUORUM", "Consistency level for Cassandra.")
	f.IntVar(&cfg.ReplicationFactor, name+"cassandra.replication-factor", 3, "Replication factor to use in Cassandra.")
	f.BoolVar(&cfg.DisableInitialHostLookup, name+"cassandra.disable-initial-host-lookup", false, "Instruct the cassandra driver to not attempt to get host info from the system.peers table.")
	f.BoolVar(&cfg.SSL, name+"cassandra.ssl", false, "Use SSL when connecting to cassandra instances.")
	f.BoolVar(&cfg.HostVerification, name+"cassandra.host-verification", true, "Require SSL certificate validation.")
	f.StringVar(&cfg.HostSelectionPolicy, name+"cassandra.host-selection-policy", HostPolicyRoundRobin, "Policy for selecting Cassandra host. Supported values are: round-robin, token-aware.")
	f.StringVar(&cfg.CAPath, name+"cassandra.ca-path", "", "Path to certificate file to verify the peer.")
	f.StringVar(&cfg.CertPath, name+"cassandra.tls-cert-path", "", "Path to certificate file used by TLS.")
	f.StringVar(&cfg.KeyPath, name+"cassandra.tls-key-path", "", "Path to private key file used by TLS.")
	f.BoolVar(&cfg.Auth, name+"cassandra.auth", false, "Enable password authentication when connecting to cassandra.")
	f.StringVar(&cfg.Username, name+"cassandra.username", "", "Username to use when connecting to cassandra.")
	f.Var(&cfg.Password, name+"cassandra.password", "Password to use when connecting to cassandra.")
	f.StringVar(&cfg.PasswordFile, name+"cassandra.password-file", "", "File containing password to use when connecting to cassandra.")
	f.Var(&cfg.CustomAuthenticators, name+"cassandra.custom-authenticator", "If set, when authenticating with cassandra a custom authenticator will be expected during the handshake. This flag can be set multiple times.")
	f.DurationVar(&cfg.Timeout, name+"cassandra.timeout", 2*time.Second, "Timeout when connecting to cassandra.")
	f.DurationVar(&cfg.ConnectTimeout, name+"cassandra.connect-timeout", 5*time.Second, "Initial connection timeout, used during initial dial to server.")
	f.DurationVar(&cfg.ReconnectInterval, name+"cassandra.reconnent-interval", 1*time.Second, "Interval to retry connecting to cassandra nodes marked as DOWN.")
	f.IntVar(&cfg.Retries, name+"cassandra.max-retries", 0, "Number of retries to perform on a request. Set to 0 to disable retries.")
	f.DurationVar(&cfg.MinBackoff, name+"cassandra.retry-min-backoff", 100*time.Millisecond, "Minimum time to wait before retrying a failed request.")
	f.DurationVar(&cfg.MaxBackoff, name+"cassandra.retry-max-backoff", 10*time.Second, "Maximum time to wait before retrying a failed request.")
	f.IntVar(&cfg.QueryConcurrency, name+"cassandra.query-concurrency", 0, "Limit number of concurrent queries to Cassandra. Set to 0 to disable the limit.")
	f.IntVar(&cfg.NumConnections, name+"cassandra.num-connections", 2, "Number of TCP connections per host.")
	f.BoolVar(&cfg.ConvictHosts, name+"cassandra.convict-hosts-on-failure", true, "Convict hosts of being down on failure.")
	f.StringVar(&cfg.TableOptions, name+"cassandra.table-options", "", "Table options used to create index or chunk tables. This value is used as plain text in the table `WITH` like this, \"CREATE TABLE <generated_by_cortex> (...) WITH <cassandra.table-options>\". For details, see https://cortexmetrics.io/docs/production/cassandra. By default it will use the default table options of your Cassandra cluster.")
}

// nolint: revive
func (cfg *Config) Validate() error {
	if cfg.Password.String() != "" && cfg.PasswordFile != "" {
		return errors.Errorf("The password and password_file config options are mutually exclusive.")
	}
	if cfg.SSL && cfg.HostVerification && len(strings.Split(cfg.Addresses, ",")) != 1 {
		return errors.Errorf("Host verification is only possible for a single host.")
	}
	if cfg.SSL && cfg.CertPath != "" && cfg.KeyPath == "" {
		return errors.Errorf("TLS certificate specified, but private key configuration is missing.")
	}
	if cfg.SSL && cfg.KeyPath != "" && cfg.CertPath == "" {
		return errors.Errorf("TLS private key specified, but certificate configuration is missing.")
	}
	return nil
}

func (cfg *Config) session(name string, reg prometheus.Registerer) (*gocql.Session, *gocql.ClusterConfig, error) {
	level.Warn(util_log.Logger).Log("msg", "Cassandra create session", "name", name, "reg", reg)
	cluster := gocql.NewCluster(strings.Split(cfg.Addresses, ",")...)
	cluster.Port = cfg.Port
	cluster.Keyspace = cfg.Keyspace
	cluster.BatchObserver = &observer{name: cfg.Name}
	cluster.QueryObserver = &observer{name: cfg.Name}
	cluster.Timeout = cfg.Timeout
	cluster.ConnectTimeout = cfg.ConnectTimeout
	cluster.ReconnectInterval = cfg.ReconnectInterval
	cluster.NumConns = cfg.NumConnections
	//cluster.Logger = log.With(util_log.Logger, "module", "gocql", "client", name)
	//cluster.Registerer = prometheus.WrapRegistererWith(
	//	prometheus.Labels{"client": cfg.Name + name}, reg)
	if cfg.Retries > 0 {
		cluster.RetryPolicy = &gocql.ExponentialBackoffRetryPolicy{
			NumRetries: cfg.Retries,
			Min:        cfg.MinBackoff,
			Max:        cfg.MaxBackoff,
		}
	}
	if !cfg.ConvictHosts {
		cluster.ConvictionPolicy = noopConvictionPolicy{}
	}
	if err := cfg.setClusterConfig(cluster); err != nil {
		return nil, nil, errors.WithStack(err)
	}

	session, err := cluster.CreateSession()
	if err == nil {
		return session, cluster, nil
	}
	// ErrNoConnectionsStarted will be returned if keyspace don't exist or is invalid.
	// ref. https://github.com/gocql/gocql/blob/07ace3bab0f84bb88477bab5d79ba1f7e1da0169/cassandra_test.go#L85-L97
	if err != gocql.ErrNoConnectionsStarted {
		return nil, nil, errors.WithStack(err)
	}
	// keyspace not exist
	if err := cfg.createKeyspace(); err != nil {
		return nil, nil, errors.WithStack(err)
	}
	session, err = cluster.CreateSession()
	return session, cluster, errors.WithStack(err)
}

// apply config settings to a cassandra ClusterConfig
func (cfg *Config) setClusterConfig(cluster *gocql.ClusterConfig) error {
	consistency, err := gocql.ParseConsistencyWrapper(cfg.Consistency)
	if err != nil {
		return errors.Wrap(err, "unable to parse the configured consistency")
	}

	cluster.Consistency = consistency
	cluster.DisableInitialHostLookup = cfg.DisableInitialHostLookup

	if cfg.SSL {
		tlsConfig := &tls.Config{}

		if cfg.CertPath != "" {
			cert, err := tls.LoadX509KeyPair(cfg.CertPath, cfg.KeyPath)
			if err != nil {
				return errors.Wrap(err, "Unable to load TLS certificate and private key")
			}

			tlsConfig.Certificates = []tls.Certificate{cert}
		}

		if cfg.HostVerification {
			tlsConfig.ServerName = strings.Split(cfg.Addresses, ",")[0]

			cluster.SslOpts = &gocql.SslOptions{
				CaPath:                 cfg.CAPath,
				EnableHostVerification: true,
				Config:                 tlsConfig,
			}
		} else {
			cluster.SslOpts = &gocql.SslOptions{
				EnableHostVerification: false,
				Config:                 tlsConfig,
			}
		}
	}

	if cfg.HostSelectionPolicy == HostPolicyRoundRobin {
		cluster.PoolConfig.HostSelectionPolicy = gocql.RoundRobinHostPolicy()
	} else if cfg.HostSelectionPolicy == HostPolicyTokenAware {
		cluster.PoolConfig.HostSelectionPolicy = gocql.TokenAwareHostPolicy(gocql.RoundRobinHostPolicy())
	} else {
		return errors.New("Unknown host selection policy")
	}

	if cfg.Auth {
		password := cfg.Password.String()
		if cfg.PasswordFile != "" {
			passwordBytes, err := ioutil.ReadFile(cfg.PasswordFile)
			if err != nil {
				return errors.Errorf("Could not read Cassandra password file: %v", err)
			}
			passwordBytes = bytes.TrimRight(passwordBytes, "\n")
			password = string(passwordBytes)
		}
		if len(cfg.CustomAuthenticators) != 0 {
			cluster.Authenticator = CustomPasswordAuthenticator{
				ApprovedAuthenticators: cfg.CustomAuthenticators,
				Username:               cfg.Username,
				Password:               password,
			}
			return nil
		}
		cluster.Authenticator = gocql.PasswordAuthenticator{
			Username: cfg.Username,
			Password: password,
		}
	}
	return nil
}

// createKeyspace will create the desired keyspace if it doesn't exist.
func (cfg *Config) createKeyspace() error {
	cluster := gocql.NewCluster(strings.Split(cfg.Addresses, ",")...)
	cluster.Port = cfg.Port
	cluster.Keyspace = "system"
	cluster.Timeout = 20 * time.Second
	cluster.ConnectTimeout = 20 * time.Second

	if err := cfg.setClusterConfig(cluster); err != nil {
		return errors.WithStack(err)
	}

	session, err := cluster.CreateSession()
	if err != nil {
		return errors.WithStack(err)
	}
	defer session.Close()

	err = session.Query(fmt.Sprintf(
		`CREATE KEYSPACE IF NOT EXISTS %s
		 WITH replication = {
			 'class' : 'SimpleStrategy',
			 'replication_factor' : %d
		 }`,
		cfg.Keyspace, cfg.ReplicationFactor)).Exec()
	return errors.WithStack(err)
}

// StorageClient implements chunk.IndexClient and chunk.ObjectClient for Cassandra.
type StorageClient struct {
	cfg                Config
	schemaCfg          config.SchemaConfig
	readSession        *gocql.Session
	writeSession       *gocql.Session
	writeMtx           sync.Mutex
	readMtx            sync.Mutex
	readClusterConfig  *gocql.ClusterConfig
	writeClusterConfig *gocql.ClusterConfig
	querySemaphore     *semaphore.Weighted
}

// NewStorageClient returns a new StorageClient.
func NewStorageClient(cfg Config, schemaCfg config.SchemaConfig, registerer prometheus.Registerer) (*StorageClient, error) {
	readSession, readClusterConfig, err := cfg.session("index-read", registerer)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	writeSession, writeClusterConfig, err := cfg.session("index-write", registerer)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	var querySemaphore *semaphore.Weighted
	if cfg.QueryConcurrency > 0 {
		querySemaphore = semaphore.NewWeighted(int64(cfg.QueryConcurrency))
	}

	client := &StorageClient{
		cfg:                cfg,
		schemaCfg:          schemaCfg,
		readSession:        readSession,
		writeSession:       writeSession,
		readClusterConfig:  readClusterConfig,
		writeClusterConfig: writeClusterConfig,
		querySemaphore:     querySemaphore,
	}
	return client, nil
}

// Stop implement chunk.IndexClient.
func (s *StorageClient) Stop() {
	s.readSession.Close()
	s.writeSession.Close()
}

// Cassandra batching isn't really useful in this case, its more to do multiple
// atomic writes.  Therefore we just do a bunch of writes in parallel.
type writeBatch struct {
	entries []index.Entry
	deletes []index.Entry
}

// NewWriteBatch implement chunk.IndexClient.
func (s *StorageClient) NewWriteBatch() index.WriteBatch {
	return &writeBatch{}
}

func (b *writeBatch) Add(tableName, hashValue string, rangeValue []byte, value []byte) {
	b.entries = append(b.entries, index.Entry{
		TableName:  tableName,
		HashValue:  hashValue,
		RangeValue: rangeValue,
		Value:      value,
	})
}

func (b *writeBatch) Delete(tableName, hashValue string, rangeValue []byte) {
	b.deletes = append(b.deletes, index.Entry{
		TableName:  tableName,
		HashValue:  hashValue,
		RangeValue: rangeValue,
	})
}

// BatchWrite implement chunk.IndexClient.
func (s *StorageClient) BatchWrite(ctx context.Context, batch index.WriteBatch) error {
	err := s.batchWrite(ctx, batch)
	if err != nil {
		fmt.Println(time.Now(), " - cassandra BatchWrite fail,err:", err, ",errors.Cause(err):", errors.Cause(err))
	}
	//
	if errors.Cause(err) == gocql.ErrNoConnections {
		fmt.Println(time.Now(), " - cassandra BatchWrite fail,do reconnect,err:", err, ",errors.Cause(err):", errors.Cause(err))
		connectErr := s.reconnectWriteSession()
		if connectErr != nil {
			fmt.Println(time.Now(), " - cassandra PutChunks fail,do reconnect fail,connectErr:", connectErr)
			return errors.Wrap(err, "BatchWrite BatchWrite reconnect fail")
		}
		fmt.Println(time.Now(), " - cassandra BatchWrite fail,do reconnect success,connectErr:", connectErr)
		// retry after reconnect
		err = s.batchWrite(ctx, batch)
		if err != nil {
			fmt.Println(time.Now(), " - cassandra BatchWrite retry fail ,do reconnect success,err:", err)
			return err
		}
		fmt.Println(time.Now(), " - cassandra BatchWrite retry success ,do reconnect success,err:", err)

	}
	return err
}

func (s *StorageClient) reconnectWriteSession() error {
	s.writeMtx.Lock()
	defer s.writeMtx.Unlock()
	newSession, err := s.writeClusterConfig.CreateSession()
	if err != nil {
		return err
	}
	s.writeSession.Close()
	s.writeSession = newSession
	return nil
}

func (s *StorageClient) reconnectReadSession() error {
	s.readMtx.Lock()
	defer s.readMtx.Unlock()
	newSession, err := s.readClusterConfig.CreateSession()
	if err != nil {
		return err
	}
	s.readSession.Close()
	s.readSession = newSession
	return nil
}

func (s *StorageClient) batchWrite(ctx context.Context, batch index.WriteBatch) error {
	b := batch.(*writeBatch)

	for _, entry := range b.entries {
		err := s.writeSession.Query(fmt.Sprintf("INSERT INTO %s (hash, range, value) VALUES (?, ?, ?)",
			entry.TableName), entry.HashValue, entry.RangeValue, entry.Value).WithContext(ctx).Exec()
		if err != nil {
			return errors.WithStack(err)
		}
	}

	for _, entry := range b.deletes {
		err := s.writeSession.Query(fmt.Sprintf("DELETE FROM %s WHERE hash = ? and range = ?",
			entry.TableName), entry.HashValue, entry.RangeValue).WithContext(ctx).Exec()
		if err != nil {
			return errors.WithStack(err)
		}
	}

	return nil
}

// QueryPages implement chunk.IndexClient.
func (s *StorageClient) QueryPages(ctx context.Context, queries []index.Query, callback index.QueryPagesCallback) error {
	err := util.DoParallelQueries(ctx, s.query, queries, callback)
	if err != nil {
		return errors.New("StorageClient QueryPages fail,err:" + err.Error())
	}
	return nil
}

func (s *StorageClient) query(ctx context.Context, query index.Query, callback index.QueryPagesCallback) error {
	err := s.queryExec(ctx, query, callback)
	//
	if errors.Cause(err) == gocql.ErrNotFound {
		return nil
	}
	if errors.Cause(err) == gocql.ErrNoConnections {
		connectErr := s.reconnectReadSession()
		if connectErr != nil {
			return errors.Wrap(err, "StorageClient query reconnect fail")
		}
		// retry after reconnect
		err = s.queryExec(ctx, query, callback)
	}
	return errors.Wrap(err, "query index fail")
}

func (s *StorageClient) queryExec(ctx context.Context, query index.Query, callback index.QueryPagesCallback) error {
	if s.querySemaphore != nil {
		if err := s.querySemaphore.Acquire(ctx, 1); err != nil {
			return err
		}
		defer s.querySemaphore.Release(1)
	}

	var q *gocql.Query

	switch {
	case len(query.RangeValuePrefix) > 0 && query.ValueEqual == nil:
		q = s.readSession.Query(fmt.Sprintf("SELECT range, value FROM %s WHERE hash = ? AND range >= ? AND range < ?",
			query.TableName), query.HashValue, query.RangeValuePrefix, append(query.RangeValuePrefix, '\xff'))

	case len(query.RangeValuePrefix) > 0 && query.ValueEqual != nil:
		q = s.readSession.Query(fmt.Sprintf("SELECT range, value FROM %s WHERE hash = ? AND range >= ? AND range < ? AND value = ? ALLOW FILTERING",
			query.TableName), query.HashValue, query.RangeValuePrefix, append(query.RangeValuePrefix, '\xff'), query.ValueEqual)

	case len(query.RangeValueStart) > 0 && query.ValueEqual == nil:
		q = s.readSession.Query(fmt.Sprintf("SELECT range, value FROM %s WHERE hash = ? AND range >= ?",
			query.TableName), query.HashValue, query.RangeValueStart)

	case len(query.RangeValueStart) > 0 && query.ValueEqual != nil:
		q = s.readSession.Query(fmt.Sprintf("SELECT range, value FROM %s WHERE hash = ? AND range >= ? AND value = ? ALLOW FILTERING",
			query.TableName), query.HashValue, query.RangeValueStart, query.ValueEqual)

	case query.ValueEqual == nil:
		q = s.readSession.Query(fmt.Sprintf("SELECT range, value FROM %s WHERE hash = ?",
			query.TableName), query.HashValue)

	case query.ValueEqual != nil:
		q = s.readSession.Query(fmt.Sprintf("SELECT range, value FROM %s WHERE hash = ? AND value = ? ALLOW FILTERING",
			query.TableName), query.HashValue, query.ValueEqual)
	}

	iter := q.WithContext(ctx).Iter()
	defer iter.Close()
	scanner := iter.Scanner()
	for scanner.Next() {
		b := &readBatch{}
		if err := scanner.Scan(&b.rangeValue, &b.value); err != nil {
			return errors.WithStack(err)
		}
		if !callback(query, b) {
			return nil
		}
	}
	return errors.WithStack(scanner.Err())
}

// Allow other packages to interact with Cassandra directly
func (s *StorageClient) GetReadSession() *gocql.Session {
	return s.readSession
}

// readBatch represents a batch of rows read from Cassandra.
type readBatch struct {
	rangeValue []byte
	value      []byte
}

func (r *readBatch) Iterator() index.ReadBatchIterator {
	return &readBatchIter{
		readBatch: r,
	}
}

type readBatchIter struct {
	consumed bool
	*readBatch
}

func (b *readBatchIter) Next() bool {
	if b.consumed {
		return false
	}
	b.consumed = true
	return true
}

func (b *readBatchIter) RangeValue() []byte {
	return b.rangeValue
}

func (b *readBatchIter) Value() []byte {
	return b.value
}

// ObjectClient implements chunk.ObjectClient for Cassandra.
type ObjectClient struct {
	cfg                Config
	schemaCfg          config.SchemaConfig
	readSession        *gocql.Session
	writeSession       *gocql.Session
	writeMtx           sync.Mutex
	readMtx            sync.Mutex
	readClusterConfig  *gocql.ClusterConfig
	writeClusterConfig *gocql.ClusterConfig
	querySemaphore     *semaphore.Weighted
	maxGetParallel     int
}

// NewObjectClient returns a new ObjectClient.
func NewObjectClient(cfg Config, schemaCfg config.SchemaConfig, registerer prometheus.Registerer, maxGetParallel int) (*ObjectClient, error) {
	readSession, readClusterConfig, err := cfg.session("chunks-read", registerer)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	writeSession, writeClusterConfig, err := cfg.session("chunks-write", registerer)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	var querySemaphore *semaphore.Weighted
	if cfg.QueryConcurrency > 0 {
		querySemaphore = semaphore.NewWeighted(int64(cfg.QueryConcurrency))
	}

	client := &ObjectClient{
		cfg:                cfg,
		schemaCfg:          schemaCfg,
		readSession:        readSession,
		writeSession:       writeSession,
		readClusterConfig:  readClusterConfig,
		writeClusterConfig: writeClusterConfig,
		querySemaphore:     querySemaphore,
		maxGetParallel:     maxGetParallel,
	}
	return client, nil
}
func (s *ObjectClient) reconnectWriteSession() error {
	s.writeMtx.Lock()
	defer s.writeMtx.Unlock()
	newSession, err := s.writeClusterConfig.CreateSession()
	if err != nil {
		return err
	}
	s.writeSession.Close()
	s.writeSession = newSession
	return nil
}

func (s *ObjectClient) reconnectReadSession() error {
	s.readMtx.Lock()
	defer s.readMtx.Unlock()
	newSession, err := s.readClusterConfig.CreateSession()
	if err != nil {
		return err
	}
	s.readSession.Close()
	s.readSession = newSession
	return nil
}

// PutChunks implements chunk.ObjectClient.
func (s *ObjectClient) PutChunks(ctx context.Context, chunks []chunk.Chunk) error {
	err := s.putChunks(ctx, chunks)
	if err != nil {
		fmt.Println(time.Now(), " - cassandra PutChunks fail,err:", err, ",errors.Cause(err):", errors.Cause(err))
	}
	//
	if errors.Cause(err) == gocql.ErrNoConnections {
		fmt.Println(time.Now(), " - cassandra PutChunks fail,do reconnect,err:", err, ",errors.Cause(err):", errors.Cause(err))
		connectErr := s.reconnectWriteSession()
		if connectErr != nil {
			fmt.Println(time.Now(), " - cassandra PutChunks fail,do reconnect fail,connectErr:", connectErr)
			return errors.Wrap(err, "ObjectClient BatchWrite reconnect fail")
		}
		fmt.Println(time.Now(), " - cassandra PutChunks fail,do reconnect success,connectErr:", connectErr)
		// retry after reconnect
		err = s.putChunks(ctx, chunks)
		if err != nil {
			fmt.Println(time.Now(), " - cassandra PutChunks retry fail ,do reconnect success,err:", err)
			return err
		}
		fmt.Println(time.Now(), " - cassandra PutChunks retry success ,do reconnect success,err:", err)
	}
	return err
}

func (s *ObjectClient) putChunks(ctx context.Context, chunks []chunk.Chunk) error {
	for i := range chunks {
		buf, err := chunks[i].Encoded()
		if err != nil {
			return errors.WithStack(err)
		}
		key := s.schemaCfg.ExternalKey(chunks[i].ChunkRef)
		tableName, err := s.schemaCfg.ChunkTableFor(chunks[i].From)
		if err != nil {
			return err
		}

		// Must provide a range key, even though its not useds - hence 0x00.
		q := s.writeSession.Query(fmt.Sprintf("INSERT INTO %s (hash, range, value) VALUES (?, 0x00, ?)",
			tableName), key, buf)
		if err := q.WithContext(ctx).Exec(); err != nil {
			return errors.WithStack(err)
		}
	}

	return nil
}

// GetChunks implements chunk.ObjectClient.
func (s *ObjectClient) GetChunks(ctx context.Context, input []chunk.Chunk) ([]chunk.Chunk, error) {
	chunks, err := util.GetParallelChunks(ctx, s.maxGetParallel, input, s.getChunk)
	if err != nil {
		return nil, errors.New("util.GetParallelChunks fail," + err.Error())
	}
	return chunks, nil
}

var emptyLabel = labels.Labels{
	{Name: model.MetricNameLabel, Value: "foo"},
	{Name: "bar", Value: "baz"},
}

func (s *ObjectClient) getChunk(ctx context.Context, decodeContext *chunk.DecodeContext, input chunk.Chunk) (chunk.Chunk, error) {
	result, err := s.getChunkExec(ctx, decodeContext, input)
	if errors.Cause(err) == gocql.ErrNotFound {
		const chunkLen = 13 * 3600 // in seconds
		userID := "-1"
		ts := model.TimeFromUnix(int64(0 * chunkLen))
		promChunk := chunk.New()
		emptyChunk := chunk.NewChunk(
			userID,
			model.Fingerprint(1),
			emptyLabel,
			promChunk,
			ts,
			ts.Add(chunkLen))
		return emptyChunk, nil
	}
	//
	if errors.Cause(err) == gocql.ErrNoConnections {
		connectErr := s.reconnectReadSession()
		if connectErr != nil {
			return input, errors.Wrap(err, "ObjectClient getChunk reconnect fail")
		}
		// retry after reconnect
		result, err = s.getChunkExec(ctx, decodeContext, input)
	}
	return result, errors.Wrap(err, "get chunk fail")
}

func (s *ObjectClient) getChunkExec(ctx context.Context, decodeContext *chunk.DecodeContext, input chunk.Chunk) (chunk.Chunk, error) {
	if s.querySemaphore != nil {
		if err := s.querySemaphore.Acquire(ctx, 1); err != nil {
			return input, err
		}
		defer s.querySemaphore.Release(1)
	}

	tableName, err := s.schemaCfg.ChunkTableFor(input.From)
	if err != nil {
		return input, err
	}

	var buf []byte
	if err := s.readSession.Query(fmt.Sprintf("SELECT value FROM %s WHERE hash = ?", tableName), s.schemaCfg.ExternalKey(input.ChunkRef)).
		WithContext(ctx).Scan(&buf); err != nil {
		return input, errors.WithStack(err)
	}
	err = input.Decode(decodeContext, buf)
	return input, err
}

func (s *ObjectClient) DeleteChunk(ctx context.Context, userID, chunkID string) error {
	chunkRef, err := chunk.ParseExternalKey(userID, chunkID)
	if err != nil {
		return err
	}

	tableName, err := s.schemaCfg.ChunkTableFor(chunkRef.From)
	if err != nil {
		return err
	}

	q := s.writeSession.Query(fmt.Sprintf("DELETE FROM %s WHERE hash = ?",
		tableName), chunkID)
	if err := q.WithContext(ctx).Exec(); err != nil {
		return errors.WithStack(err)
	}

	return nil
}

func (s *ObjectClient) IsChunkNotFoundErr(_ error) bool {
	return false
}

// Stop implement chunk.ObjectClient.
func (s *ObjectClient) Stop() {
	s.readSession.Close()
	s.writeSession.Close()
}

type noopConvictionPolicy struct{}

// AddFailure should return `true` if the host should be convicted, `false` otherwise.
// Convicted means connections are removed - we don't want that.
// Implementats gocql.ConvictionPolicy.
func (noopConvictionPolicy) AddFailure(err error, host *gocql.HostInfo) bool {
	level.Error(util_log.Logger).Log("msg", "Cassandra host failure", "err", err, "host", host.String())
	return false
}

// Implementats gocql.ConvictionPolicy.
func (noopConvictionPolicy) Reset(host *gocql.HostInfo) {}
