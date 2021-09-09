package cassandra

import (
	"bytes"
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"io/ioutil"
	"strings"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/gocql/gocql"
	"github.com/grafana/dskit/flagext"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/sync/semaphore"

	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/util"
	util_log "github.com/grafana/loki/pkg/util/log"
)

// Config for a StorageClient
type Config struct {
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

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.Addresses, "cassandra.addresses", "", "Comma-separated hostnames or IPs of Cassandra instances.")
	f.IntVar(&cfg.Port, "cassandra.port", 9042, "Port that Cassandra is running on")
	f.StringVar(&cfg.Keyspace, "cassandra.keyspace", "", "Keyspace to use in Cassandra.")
	f.StringVar(&cfg.Consistency, "cassandra.consistency", "QUORUM", "Consistency level for Cassandra.")
	f.IntVar(&cfg.ReplicationFactor, "cassandra.replication-factor", 3, "Replication factor to use in Cassandra.")
	f.BoolVar(&cfg.DisableInitialHostLookup, "cassandra.disable-initial-host-lookup", false, "Instruct the cassandra driver to not attempt to get host info from the system.peers table.")
	f.BoolVar(&cfg.SSL, "cassandra.ssl", false, "Use SSL when connecting to cassandra instances.")
	f.BoolVar(&cfg.HostVerification, "cassandra.host-verification", true, "Require SSL certificate validation.")
	f.StringVar(&cfg.HostSelectionPolicy, "cassandra.host-selection-policy", HostPolicyRoundRobin, "Policy for selecting Cassandra host. Supported values are: round-robin, token-aware.")
	f.StringVar(&cfg.CAPath, "cassandra.ca-path", "", "Path to certificate file to verify the peer.")
	f.StringVar(&cfg.CertPath, "cassandra.tls-cert-path", "", "Path to certificate file used by TLS.")
	f.StringVar(&cfg.KeyPath, "cassandra.tls-key-path", "", "Path to private key file used by TLS.")
	f.BoolVar(&cfg.Auth, "cassandra.auth", false, "Enable password authentication when connecting to cassandra.")
	f.StringVar(&cfg.Username, "cassandra.username", "", "Username to use when connecting to cassandra.")
	f.Var(&cfg.Password, "cassandra.password", "Password to use when connecting to cassandra.")
	f.StringVar(&cfg.PasswordFile, "cassandra.password-file", "", "File containing password to use when connecting to cassandra.")
	f.Var(&cfg.CustomAuthenticators, "cassandra.custom-authenticator", "If set, when authenticating with cassandra a custom authenticator will be expected during the handshake. This flag can be set multiple times.")
	f.DurationVar(&cfg.Timeout, "cassandra.timeout", 2*time.Second, "Timeout when connecting to cassandra.")
	f.DurationVar(&cfg.ConnectTimeout, "cassandra.connect-timeout", 5*time.Second, "Initial connection timeout, used during initial dial to server.")
	f.DurationVar(&cfg.ReconnectInterval, "cassandra.reconnent-interval", 1*time.Second, "Interval to retry connecting to cassandra nodes marked as DOWN.")
	f.IntVar(&cfg.Retries, "cassandra.max-retries", 0, "Number of retries to perform on a request. Set to 0 to disable retries.")
	f.DurationVar(&cfg.MinBackoff, "cassandra.retry-min-backoff", 100*time.Millisecond, "Minimum time to wait before retrying a failed request.")
	f.DurationVar(&cfg.MaxBackoff, "cassandra.retry-max-backoff", 10*time.Second, "Maximum time to wait before retrying a failed request.")
	f.IntVar(&cfg.QueryConcurrency, "cassandra.query-concurrency", 0, "Limit number of concurrent queries to Cassandra. Set to 0 to disable the limit.")
	f.IntVar(&cfg.NumConnections, "cassandra.num-connections", 2, "Number of TCP connections per host.")
	f.BoolVar(&cfg.ConvictHosts, "cassandra.convict-hosts-on-failure", true, "Convict hosts of being down on failure.")
	f.StringVar(&cfg.TableOptions, "cassandra.table-options", "", "Table options used to create index or chunk tables. This value is used as plain text in the table `WITH` like this, \"CREATE TABLE <generated_by_cortex> (...) WITH <cassandra.table-options>\". For details, see https://cortexmetrics.io/docs/production/cassandra. By default it will use the default table options of your Cassandra cluster.")
}

