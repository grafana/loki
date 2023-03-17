package wal

import (
	"sync"

	"github.com/grafana/loki/pkg/logproto"
)

type ResettingPool struct {
	rPool *sync.Pool // records
	ePool *sync.Pool // entries
	bPool *sync.Pool // bytes
}

func NewRecordPool() *ResettingPool {
	return &ResettingPool{
		rPool: &sync.Pool{
			New: func() interface{} {
				return &Record{}
			},
		},
		ePool: &sync.Pool{
			New: func() interface{} {
				return make([]logproto.Entry, 0, 512)
			},
		},
		bPool: &sync.Pool{
			New: func() interface{} {
				buf := make([]byte, 0, 1<<10) // 1kb
				return &buf
			},
		},
	}
}

func (p *ResettingPool) GetRecord() *Record {
	rec := p.rPool.Get().(*Record)
	rec.Reset()
	for _, ref := range rec.RefEntries {
		p.PutEntries(ref.Entries)
	}
	return rec
}

func (p *ResettingPool) PutRecord(r *Record) {
	p.rPool.Put(r)
}

func (p *ResettingPool) GetEntries() []logproto.Entry {
	return p.ePool.Get().([]logproto.Entry)
}

func (p *ResettingPool) PutEntries(es []logproto.Entry) {
	p.ePool.Put(es[:0]) // nolint:staticcheck
}

func (p *ResettingPool) GetBytes() *[]byte {
	return p.bPool.Get().(*[]byte)
}

func (p *ResettingPool) PutBytes(b *[]byte) {
	*b = (*b)[:0]
	p.bPool.Put(b)
}
