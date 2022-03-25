package avatica

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"strings"
	"time"

	avatica "github.com/apache/calcite-avatica-go/v5"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/flagext"
	ot "github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
	otlog "github.com/opentracing/opentracing-go/log"
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
	MaxOpenConns     int            `yaml:"max_open_conns"`
	MaxIdleConns     int            `yaml:"max_idle_conns"`
	BackoffConfig    backoff.Config `yaml:"backoff_config"`
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.Addresses, "avatica.addresses", "", "Comma-separated hostnames or IPs of avatica instances.")
	f.StringVar(&cfg.Database, "avatica.database", "", "database to use .")
	f.StringVar(&cfg.Username, "avatica.username", "", "Username to use when connecting to avatica.")
	f.Var(&cfg.Password, "avatica.password", "Password to use when connecting to avatica.")
	f.IntVar(&cfg.QueryConcurrency, "avatica.query-concurrency", 0, "Limit number of concurrent queries to avatica. Set to 0 to disable the limit.")
	f.StringVar(&cfg.Backend, "avatica.backend", BackendAlibabacloudLindorm, "backend of avatica.")
	f.IntVar(&cfg.MaxOpenConns, "avatica.max-open-conns", 16, "sets the maximum number of open connections to the database.")
	f.IntVar(&cfg.MaxIdleConns, "avatica.max-idle-conns", 3, "sets the maximum number of connections in the idle connection pool.")
	f.DurationVar(&cfg.BackoffConfig.MinBackoff, "avatica.min-backoff", 100*time.Millisecond, "Minimum backoff time when avatica query")
	f.DurationVar(&cfg.BackoffConfig.MaxBackoff, "avatica.max-backoff", 3*time.Second, "Maximum backoff time when s3 avatica query")
	f.IntVar(&cfg.BackoffConfig.MaxRetries, "avatica.max-retries", 5, "Maximum number of times to retry when s3 avatica query")
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
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
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
	BackoffConfig  backoff.Config `yaml:"backoff_config"`
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
		retries := backoff.New(ctx, s.cfg.BackoffConfig)
		err := ctx.Err()
		for retries.Ongoing() {
			err = s.queryInstrumentation(ctx, querySQL, func() error {
				rows, err := s.writeSession.Query(querySQL, entry.HashValue, entry.RangeValue, entry.Value)
				if err != nil {
					return nil
				}
				rows.Close()
				return nil
			})
			if err == nil {
				continue
			}
			retries.Wait()
		}
		if err != nil {
			return errors.WithStack(err)
		}
	}

	for _, entry := range b.deletes {
		querySQL := fmt.Sprintf("DELETE FROM %s WHERE hash = ? and range = ?",
			entry.TableName)
		err := s.queryInstrumentation(ctx, querySQL, func() error {
			rows, err := s.writeSession.Query(querySQL, entry.HashValue, entry.RangeValue)
			if err != nil {
				return nil
			}
			rows.Close()
			return nil
		})
		if err != nil {
			return errors.WithStack(err)
		}

	}

	return nil
}

func (s *StorageClient) queryInstrumentation(ctx context.Context, query string, queryFunc func() error) error {
	var start time.Time
	var end time.Time
	var err error
	sp := ot.SpanFromContext(ctx)
	sp.SetTag("sql", query)

	defer func() {
		statusCode := "200"
		if err != nil {
			statusCode = "500"
		}
		parts := strings.SplitN(query, " ", 2)
		requestDuration.WithLabelValues(parts[0], statusCode).Observe(end.Sub(start).Seconds())
	}()
	start = time.Now()
	err = queryFunc()
	end = time.Now()
	if err != nil {
		ext.Error.Set(sp, true)
		sp.LogFields(otlog.String("event", "error"), otlog.String("message", err.Error()))
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

	retries := backoff.New(ctx, s.cfg.BackoffConfig)
	err = ctx.Err()
	for retries.Ongoing() {
		err = s.queryInstrumentation(ctx, querySQL, queryFunc)
		if err == nil {
			continue
		}
		retries.Wait()
	}
	if err != nil {
		return errors.WithStack(err)
	}
	defer rows.Close()
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
