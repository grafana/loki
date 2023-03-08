package wal

import (
	"sync"
)

type ResettingPool struct {
	rPool *sync.Pool // records
	bPool *sync.Pool // bytes
}

func NewRecordPool() *ResettingPool {
	return &ResettingPool{
		rPool: &sync.Pool{
			New: func() interface{} {
				return &Record{}
			},
		},
		bPool: &sync.Pool{
			New: func() interface{} {
				buf := new([]byte)            // Attempt to force allocation on heap.
				*buf = make([]byte, 0, 1<<10) // 1kb
				return buf
			},
		},
	}
}

func (p *ResettingPool) GetRecord() *Record {
	rec := p.rPool.Get().(*Record)
	rec.Reset()
	return rec
}

func (p *ResettingPool) PutRecord(r *Record) {
	p.rPool.Put(r)
}

func (p *ResettingPool) GetBytes() *[]byte {
	return p.bPool.Get().(*[]byte)
}

func (p *ResettingPool) PutBytes(b *[]byte) {
	*b = (*b)[:0]
	p.bPool.Put(b)
}
