package index

import (
	"context"
)

// QueryPagesCallback from an IndexQuery.
type QueryPagesCallback func(Query, ReadBatchResult) bool

// Client for the read path.
type ReadClient interface {
	QueryPages(ctx context.Context, queries []Query, callback QueryPagesCallback) error
}

// Client for the write path.
type WriteClient interface {
	NewWriteBatch() WriteBatch
	BatchWrite(context.Context, WriteBatch) error
}

// Client is a client for the storage of the index (e.g. DynamoDB or Bigtable).
type Client interface {
	ReadClient
	WriteClient
	Stop()
}

// ReadBatchResult represents the results of a QueryPages.
type ReadBatchResult interface {
	Iterator() ReadBatchIterator
}

// ReadBatchIterator is an iterator over a ReadBatch.
type ReadBatchIterator interface {
	Next() bool
	RangeValue() []byte
	Value() []byte
}

// WriteBatch represents a batch of writes.
type WriteBatch interface {
	Add(tableName, hashValue string, rangeValue []byte, value []byte)
	Delete(tableName, hashValue string, rangeValue []byte)
}

// Query describes a query for entries
type Query struct {
	TableName string
	HashValue string

	// One of RangeValuePrefix or RangeValueStart might be set:
	// - If RangeValuePrefix is not nil, must read all keys with that prefix.
	// - If RangeValueStart is not nil, must read all keys from there onwards.
	// - If neither is set, must read all keys for that row.
	// RangeValueStart should only be used for querying Chunk IDs.
	// If this is going to change then please take care of func isChunksQuery in pkg/chunk/storage/caching_index_client.go which relies on it.
	RangeValuePrefix []byte
	RangeValueStart  []byte

	// Filters for querying
	ValueEqual []byte

	// If the result of this lookup is immutable or not (for caching).
	Immutable bool
}

// Entry describes an entry in the chunk index
type Entry struct {
	TableName string
	HashValue string

	// For writes, RangeValue will always be set.
	RangeValue []byte

	// New for v6 schema, label value is not written as part of the range key.
	Value []byte
}
