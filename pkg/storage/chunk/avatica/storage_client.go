package avatica

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"strings"
	"time"

	avatica "github.com/apache/calcite-avatica-go/v5"
	"github.com/grafana/dskit/flagext"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/sync/semaphore"

	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/util"
)

var requestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
	Namespace: "loki",
	Name:      "avatica_request_duration_seconds",
	Help:      "Time spent doing avatica requests.",
	Buckets:   prometheus.ExponentialBuckets(0.001, 4, 9),
}, []string{"operation", "status_code"})

const BackendAlibabacloudLindorm = "alibabacloud_lindorm"

// Config for a StorageClient
type Config struct {
	Addresses        string         `yaml:"addresses"`
	Database         string         `yaml:"database"`
	Username         string         `yaml:"username"`
	Password         flagext.Secret `yaml:"password"`
	QueryConcurrency int            `yaml:"query_concurrency"`
	TableOptions     string         `yaml:"table_options"`
	Backend          string         `yaml:"backend"`
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.Addresses, "avatica.addresses", "", "Comma-separated hostnames or IPs of avatica instances.")
	f.StringVar(&cfg.Database, "avatica.database", "", "database to use .")
	f.StringVar(&cfg.Username, "avatica.username", "", "Username to use when connecting to avatica.")
	f.Var(&cfg.Password, "avatica.password", "Password to use when connecting to avatica.")
	f.StringVar(&cfg.Backend, "avatica.backend", BackendAlibabacloudLindorm, "backend of avatica.")
}

func (cfg *Config) Validate() error {
	return nil
}

func (cfg *Config) session() (*sql.DB, error) {
	conn := avatica.NewConnector(cfg.Addresses).(*avatica.Connector)
	conn.Info = map[string]string{
		"user":     cfg.Username,
		"password": cfg.Password.String(),
		"database": cfg.Database,
	}
	db := sql.OpenDB(conn)
	//TODO: lindorm should support ping()
	//err := db.Ping()
	//if err != nil {
	//	return nil, err
	//}

	// database not exist
	if err := cfg.createDatabase(db); err != nil {
		return nil, errors.WithStack(err)
	}

	db = sql.OpenDB(conn)
	//TODO: lindorm should support ping()
	//err = db.Ping()
	//if err != nil {
	//	return nil, err
	//}
	_, err := db.Exec("USE " + cfg.Database)
	if err != nil {
		return nil, err
	}
	return db, nil
}

// createDatabase will create the desired DATABASE if it doesn't exist.
func (cfg *Config) createDatabase(db *sql.DB) error {
	sql := "CREATE DATABASE IF NOT EXISTS " + cfg.Database
	_, err := db.Exec(sql)
	if err != nil {
		return err
	}
	return nil
}

// StorageClient implements chunk.IndexClient and chunk.ObjectClient for avatica.
type StorageClient struct {
	cfg            Config
	readSession    *sql.DB
	writeSession   *sql.DB
	querySemaphore *semaphore.Weighted
}

// NewStorageClient returns a new StorageClient.
func NewStorageClient(cfg Config, registerer prometheus.Registerer) (*StorageClient, error) {
	if registerer != nil {
		registerer.MustRegister(requestDuration)
	}
	readSession, err := cfg.session()
	if err != nil {
		return nil, errors.WithStack(err)
	}
	writeSession, err := cfg.session()
	if err != nil {
		return nil, errors.WithStack(err)
	}
	var querySemaphore *semaphore.Weighted
	if cfg.QueryConcurrency > 0 {
		querySemaphore = semaphore.NewWeighted(int64(cfg.QueryConcurrency))
	}

	client := &StorageClient{
		cfg:            cfg,
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

// avatica batching isn't really useful in this case, its more to do multiple
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
		querySQL := fmt.Sprintf("INSERT INTO %s (hash, range, value) VALUES (?, ?, ?)",
			entry.TableName)

		err := s.queryInstrumentation(querySQL, func() error {
			_, err := s.writeSession.Query(querySQL, entry.HashValue, entry.RangeValue, entry.Value)
			return err
		})
		if err != nil {
			return errors.WithStack(err)
		}
	}

	for _, entry := range b.deletes {
		querySQL := fmt.Sprintf("DELETE FROM %s WHERE hash = ? and range = ?",
			entry.TableName)
		err := s.queryInstrumentation(querySQL, func() error {
			_, err := s.writeSession.Query(querySQL, entry.HashValue, entry.RangeValue)
			return err
		})
		if err != nil {
			return errors.WithStack(err)
		}

	}

	return nil
}