func (cfg *Config) Validate() error {
	if cfg.Password.Value != "" && cfg.PasswordFile != "" {
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

func (cfg *Config) session(name string, reg prometheus.Registerer) (*gocql.Session, error) {
	cluster := gocql.NewCluster(strings.Split(cfg.Addresses, ",")...)
	cluster.Port = cfg.Port
	cluster.Keyspace = cfg.Keyspace
	cluster.BatchObserver = observer{}
	cluster.QueryObserver = observer{}
	cluster.Timeout = cfg.Timeout
	cluster.ConnectTimeout = cfg.ConnectTimeout
	cluster.ReconnectInterval = cfg.ReconnectInterval
	cluster.NumConns = cfg.NumConnections
	cluster.Logger = log.With(util_log.Logger, "module", "gocql", "client", name)
	cluster.Registerer = prometheus.WrapRegistererWith(
		prometheus.Labels{"client": name}, reg)
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
		return nil, errors.WithStack(err)
	}

	session, err := cluster.CreateSession()
	if err == nil {
		return session, nil
	}
	// ErrNoConnectionsStarted will be returned if keyspace don't exist or is invalid.
	// ref. https://github.com/gocql/gocql/blob/07ace3bab0f84bb88477bab5d79ba1f7e1da0169/cassandra_test.go#L85-L97
	if err != gocql.ErrNoConnectionsStarted {
		return nil, errors.WithStack(err)
	}
	// keyspace not exist
	if err := cfg.createKeyspace(); err != nil {
		return nil, errors.WithStack(err)
	}
	session, err = cluster.CreateSession()
	return session, errors.WithStack(err)
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
		password := cfg.Password.Value
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
	cfg            Config
	schemaCfg      chunk.SchemaConfig
	readSession    *gocql.Session
	writeSession   *gocql.Session
	querySemaphore *semaphore.Weighted
}

// NewStorageClient returns a new StorageClient.
func NewStorageClient(cfg Config, schemaCfg chunk.SchemaConfig, registerer prometheus.Registerer) (*StorageClient, error) {
	readSession, err := cfg.session("index-read", registerer)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	writeSession, err := cfg.session("index-write", registerer)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	var querySemaphore *semaphore.Weighted
	if cfg.QueryConcurrency > 0 {
		querySemaphore = semaphore.NewWeighted(int64(cfg.QueryConcurrency))
	}

	client := &StorageClient{
		cfg:            cfg,
		schemaCfg:      schemaCfg,
		readSession:    readSession,
		writeSession:   writeSession,
		querySemaphore: querySemaphore,
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
	entries []chunk.IndexEntry
	deletes []chunk.IndexEntry
}

// NewWriteBatch implement chunk.IndexClient.
func (s *StorageClient) NewWriteBatch() chunk.WriteBatch {
	return &writeBatch{}
}

func (b *writeBatch) Add(tableName, hashValue string, rangeValue []byte, value []byte) {
	b.entries = append(b.entries, chunk.IndexEntry{
		TableName:  tableName,
		HashValue:  hashValue,
		RangeValue: rangeValue,
		Value:      value,
	})
}

func (b *writeBatch) Delete(tableName, hashValue string, rangeValue []byte) {
	b.deletes = append(b.deletes, chunk.IndexEntry{
		TableName:  tableName,
		HashValue:  hashValue,
		RangeValue: rangeValue,
	})
}

// BatchWrite implement chunk.IndexClient.
func (s *StorageClient) BatchWrite(ctx context.Context, batch chunk.WriteBatch) error {
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
func (s *StorageClient) QueryPages(ctx context.Context, queries []chunk.IndexQuery, callback func(chunk.IndexQuery, chunk.ReadBatch) bool) error {
	return util.DoParallelQueries(ctx, s.query, queries, callback)
}

func (s *StorageClient) query(ctx context.Context, query chunk.IndexQuery, callback util.Callback) error {
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

func (r *readBatch) Iterator() chunk.ReadBatchIterator {
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
	cfg            Config
	schemaCfg      chunk.SchemaConfig
	readSession    *gocql.Session
	writeSession   *gocql.Session
	querySemaphore *semaphore.Weighted
}

// NewObjectClient returns a new ObjectClient.
func NewObjectClient(cfg Config, schemaCfg chunk.SchemaConfig, registerer prometheus.Registerer) (*ObjectClient, error) {
	readSession, err := cfg.session("chunks-read", registerer)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	writeSession, err := cfg.session("chunks-write", registerer)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	var querySemaphore *semaphore.Weighted
	if cfg.QueryConcurrency > 0 {
		querySemaphore = semaphore.NewWeighted(int64(cfg.QueryConcurrency))
	}

	client := &ObjectClient{
		cfg:            cfg,
		schemaCfg:      schemaCfg,
		readSession:    readSession,
		writeSession:   writeSession,
		querySemaphore: querySemaphore,
	}
	return client, nil
}

// PutChunks implements chunk.ObjectClient.
func (s *ObjectClient) PutChunks(ctx context.Context, chunks []chunk.Chunk) error {
	for i := range chunks {
		buf, err := chunks[i].Encoded()
		if err != nil {
			return errors.WithStack(err)
		}
		key := chunks[i].ExternalKey()
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
	return util.GetParallelChunks(ctx, input, s.getChunk)
}

func (s *ObjectClient) getChunk(ctx context.Context, decodeContext *chunk.DecodeContext, input chunk.Chunk) (chunk.Chunk, error) {
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
	if err := s.readSession.Query(fmt.Sprintf("SELECT value FROM %s WHERE hash = ?", tableName), input.ExternalKey()).
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
