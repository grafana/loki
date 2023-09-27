package main

import (
	"github.com/grafana/loki/pkg/storage/stores/indexshipper/tsdb/index"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
)

type pool struct {
	n  int // number of workers
	ch chan struct{}
}

func newPool(n int) *pool {
	p := &pool{
		n:  n,
		ch: make(chan struct{}, n),
	}

	// seed channel
	for i := 0; i < n; i++ {
		p.ch <- struct{}{}
	}

	return p
}

func (p *pool) acquire(
	ls labels.Labels, fp model.Fingerprint, chks []index.ChunkMeta,
	fn func(ls labels.Labels, fp model.Fingerprint, chks []index.ChunkMeta),
) {
	<-p.ch
	go func() {
		fn(ls, fp, chks)
		p.ch <- struct{}{}
	}()
}

func (p *pool) drain() {
	for i := 0; i < p.n; i++ {
		<-p.ch
	}
}
