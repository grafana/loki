package ingester

import "sync"

/*
need to rebuild
- user states
  - inverted index
  - map[fp]stream


plan without checkpoint:
1) Read wal records, mapping FPs & creating series into map[userId]map[uint64]stream
2) ensure fpmapper & inverted index are created/used.
3) iterate wal samples? Do this after b/c chunks may be cut while adding wal samples.

After recovering from a checkpoint, all flushed chunks that are now passed the retention config may be dropped

Error conditions to test:
- stream limited by limiter should not apply during wal replay. Can be hacked by setting an unlimited limiter then overriding after wal replay.
-
*/

var (
	// shared pool for WALRecords
	recordPool = newRecordPool()
)

// WAL interface allows us to have a no-op WAL when the WAL is disabled.
type WAL interface {
	// Log marshalls the records and writes it into the WAL.
	Log(*WALRecord) error
	// Stop stops all the WAL operations.
	Stop()
}

type noopWAL struct{}

func (noopWAL) Log(*WALRecord) error { return nil }
func (noopWAL) Stop()                {}

type resettingPool struct {
	pool *sync.Pool
}

func (p *resettingPool) GetRecord() *WALRecord {
	rec := p.pool.Get().(*WALRecord)
	rec.Reset()
	return rec
}

func (p *resettingPool) PutRecord(r *WALRecord) {
	p.pool.Put(r)
}

func newRecordPool() *resettingPool {
	return &resettingPool{
		&sync.Pool{
			New: func() interface{} {
				return &WALRecord{}
			},
		},
	}
}
