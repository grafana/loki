package wal

import (
	"bytes"
	"context"
	"io"
	"sync"

	"github.com/prometheus/prometheus/model/labels"
	"golang.org/x/sync/errgroup"
	grpc "google.golang.org/grpc"

	"github.com/grafana/dskit/tenant"
	"github.com/grafana/loki/v3/pkg/ingester-rf1/metastore/metastorepb"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/storage/wal"
	"github.com/grafana/loki/v3/pkg/storage/wal/chunks"
	"github.com/grafana/loki/v3/pkg/storage/wal/index"
)

var _ logql.Querier = (*Querier)(nil)

type BlockStorage interface {
	GetRangeObject(ctx context.Context, objectKey string, off, length int64) (io.ReadCloser, error)
}

type Metastore interface {
	ListBlocksForQuery(ctx context.Context, in *metastorepb.ListBlocksForQueryRequest, opts ...grpc.CallOption) (*metastorepb.ListBlocksForQueryResponse, error)
}

type Querier struct {
	blockStorage BlockStorage
	metaStore    Metastore
}

func New(
	metaStore Metastore,
	blockStorage BlockStorage,
) (*Querier, error) {
	return &Querier{
		blockStorage: blockStorage,
		metaStore:    metaStore,
	}, nil
}

func (q *Querier) SelectLogs(ctx context.Context, req logql.SelectLogParams) (iter.EntryIterator, error) {
	// todo request validation and delete markers.
	tenantID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}
	expr, err := req.LogSelector()
	if err != nil {
		return nil, err
	}
	matchers := expr.Matchers()
	// todo: not sure if Pipeline is thread safe
	pipeline, err := expr.Pipeline()
	if err != nil {
		return nil, err
	}
	// todo support sharding
	var (
		lazyChunks []lazyChunk
		mtx        sync.Mutex
	)

	err = q.forSeries(ctx, &metastorepb.ListBlocksForQueryRequest{
		TenantId:  tenantID,
		StartTime: req.Start.UnixNano(),
		EndTime:   req.End.UnixNano(),
	}, func(id string, lbs *labels.ScratchBuilder, chk *chunks.Meta) error {
		mtx.Lock()
		lazyChunks = append(lazyChunks, newLazyChunk(id, lbs, chk))
		mtx.Unlock()
		return nil
	}, matchers...)

	return NewChunksEntryIterator(ctx,
		q.blockStorage,
		lazyChunks,
		pipeline,
		req.Direction,
		req.Start.UnixNano(),
		req.End.UnixNano()), err
}

func (q *Querier) SelectSamples(context.Context, logql.SelectSampleParams) (iter.SampleIterator, error) {
	// todo: implement
	return nil, nil
}

func (q *Querier) forSeries(ctx context.Context, req *metastorepb.ListBlocksForQueryRequest, fn func(string, *labels.ScratchBuilder, *chunks.Meta) error, ms ...*labels.Matcher) error {
	return q.forIndices(ctx, req, func(ir *index.Reader, id string) error {
		bufLbls := labels.ScratchBuilder{}
		chunks := make([]chunks.Meta, 0, 1)
		p, err := ir.PostingsForMatchers(ctx, req.TenantId, ms...)
		if err != nil {
			return err
		}
		for p.Next() {
			err := ir.Series(p.At(), &bufLbls, &chunks)
			if err != nil {
				return err
			}
			if err := fn(id, &bufLbls, &chunks[0]); err != nil {
				return err
			}
		}
		return p.Err()
	})
}

func (q *Querier) forIndices(ctx context.Context, req *metastorepb.ListBlocksForQueryRequest, fn func(ir *index.Reader, id string) error) error {
	resp, err := q.metaStore.ListBlocksForQuery(ctx, req)
	if err != nil {
		return err
	}
	metas := resp.Blocks
	if len(metas) == 0 {
		return nil
	}
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(32)
	for _, meta := range metas {

		meta := meta
		g.Go(func() error {
			reader, err := q.blockStorage.GetRangeObject(ctx, wal.Dir+meta.Id, meta.IndexRef.Offset, meta.IndexRef.Length)
			if err != nil {
				return err
			}
			defer reader.Close()
			// todo: use a buffer pool
			buf := bytes.NewBuffer(make([]byte, 0, meta.IndexRef.Length))
			_, err = buf.ReadFrom(reader)
			if err != nil {
				return err
			}
			index, err := index.NewReader(index.RealByteSlice(buf.Bytes()))
			if err != nil {
				return err
			}
			return fn(index, meta.Id)
		})
	}
	return g.Wait()
}
