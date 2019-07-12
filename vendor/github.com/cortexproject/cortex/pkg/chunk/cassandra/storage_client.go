package cassandra

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/gocql/gocql"
	"github.com/pkg/errors"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/util"
)

const (
	maxRowReads = 100
)

// Config for a StorageClient
type Config struct {
	Addresses                string        `yaml:"addresses,omitempty"`
	Port                     int           `yaml:"port,omitempty"`
	Keyspace                 string        `yaml:"keyspace,omitempty"`
	Consistency              string        `yaml:"consistency,omitempty"`
	ReplicationFactor        int           `yaml:"replication_factor,omitempty"`
	DisableInitialHostLookup bool          `yaml:"disable_initial_host_lookup,omitempty"`
	SSL                      bool          `yaml:"SSL,omitempty"`
	HostVerification         bool          `yaml:"host_verification,omitempty"`
	CAPath                   string        `yaml:"CA_path,omitempty"`
	Auth                     bool          `yaml:"auth,omitempty"`
	Username                 string        `yaml:"username,omitempty"`
	Password                 string        `yaml:"password,omitempty"`
	Timeout                  time.Duration `yaml:"timeout,omitempty"`
	ConnectTimeout           time.Duration `yaml:"connect_timeout,omitempty"`
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.Addresses, "cassandra.addresses", "", "Comma-separated hostnames or IPs of Cassandra instances.")
	f.IntVar(&cfg.Port, "cassandra.port", 9042, "Port that Cassandra is running on")
	f.StringVar(&cfg.Keyspace, "cassandra.keyspace", "", "Keyspace to use in Cassandra.")
	f.StringVar(&cfg.Consistency, "cassandra.consistency", "QUORUM", "Consistency level for Cassandra.")
	f.IntVar(&cfg.ReplicationFactor, "cassandra.replication-factor", 1, "Replication factor to use in Cassandra.")
	f.BoolVar(&cfg.DisableInitialHostLookup, "cassandra.disable-initial-host-lookup", false, "Instruct the cassandra driver to not attempt to get host info from the system.peers table.")
	f.BoolVar(&cfg.SSL, "cassandra.ssl", false, "Use SSL when connecting to cassandra instances.")
	f.BoolVar(&cfg.HostVerification, "cassandra.host-verification", true, "Require SSL certificate validation.")
	f.StringVar(&cfg.CAPath, "cassandra.ca-path", "", "Path to certificate file to verify the peer.")
	f.BoolVar(&cfg.Auth, "cassandra.auth", false, "Enable password authentication when connecting to cassandra.")
	f.StringVar(&cfg.Username, "cassandra.username", "", "Username to use when connecting to cassandra.")
	f.StringVar(&cfg.Password, "cassandra.password", "", "Password to use when connecting to cassandra.")
	f.DurationVar(&cfg.Timeout, "cassandra.timeout", 600*time.Millisecond, "Timeout when connecting to cassandra.")
	f.DurationVar(&cfg.ConnectTimeout, "cassandra.connect-timeout", 600*time.Millisecond, "Initial connection timeout, used during initial dial to server.")
}

func (cfg *Config) session() (*gocql.Session, error) {
	consistency, err := gocql.ParseConsistencyWrapper(cfg.Consistency)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	if err := cfg.createKeyspace(); err != nil {
		return nil, errors.WithStack(err)
	}

	cluster := gocql.NewCluster(strings.Split(cfg.Addresses, ",")...)
	cluster.Port = cfg.Port
	cluster.Keyspace = cfg.Keyspace
	cluster.Consistency = consistency
	cluster.BatchObserver = observer{}
	cluster.QueryObserver = observer{}
	cluster.Timeout = cfg.Timeout
	cluster.ConnectTimeout = cfg.ConnectTimeout
	cfg.setClusterConfig(cluster)

	return cluster.CreateSession()
}

// apply config settings to a cassandra ClusterConfig
func (cfg *Config) setClusterConfig(cluster *gocql.ClusterConfig) {
	cluster.DisableInitialHostLookup = cfg.DisableInitialHostLookup

	if cfg.SSL {
		cluster.SslOpts = &gocql.SslOptions{
			CaPath:                 cfg.CAPath,
			EnableHostVerification: cfg.HostVerification,
		}
	}
	if cfg.Auth {
		cluster.Authenticator = gocql.PasswordAuthenticator{
			Username: cfg.Username,
			Password: cfg.Password,
		}
	}
}

// createKeyspace will create the desired keyspace if it doesn't exist.
func (cfg *Config) createKeyspace() error {
	cluster := gocql.NewCluster(strings.Split(cfg.Addresses, ",")...)
	cluster.Port = cfg.Port
	cluster.Keyspace = "system"
	cluster.Timeout = 20 * time.Second
	cluster.ConnectTimeout = 20 * time.Second

	cfg.setClusterConfig(cluster)

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
	cfg       Config
	schemaCfg chunk.SchemaConfig
	session   *gocql.Session
}

