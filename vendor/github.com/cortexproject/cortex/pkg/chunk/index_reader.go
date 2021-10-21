package chunk

import (
	"context"
)

// IndexEntryProcessor receives index entries from a table.
type IndexEntryProcessor interface {
	ProcessIndexEntry(indexEntry IndexEntry) error

	// Will this user be accepted by the processor?
	AcceptUser(user string) bool

	// Called at the end of reading of index entries.
	Flush() error
}

// IndexReader parses index entries and passes them to the IndexEntryProcessor.
type IndexReader interface {
	IndexTableNames(ctx context.Context) ([]string, error)

	// Reads a single table from index, and passes individual index entries to the processors.
	//
	// All entries with the same TableName, HashValue and RangeValue are passed to the same processor,
	// and all such entries (with different Values) are passed before index entries with different
	// values of HashValue and RangeValue are passed to the same processor.
	//
	// This allows IndexEntryProcessor to find when values for given Hash and Range finish:
	// as soon as new Hash and Range differ from last IndexEntry.
	//
	// Index entries passed to the same processor arrive sorted by HashValue and RangeValue.
	ReadIndexEntries(ctx context.Context, table string, processors []IndexEntryProcessor) error
}