func (s *StorageClient) queryInstrumentation(query string, queryFunc func() error) error {
	var start time.Time
	var end time.Time
	var err error
	defer func() {
		statusCode := "200"
		if err != nil {
			statusCode = "500"
		}
		parts := strings.SplitN(query, " ", 2)
		requestDuration.WithLabelValues(parts[0], statusCode).Observe(end.Sub(start).Seconds())
	}()
	err = queryFunc()
	end = time.Now()
	if err != nil {
		return errors.WithStack(err)
	}
	return nil
}

// QueryPages implement chunk.IndexClient.
func (s *StorageClient) QueryPages(ctx context.Context, queries []chunk.IndexQuery, callback chunk.QueryPagesCallback) error {
	return util.DoParallelQueries(ctx, s.query, queries, callback)
}

func (s *StorageClient) query(ctx context.Context, query chunk.IndexQuery, callback chunk.QueryPagesCallback) error {
	if s.querySemaphore != nil {
		if err := s.querySemaphore.Acquire(ctx, 1); err != nil {
			return err
		}
		defer s.querySemaphore.Release(1)
	}
	var querySQL string
	var rows *sql.Rows
	var err error
	var queryFunc func() error
	switch {
	case len(query.RangeValuePrefix) > 0 && query.ValueEqual == nil:
		querySQL = fmt.Sprintf("SELECT range, value FROM %s WHERE hash = ? AND range >= ? AND range < ?",
			query.TableName)
		queryFunc = func() error {
			rows, err = s.readSession.Query(querySQL, query.HashValue, query.RangeValuePrefix, append(query.RangeValuePrefix, '\xff'))
			return err
		}
	case len(query.RangeValuePrefix) > 0 && query.ValueEqual != nil:
		querySQL = fmt.Sprintf("SELECT range, value FROM %s WHERE hash = ? AND range >= ? AND range < ? AND value = ? ALLOW FILTERING",
			query.TableName)
		queryFunc = func() error {
			rows, err = s.readSession.Query(querySQL, query.HashValue, query.RangeValuePrefix, append(query.RangeValuePrefix, '\xff'), query.ValueEqual)
			return err
		}
	case len(query.RangeValueStart) > 0 && query.ValueEqual == nil:
		querySQL = fmt.Sprintf("SELECT range, value FROM %s WHERE hash = ? AND range >= ?",
			query.TableName)
		queryFunc = func() error {
			rows, err = s.readSession.Query(querySQL, query.HashValue, query.RangeValueStart)
			return err
		}
	case len(query.RangeValueStart) > 0 && query.ValueEqual != nil:
		querySQL = fmt.Sprintf("SELECT range, value FROM %s WHERE hash = ? AND range >= ? AND value = ? ALLOW FILTERING",
			query.TableName)
		queryFunc = func() error {
			rows, err = s.readSession.Query(querySQL, query.HashValue, query.RangeValueStart, query.ValueEqual)
			return err
		}
	case query.ValueEqual == nil:
		querySQL = fmt.Sprintf("SELECT range, value FROM %s WHERE hash = ?",
			query.TableName)
		queryFunc = func() error {
			rows, err = s.readSession.Query(querySQL, query.HashValue)
			return err
		}
	case query.ValueEqual != nil:
		querySQL = fmt.Sprintf("SELECT range, value FROM %s WHERE hash = ? AND value = ? ALLOW FILTERING",
			query.TableName)
		queryFunc = func() error {
			rows, err = s.readSession.Query(querySQL, query.HashValue, query.ValueEqual)
			return err
		}
	}
	if err != nil {
		return errors.WithStack(err)
	}
	err = s.queryInstrumentation(querySQL, queryFunc)
	if err != nil {
		return errors.WithStack(err)
	}

	for rows.Next() {
		b := &readBatch{}
		err = rows.Scan(&b.rangeValue, &b.value)
		if err != nil {
			return errors.WithStack(err)
		}
		if !callback(query, b) {
			return nil
		}
	}
	return nil
}

// readBatch represents a batch of rows read from avatica.
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
