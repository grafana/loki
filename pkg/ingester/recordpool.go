package ingester

import (
	"sync"

	"github.com/grafana/loki/pkg/logproto"
)

type resettingPool struct {
	rPool *sync.Pool // records
	ePool *sync.Pool // entries
	bPool *sync.Pool // bytes
}

func (p *resettingPool) GetRecord() *WALRecord {
	rec := p.rPool.Get().(*WALRecord)
	rec.Reset()
	return rec
}

func (p *resettingPool) PutRecord(r *WALRecord) {
	p.rPool.Put(r)
}

func (p *resettingPool) GetEntries() []logproto.Entry {
	return p.ePool.Get().([]logproto.Entry)
}

func (p *resettingPool) PutEntries(es []logproto.Entry) {
	p.ePool.Put(es[:0]) // nolint:staticcheck
}

func (p *resettingPool) GetBytes() []byte {
	return p.bPool.Get().([]byte)
}

func (p *resettingPool) PutBytes(b []byte) {
	p.bPool.Put(b[:0]) // nolint:staticcheck
}

func NewRecordPool() *resettingPool {
	return &resettingPool{
		rPool: &sync.Pool{
			New: func() interface{} {
				return &WALRecord{}
			},
		},
		ePool: &sync.Pool{
			New: func() interface{} {
				return make([]logproto.Entry, 0, 512)
			},
		},
		bPool: &sync.Pool{
			New: func() interface{} {
				return make([]byte, 0, 1<<10) // 1kb
			},
		},
	}
}
