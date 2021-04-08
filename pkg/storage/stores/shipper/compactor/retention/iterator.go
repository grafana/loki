package retention

import (
	"go.etcd.io/bbolt"
)

var _ ChunkEntryIterator = &boltdbChunkIndexIterator{}

type ChunkEntryIterator interface {
	Next() bool
	Entry() *ChunkRef
	Delete() error
	Err() error
}

type boltdbChunkIndexIterator struct {
	cursor  *bbolt.Cursor
	current *ChunkRef
	first   bool
	err     error
}

func newBoltdbChunkIndexIterator(bucket *bbolt.Bucket) *boltdbChunkIndexIterator {
	return &boltdbChunkIndexIterator{
		cursor: bucket.Cursor(),
		first:  true,
	}
}

func (b boltdbChunkIndexIterator) Err() error {
	return b.err
}

func (b boltdbChunkIndexIterator) Entry() *ChunkRef {
	return b.current
}

func (b boltdbChunkIndexIterator) Delete() error {
	return b.cursor.Delete()
}

func (b *boltdbChunkIndexIterator) Next() bool {
	var key []byte
	if b.first {
		key, _ = b.cursor.First()
		b.first = false
	} else {
		key, _ = b.cursor.Next()
	}
	for key != nil {
		ref, ok, err := parseChunkRef(decodeKey(key))
		if err != nil {
			b.err = err
			return false
		}
		// skips anything else than chunk index entries.
		if !ok {
			key, _ = b.cursor.Next()
			continue
		}
		b.current = ref
		return true
	}
	return false
}
