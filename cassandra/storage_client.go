package cassandra

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/gocql/gocql"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/util"
)

const (
	maxRowReads = 100
)

// Config for a StorageClient
type Config struct {
	addresses                string
	port                     int
	keyspace                 string
	consistency              string
	replicationFactor        int
	disableInitialHostLookup bool
	ssl                      bool
	hostVerification         bool
	caPath                   string
	auth                     bool
	username                 string
	password                 string
	timeout                  time.Duration
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.addresses, "cassandra.addresses", "", "Comma-separated hostnames or ips of Cassandra instances.")
	f.IntVar(&cfg.port, "cassandra.port", 9042, "Port that Cassandra is running on")
	f.StringVar(&cfg.keyspace, "cassandra.keyspace", "", "Keyspace to use in Cassandra.")
	f.StringVar(&cfg.consistency, "cassandra.consistency", "QUORUM", "Consistency level for Cassandra.")
	f.IntVar(&cfg.replicationFactor, "cassandra.replication-factor", 1, "Replication factor to use in Cassandra.")
	f.BoolVar(&cfg.disableInitialHostLookup, "cassandra.disable-initial-host-lookup", false, "Instruct the cassandra driver to not attempt to get host info from the system.peers table.")
	f.BoolVar(&cfg.ssl, "cassandra.ssl", false, "Use SSL when connecting to cassandra instances.")
	f.BoolVar(&cfg.hostVerification, "cassandra.host-verification", true, "Require SSL certificate validation.")
	f.StringVar(&cfg.caPath, "cassandra.ca-path", "", "Path to certificate file to verify the peer.")
	f.BoolVar(&cfg.auth, "cassandra.auth", false, "Enable password authentication when connecting to cassandra.")
	f.StringVar(&cfg.username, "cassandra.username", "", "Username to use when connecting to cassandra.")
	f.StringVar(&cfg.password, "cassandra.password", "", "Password to use when connecting to cassandra.")
	f.DurationVar(&cfg.timeout, "cassandra.timeout", 600*time.Millisecond, "Timeout when connecting to cassandra.")
}

func (cfg *Config) session() (*gocql.Session, error) {
	consistency, err := gocql.ParseConsistencyWrapper(cfg.consistency)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	if err := cfg.createKeyspace(); err != nil {
		return nil, errors.WithStack(err)
	}

	cluster := gocql.NewCluster(strings.Split(cfg.addresses, ",")...)
	cluster.Port = cfg.port
	cluster.Keyspace = cfg.keyspace
	cluster.Consistency = consistency
	cluster.BatchObserver = observer{}
	cluster.QueryObserver = observer{}
	cluster.Timeout = cfg.timeout
	cfg.setClusterConfig(cluster)

	return cluster.CreateSession()
}

// apply config settings to a cassandra ClusterConfig
func (cfg *Config) setClusterConfig(cluster *gocql.ClusterConfig) {
	cluster.DisableInitialHostLookup = cfg.disableInitialHostLookup

	if cfg.ssl {
		cluster.SslOpts = &gocql.SslOptions{
			CaPath:                 cfg.caPath,
			EnableHostVerification: cfg.hostVerification,
		}
	}
	if cfg.auth {
		cluster.Authenticator = gocql.PasswordAuthenticator{
			Username: cfg.username,
			Password: cfg.password,
		}
	}
}

// createKeyspace will create the desired keyspace if it doesn't exist.
func (cfg *Config) createKeyspace() error {
	cluster := gocql.NewCluster(strings.Split(cfg.addresses, ",")...)
	cluster.Port = cfg.port
	cluster.Keyspace = "system"
	cluster.Timeout = 20 * time.Second

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
		cfg.keyspace, cfg.replicationFactor)).Exec()
	return errors.WithStack(err)
}

// storageClient implements chunk.storageClient for GCP.
type storageClient struct {
	cfg       Config
	schemaCfg chunk.SchemaConfig
	session   *gocql.Session
}

// Opts returns the chunk.StorageOpt's for the config.
func Opts(cfg Config, schemaCfg chunk.SchemaConfig) ([]chunk.StorageOpt, error) {
	client, err := NewStorageClient(cfg, schemaCfg)
	if err != nil {
		return nil, err
	}
	return []chunk.StorageOpt{{
		From:   model.Time(0),
		Client: client,
	}}, err
}

// NewStorageClient returns a new StorageClient.
func NewStorageClient(cfg Config, schemaCfg chunk.SchemaConfig) (chunk.StorageClient, error) {
	session, err := cfg.session()
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return &storageClient{
		cfg:       cfg,
		schemaCfg: schemaCfg,
		session:   session,
	}, nil
}

func (s *storageClient) Close() {
	s.session.Close()
}

// Cassandra batching isn't really useful in this case, its more to do multiple
// atomic writes.  Therefore we just do a bunch of writes in parallel.
type writeBatch struct {
	entries []chunk.IndexEntry
}

func (s *storageClient) NewWriteBatch() chunk.WriteBatch {
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

func (s *storageClient) BatchWrite(ctx context.Context, batch chunk.WriteBatch) error {
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

func (s *storageClient) QueryPages(ctx context.Context, queries []chunk.IndexQuery, callback func(chunk.IndexQuery, chunk.ReadBatch) bool) error {
	return util.DoParallelQueries(ctx, s.query, queries, callback)
}

func (s *storageClient) query(ctx context.Context, query chunk.IndexQuery, callback func(result chunk.ReadBatch) (shouldContinue bool)) error {
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

func (s *storageClient) PutChunks(ctx context.Context, chunks []chunk.Chunk) error {
	for i := range chunks {
		// Encode the chunk first - checksum is calculated as a side effect.
		buf, err := chunks[i].Encode()
		if err != nil {
			return errors.WithStack(err)
		}
		key := chunks[i].ExternalKey()
		tableName := s.schemaCfg.ChunkTables.TableFor(chunks[i].From)

		// Must provide a range key, even though its not useds - hence 0x00.
		q := s.session.Query(fmt.Sprintf("INSERT INTO %s (hash, range, value) VALUES (?, 0x00, ?)",
			tableName), key, buf)
		if err := q.WithContext(ctx).Exec(); err != nil {
			return errors.WithStack(err)
		}
	}

	return nil
}

func (s *storageClient) GetChunks(ctx context.Context, input []chunk.Chunk) ([]chunk.Chunk, error) {
	outs := make(chan chunk.Chunk, len(input))
	errs := make(chan error, len(input))

	for i := 0; i < len(input); i++ {
		c := input[i]
		go func(c chunk.Chunk) {
			out, err := s.getChunk(ctx, c)
			if err != nil {
				errs <- err
			} else {
				outs <- out
			}
		}(c)
	}

	output := make([]chunk.Chunk, 0, len(input))
	for i := 0; i < len(input); i++ {
		select {
		case c := <-outs:
			output = append(output, c)
		case err := <-errs:
			return nil, err
		}
	}

	return output, nil
}

func (s *storageClient) getChunk(ctx context.Context, input chunk.Chunk) (chunk.Chunk, error) {
	tableName := s.schemaCfg.ChunkTables.TableFor(input.From)
	var buf []byte
	if err := s.session.Query(fmt.Sprintf("SELECT value FROM %s WHERE hash = ?", tableName), input.ExternalKey()).
		WithContext(ctx).Scan(&buf); err != nil {
		return input, errors.WithStack(err)
	}
	decodeContext := chunk.NewDecodeContext()
	err := input.Decode(decodeContext, buf)
	return input, err
}
