package gcp

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"strings"

	"cloud.google.com/go/bigtable"
	ot "github.com/opentracing/opentracing-go"
	otlog "github.com/opentracing/opentracing-go/log"

	"github.com/pkg/errors"
	"github.com/weaveworks/cortex/pkg/chunk"
	chunk_util "github.com/weaveworks/cortex/pkg/chunk/util"
	"github.com/weaveworks/cortex/pkg/util"
)

const (
	columnFamily = "f"
	columnPrefix = columnFamily + ":"
	column       = "c"
	separator    = "\000"
	maxRowReads  = 100
)

// Config for a StorageClient
type Config struct {
	project  string
	instance string

	ColumnKey bool
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.project, "bigtable.project", "", "Bigtable project ID.")
	f.StringVar(&cfg.instance, "bigtable.instance", "", "Bigtable instance ID.")
}

// NewStorageClient returns a new StorageClient.
func NewStorageClient(ctx context.Context, cfg Config, schemaCfg chunk.SchemaConfig) (chunk.StorageClient, error) {
	if cfg.ColumnKey {
		return NewStorageClientColumnKey(ctx, cfg, schemaCfg)
	}
	return NewStorageClientV1(ctx, cfg, schemaCfg)
}

// storageClientColumnKey implements chunk.storageClient for GCP.
type storageClientColumnKey struct {
	cfg       Config
	schemaCfg chunk.SchemaConfig
	client    *bigtable.Client
	keysFn    keysFn
}

// storageClientV1 implements chunk.storageClient for GCP.
type storageClientV1 struct {
	storageClientColumnKey
}

// NewStorageClientV1 returns a new v1 StorageClient.
func NewStorageClientV1(ctx context.Context, cfg Config, schemaCfg chunk.SchemaConfig) (chunk.StorageClient, error) {
	client, err := bigtable.NewClient(ctx, cfg.project, cfg.instance, instrumentation()...)
	if err != nil {
		return nil, err
	}
	return newStorageClientV1(cfg, client, schemaCfg), nil
}

func newStorageClientV1(cfg Config, client *bigtable.Client, schemaCfg chunk.SchemaConfig) *storageClientV1 {
	return &storageClientV1{
		storageClientColumnKey{
			cfg:       cfg,
			schemaCfg: schemaCfg,
			client:    client,
			keysFn: func(hashValue string, rangeValue []byte) (string, string) {
				rowKey := hashValue + separator + string(rangeValue)
				return rowKey, column
			},
		},
	}
}

// NewStorageClientColumnKey returns a new v2 StorageClient.
func NewStorageClientColumnKey(ctx context.Context, cfg Config, schemaCfg chunk.SchemaConfig) (chunk.StorageClient, error) {
	client, err := bigtable.NewClient(ctx, cfg.project, cfg.instance, instrumentation()...)
	if err != nil {
		return nil, err
	}

	return newStorageClientColumnKey(cfg, client, schemaCfg), nil
}

func newStorageClientColumnKey(cfg Config, client *bigtable.Client, schemaCfg chunk.SchemaConfig) *storageClientColumnKey {
	return &storageClientColumnKey{
		cfg:       cfg,
		schemaCfg: schemaCfg,
		client:    client,
		keysFn: func(hashValue string, rangeValue []byte) (string, string) {
			// We could hash the row key for better distribution but we decided against it
			// because that would make migrations very, very hard.
			return hashValue, string(rangeValue)
		},
	}
}

func (s *storageClientColumnKey) NewWriteBatch() chunk.WriteBatch {
	return bigtableWriteBatch{
		tables: map[string]map[string]*bigtable.Mutation{},
		keysFn: s.keysFn,
	}
}

// keysFn returns the row and column keys for the given hash and range keys.
type keysFn func(hashValue string, rangeValue []byte) (rowKey, columnKey string)

type bigtableWriteBatch struct {
	tables map[string]map[string]*bigtable.Mutation
	keysFn keysFn
}

