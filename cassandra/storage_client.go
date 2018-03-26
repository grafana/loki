package cassandra

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/gocql/gocql"

	"github.com/weaveworks/cortex/pkg/chunk"
)

const (
	maxRowReads = 100
)

// Config for a StorageClient
type Config struct {
	addresses         string
	keyspace          string
	consistency       string
	replicationFactor int
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.addresses, "cassandra.addresses", "", "Comma-separated addresses of Cassandra instances.")
	f.StringVar(&cfg.keyspace, "cassandra.keyspace", "", "Keyspace to use in Cassandra.")
	f.StringVar(&cfg.consistency, "cassandra.consistency", "QUORUM", "Consistency level for Cassandra.")
	f.IntVar(&cfg.replicationFactor, "cassandra.replication-factor", 1, "Replication factor to use in Cassandra.")
}

func (cfg *Config) session() (*gocql.Session, error) {
	consistency, err := gocql.ParseConsistencyWrapper(cfg.consistency)
	if err != nil {
		return nil, err
	}

	if err := cfg.createKeyspace(); err != nil {
		return nil, err
	}

	cluster := gocql.NewCluster(strings.Split(cfg.addresses, ",")...)
	cluster.Keyspace = cfg.keyspace
	cluster.Consistency = consistency
	cluster.BatchObserver = observer{}
	cluster.QueryObserver = observer{}

	return cluster.CreateSession()
}

// createKeyspace will create the desired keyspace if it doesn't exist.
func (cfg *Config) createKeyspace() error {
	cluster := gocql.NewCluster(strings.Split(cfg.addresses, ",")...)
	cluster.Keyspace = "system"
	cluster.Timeout = 20 * time.Second
	session, err := cluster.CreateSession()
	if err != nil {
		return err
	}
	defer session.Close()

	return session.Query(fmt.Sprintf(
		`CREATE KEYSPACE IF NOT EXISTS %s
		 WITH replication = {
			 'class' : 'SimpleStrategy',
			 'replication_factor' : %d
		 }`,
		cfg.keyspace, cfg.replicationFactor)).Exec()
}

// storageClient implements chunk.storageClient for GCP.
type storageClient struct {
	cfg       Config
	schemaCfg chunk.SchemaConfig
	session   *gocql.Session
}

// NewStorageClient returns a new StorageClient.
func NewStorageClient(cfg Config, schemaCfg chunk.SchemaConfig) (chunk.StorageClient, error) {
	session, err := cfg.session()
	if err != nil {
		return nil, err
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

type writeBatch struct {
	b *gocql.Batch
}

func (s *storageClient) NewWriteBatch() chunk.WriteBatch {
	return writeBatch{
		b: gocql.NewBatch(gocql.UnloggedBatch),
	}
}

func (b writeBatch) Add(tableName, hashValue string, rangeValue []byte, value []byte) {
	b.b.Query(fmt.Sprintf("INSERT INTO %s (hash, range, value) VALUES (?, ?, ?)", tableName),
		hashValue, rangeValue, value)
}

func (s *storageClient) BatchWrite(ctx context.Context, batch chunk.WriteBatch) error {
	cassandraBatch := batch.(writeBatch)
	return s.session.ExecuteBatch(cassandraBatch.b.WithContext(ctx))
}

func (s *storageClient) QueryPages(ctx context.Context, query chunk.IndexQuery, callback func(result chunk.ReadBatch, lastPage bool) (shouldContinue bool)) error {
	var q *gocql.Query
	if len(query.RangeValuePrefix) > 0 {
		q = s.session.Query(fmt.Sprintf("SELECT range, value FROM %s WHERE hash = ? AND range >= ?", query.TableName),
			query.HashValue, query.RangeValuePrefix)
	} else if len(query.RangeValueStart) > 0 {
		q = s.session.Query(fmt.Sprintf("SELECT range, value FROM %s WHERE hash = ? AND range >= ?", query.TableName),
			query.HashValue, query.RangeValueStart)
	} else {
		q = s.session.Query(fmt.Sprintf("SELECT range, value FROM %s WHERE hash = ?", query.TableName),
			query.HashValue)
	}

	iter := q.WithContext(ctx).Iter()
	defer iter.Close()
	scanner := iter.Scanner()
	for scanner.Next() {
		var b readBatch
		if err := scanner.Scan(&b.rangeValue, &b.value); err != nil {
			return err
		}
		if callback(b, false) {
			return nil
		}
	}
	return scanner.Err()
}

// readBatch represents a batch of rows read from Cassandra.
type readBatch struct {
	rangeValue []byte
	value      []byte
}

// Len implements chunk.ReadBatch; in Cassandra we 'stream' results back
// one-by-one, so this always returns 1.
func (readBatch) Len() int {
	return 1
}

func (b readBatch) RangeValue(index int) []byte {
	if index != 0 {
		panic("index != 0")
	}
	return b.rangeValue
}

func (b readBatch) Value(index int) []byte {
	if index != 0 {
		panic("index != 0")
	}
	return b.value
}

func (s *storageClient) PutChunks(ctx context.Context, chunks []chunk.Chunk) error {
	b := gocql.NewBatch(gocql.UnloggedBatch).WithContext(ctx)

	for i := range chunks {
		// Encode the chunk first - checksum is calculated as a side effect.
		buf, err := chunks[i].Encode()
		if err != nil {
			return err
		}
		key := chunks[i].ExternalKey()
		tableName := s.schemaCfg.ChunkTables.TableFor(chunks[i].From)

		// Must provide a range key, even though its not useds - hence 0x00.
		b.Query(fmt.Sprintf("INSERT INTO %s (hash, range, value) VALUES (?, 0x00, ?)", tableName), key, buf)
	}

	return s.session.ExecuteBatch(b)
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
		return input, err
	}
	decodeContext := chunk.NewDecodeContext()
	err := input.Decode(decodeContext, buf)
	return input, err
}