// NewStorageClient returns a new StorageClient.
func NewStorageClient(cfg Config, schemaCfg chunk.SchemaConfig) (*StorageClient, error) {
	session, err := cfg.session()
	if err != nil {
		return nil, errors.WithStack(err)
	}

	client := &StorageClient{
		cfg:       cfg,
		schemaCfg: schemaCfg,
		session:   session,
	}
	return client, nil
}

// Stop implement chunk.IndexClient.
func (s *StorageClient) Stop() {
	s.session.Close()
}

// Cassandra batching isn't really useful in this case, its more to do multiple
// atomic writes.  Therefore we just do a bunch of writes in parallel.
type writeBatch struct {
	entries []chunk.IndexEntry
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

// BatchWrite implement chunk.IndexClient.
func (s *StorageClient) BatchWrite(ctx context.Context, batch chunk.WriteBatch) error {
	b := batch.(*writeBatch)

	for _, entry := range b.entries {
		err := s.session.Query(fmt.Sprintf("INSERT INTO %s (hash, range, value) VALUES (?, ?, ?)",
			entry.TableName), entry.HashValue, entry.RangeValue, entry.Value).WithContext(ctx).Exec()
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

func (s *StorageClient) query(ctx context.Context, query chunk.IndexQuery, callback func(result chunk.ReadBatch) (shouldContinue bool)) error {
	var q *gocql.Query

	switch {
	case len(query.RangeValuePrefix) > 0 && query.ValueEqual == nil:
		q = s.session.Query(fmt.Sprintf("SELECT range, value FROM %s WHERE hash = ? AND range >= ? AND range < ?",
			query.TableName), query.HashValue, query.RangeValuePrefix, append(query.RangeValuePrefix, '\xff'))

	case len(query.RangeValuePrefix) > 0 && query.ValueEqual != nil:
		q = s.session.Query(fmt.Sprintf("SELECT range, value FROM %s WHERE hash = ? AND range >= ? AND range < ? AND value = ? ALLOW FILTERING",
			query.TableName), query.HashValue, query.RangeValuePrefix, append(query.RangeValuePrefix, '\xff'), query.ValueEqual)

	case len(query.RangeValueStart) > 0 && query.ValueEqual == nil:
		q = s.session.Query(fmt.Sprintf("SELECT range, value FROM %s WHERE hash = ? AND range >= ?",
			query.TableName), query.HashValue, query.RangeValueStart)

	case len(query.RangeValueStart) > 0 && query.ValueEqual != nil:
		q = s.session.Query(fmt.Sprintf("SELECT range, value FROM %s WHERE hash = ? AND range >= ? AND value = ? ALLOW FILTERING",
			query.TableName), query.HashValue, query.RangeValueStart, query.ValueEqual)

	case query.ValueEqual == nil:
		q = s.session.Query(fmt.Sprintf("SELECT range, value FROM %s WHERE hash = ?",
			query.TableName), query.HashValue)

	case query.ValueEqual != nil:
		q = s.session.Query(fmt.Sprintf("SELECT range, value FROM %s WHERE hash = ? value = ? ALLOW FILTERING",
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
		if !callback(b) {
			return nil
		}
	}
	return errors.WithStack(scanner.Err())
}

// readBatch represents a batch of rows read from Cassandra.
type readBatch struct {
	consumed   bool
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

// PutChunks implements chunk.ObjectClient.
func (s *StorageClient) PutChunks(ctx context.Context, chunks []chunk.Chunk) error {
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
		q := s.session.Query(fmt.Sprintf("INSERT INTO %s (hash, range, value) VALUES (?, 0x00, ?)",
			tableName), key, buf)
		if err := q.WithContext(ctx).Exec(); err != nil {
			return errors.WithStack(err)
		}
	}

	return nil
}

// GetChunks implements chunk.ObjectClient.
func (s *StorageClient) GetChunks(ctx context.Context, input []chunk.Chunk) ([]chunk.Chunk, error) {
	return util.GetParallelChunks(ctx, input, s.getChunk)
}

func (s *StorageClient) getChunk(ctx context.Context, decodeContext *chunk.DecodeContext, input chunk.Chunk) (chunk.Chunk, error) {
	tableName, err := s.schemaCfg.ChunkTableFor(input.From)
	if err != nil {
		return input, err
	}

	var buf []byte
	if err := s.session.Query(fmt.Sprintf("SELECT value FROM %s WHERE hash = ?", tableName), input.ExternalKey()).
		WithContext(ctx).Scan(&buf); err != nil {
		return input, errors.WithStack(err)
	}
	err = input.Decode(decodeContext, buf)
	return input, err
}