func (b bigtableWriteBatch) Add(tableName, hashValue string, rangeValue []byte, value []byte) {
	rows, ok := b.tables[tableName]
	if !ok {
		rows = map[string]*bigtable.Mutation{}
		b.tables[tableName] = rows
	}

	rowKey, columnKey := b.keysFn(hashValue, rangeValue)
	mutation, ok := rows[rowKey]
	if !ok {
		mutation = bigtable.NewMutation()
		rows[rowKey] = mutation
	}

	mutation.Set(columnFamily, columnKey, 0, value)
}

func (s *storageClientColumnKey) BatchWrite(ctx context.Context, batch chunk.WriteBatch) error {
	bigtableBatch := batch.(bigtableWriteBatch)

	for tableName, rows := range bigtableBatch.tables {
		table := s.client.Open(tableName)
		rowKeys := make([]string, 0, len(rows))
		muts := make([]*bigtable.Mutation, 0, len(rows))
		for rowKey, mut := range rows {
			rowKeys = append(rowKeys, rowKey)
			muts = append(muts, mut)
		}

		errs, err := table.ApplyBulk(ctx, rowKeys, muts)
		if err != nil {
			return err
		}
		for _, err := range errs {
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (s *storageClientColumnKey) QueryPages(ctx context.Context, queries []chunk.IndexQuery, callback func(chunk.IndexQuery, chunk.ReadBatch) bool) error {
	sp, ctx := ot.StartSpanFromContext(ctx, "QueryPages")
	defer sp.Finish()

	// A limitation of this approach is that this only fetches whole rows; but
	// whatever, we filter them in the cache on the client.  But for unit tests to
	// pass, we must do this.
	callback = chunk_util.QueryFilter(callback)

	type tableQuery struct {
		name    string
		queries map[string]chunk.IndexQuery
		rows    bigtable.RowList
	}

	tableQueries := map[string]tableQuery{}
	for _, query := range queries {
		tq, ok := tableQueries[query.TableName]
		if !ok {
			tq = tableQuery{
				name:    query.TableName,
				queries: map[string]chunk.IndexQuery{},
			}
		}
		tq.queries[query.HashValue] = query
		tq.rows = append(tq.rows, query.HashValue)
		tableQueries[query.TableName] = tq
	}

	errs := make(chan error)

	for _, tq := range tableQueries {

		table := s.client.Open(tq.name)
		for i := 0; i < len(tq.rows); i += maxRowReads {

			page := tq.rows[i:util.Min(i+maxRowReads, len(tq.rows))]
			go func(page bigtable.RowList, tq tableQuery) {
				var processingErr error
				// rows are returned in key order, not order in row list
				err := table.ReadRows(ctx, page, func(row bigtable.Row) bool {

					query, ok := tq.queries[row.Key()]
					if !ok {
						processingErr = errors.WithStack(fmt.Errorf("Got row for unknown chunk: %s", row.Key()))
						return false
					}

					val, ok := row[columnFamily]
					if !ok {
						// There are no matching rows.
						return true
					}

					return callback(query, &bigtableReadBatchColumnKey{
						i:     -1,
						items: val,
					})
				})

				if processingErr != nil {
					errs <- processingErr
				} else {
					errs <- err
				}
			}(page, tq)
		}
	}

	var lastErr error
	for _, tq := range tableQueries {
		for i := 0; i < len(tq.rows); i += maxRowReads {
			err := <-errs
			if err != nil {
				lastErr = err
			}
		}
	}
	return lastErr
}

func (s *storageClientColumnKey) query(ctx context.Context, query chunk.IndexQuery, callback func(result chunk.ReadBatch) (shouldContinue bool)) error {
	const null = string('\xff')

	sp, ctx := ot.StartSpanFromContext(ctx, "QueryPages", ot.Tag{Key: "tableName", Value: query.TableName}, ot.Tag{Key: "hashValue", Value: query.HashValue})
	defer sp.Finish()

	table := s.client.Open(query.TableName)

	rOpts := []bigtable.ReadOption{
		bigtable.RowFilter(bigtable.FamilyFilter(columnFamily)),
	}

	if len(query.RangeValuePrefix) > 0 {
		rOpts = append(rOpts, bigtable.RowFilter(bigtable.ColumnRangeFilter(columnFamily, string(query.RangeValuePrefix), string(query.RangeValuePrefix)+null)))
	} else if len(query.RangeValueStart) > 0 {
		rOpts = append(rOpts, bigtable.RowFilter(bigtable.ColumnRangeFilter(columnFamily, string(query.RangeValueStart), null)))
	}

	r, err := table.ReadRow(ctx, query.HashValue, rOpts...)
	if err != nil {
		sp.LogFields(otlog.String("error", err.Error()))
		return errors.WithStack(err)
	}

	val, ok := r[columnFamily]
	if !ok {
		// There are no matching rows.
		return nil
	}

	if query.ValueEqual != nil {
		filteredItems := make([]bigtable.ReadItem, 0, len(val))
		for _, item := range val {
			if bytes.Equal(query.ValueEqual, item.Value) {
				filteredItems = append(filteredItems, item)
			}
		}

		val = filteredItems
	}
	callback(&bigtableReadBatchColumnKey{
		i:     -1,
		items: val,
	})
	return nil
}

// bigtableReadBatchColumnKey represents a batch of values read from Bigtable.
type bigtableReadBatchColumnKey struct {
	i     int
	items []bigtable.ReadItem
}

func (b *bigtableReadBatchColumnKey) Next() bool {
	b.i++
	return b.i < len(b.items)
}

func (b *bigtableReadBatchColumnKey) RangeValue() []byte {
	return []byte(strings.TrimPrefix(b.items[b.i].Column, columnPrefix))
}

func (b *bigtableReadBatchColumnKey) Value() []byte {
	return b.items[b.i].Value
}

func (s *storageClientColumnKey) PutChunks(ctx context.Context, chunks []chunk.Chunk) error {
	keys := map[string][]string{}
	muts := map[string][]*bigtable.Mutation{}

	for i := range chunks {
		// Encode the chunk first - checksum is calculated as a side effect.
		buf, err := chunks[i].Encode()
		if err != nil {
			return err
		}
		key := chunks[i].ExternalKey()
		tableName := s.schemaCfg.ChunkTables.TableFor(chunks[i].From)
		keys[tableName] = append(keys[tableName], key)

		mut := bigtable.NewMutation()
		mut.Set(columnFamily, column, 0, buf)
		muts[tableName] = append(muts[tableName], mut)
	}

	for tableName := range keys {
		table := s.client.Open(tableName)
		errs, err := table.ApplyBulk(ctx, keys[tableName], muts[tableName])
		if err != nil {
			return err
		}
		for _, err := range errs {
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *storageClientColumnKey) GetChunks(ctx context.Context, input []chunk.Chunk) ([]chunk.Chunk, error) {
	sp, ctx := ot.StartSpanFromContext(ctx, "GetChunks")
	defer sp.Finish()
	sp.LogFields(otlog.Int("chunks requested", len(input)))

	chunks := map[string]map[string]chunk.Chunk{}
	keys := map[string]bigtable.RowList{}
	for _, c := range input {
		tableName := s.schemaCfg.ChunkTables.TableFor(c.From)
		key := c.ExternalKey()
		keys[tableName] = append(keys[tableName], key)
		if _, ok := chunks[tableName]; !ok {
			chunks[tableName] = map[string]chunk.Chunk{}
		}
		chunks[tableName][key] = c
	}

	outs := make(chan chunk.Chunk, len(input))
	errs := make(chan error, len(input))

	for tableName := range keys {
		var (
			table  = s.client.Open(tableName)
			keys   = keys[tableName]
			chunks = chunks[tableName]
		)

		for i := 0; i < len(keys); i += maxRowReads {
			page := keys[i:util.Min(i+maxRowReads, len(keys))]
			go func(page bigtable.RowList) {
				decodeContext := chunk.NewDecodeContext()

				var processingErr error
				var recievedChunks = 0

				// rows are returned in key order, not order in row list
				err := table.ReadRows(ctx, page, func(row bigtable.Row) bool {
					chunk, ok := chunks[row.Key()]
					if !ok {
						processingErr = errors.WithStack(fmt.Errorf("Got row for unknown chunk: %s", row.Key()))
						return false
					}

					err := chunk.Decode(decodeContext, row[columnFamily][0].Value)
					if err != nil {
						processingErr = err
						return false
					}

					recievedChunks++
					outs <- chunk
					return true
				})

				if processingErr != nil {
					errs <- processingErr
				} else if err != nil {
					errs <- errors.WithStack(err)
				} else if recievedChunks < len(page) {
					errs <- errors.WithStack(fmt.Errorf("Asked for %d chunks for BigTable, received %d", len(page), recievedChunks))
				}
			}(page)
		}
	}

	output := make([]chunk.Chunk, 0, len(input))
	for i := 0; i < len(input); i++ {
		select {
		case c := <-outs:
			output = append(output, c)
		case err := <-errs:
			return nil, err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	return output, nil
}

func (s *storageClientV1) QueryPages(ctx context.Context, queries []chunk.IndexQuery, callback func(chunk.IndexQuery, chunk.ReadBatch) bool) error {
	return chunk_util.DoParallelQueries(ctx, s.query, queries, callback)
}

func (s *storageClientV1) query(ctx context.Context, query chunk.IndexQuery, callback func(result chunk.ReadBatch) (shouldContinue bool)) error {
	const null = string('\xff')

	sp, ctx := ot.StartSpanFromContext(ctx, "QueryPages", ot.Tag{Key: "tableName", Value: query.TableName}, ot.Tag{Key: "hashValue", Value: query.HashValue})
	defer sp.Finish()

	table := s.client.Open(query.TableName)

	var rowRange bigtable.RowRange

	/* BigTable only seems to support regex match on cell values, so doing it
	   client side for now
	readOpts := []bigtable.ReadOption{
		bigtable.RowFilter(bigtable.FamilyFilter(columnFamily)),
	}
	if query.ValueEqual != nil {
		readOpts = append(readOpts, bigtable.RowFilter(bigtable.ValueFilter(string(query.ValueEqual))))
	}
	*/

	if len(query.RangeValuePrefix) > 0 {
		rowRange = bigtable.PrefixRange(query.HashValue + separator + string(query.RangeValuePrefix))
	} else if len(query.RangeValueStart) > 0 {
		rowRange = bigtable.NewRange(query.HashValue+separator+string(query.RangeValueStart), query.HashValue+separator+null)
	} else {
		rowRange = bigtable.PrefixRange(query.HashValue + separator)
	}

	err := table.ReadRows(ctx, rowRange, func(r bigtable.Row) bool {
		if query.ValueEqual == nil || bytes.Equal(r[columnFamily][0].Value, query.ValueEqual) {
			return callback(&bigtableReadBatchV1{
				row: r,
			})
		}

		return true
	})
	if err != nil {
		sp.LogFields(otlog.String("error", err.Error()))
		return errors.WithStack(err)
	}
	return nil
}

// bigtableReadBatchV1 represents a batch of rows read from Bigtable.  As the
// bigtable interface gives us rows one-by-one, a batch always only contains
// a single row.
type bigtableReadBatchV1 struct {
	consumed bool
	row      bigtable.Row
}

func (b *bigtableReadBatchV1) Next() bool {
	if b.consumed {
		return false
	}
	b.consumed = true
	return true
}

func (b *bigtableReadBatchV1) RangeValue() []byte {
	// String before the first separator is the hashkey
	parts := strings.SplitN(b.row.Key(), separator, 2)
	return []byte(parts[1])
}

func (b *bigtableReadBatchV1) Value() []byte {
	cf, ok := b.row[columnFamily]
	if !ok || len(cf) != 1 {
		panic("bad response from bigtable")
	}
	return cf[0].Value
}
