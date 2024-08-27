package wal

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/go-kit/kit/log/level"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/opentracing/opentracing-go"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"

	"github.com/grafana/dskit/tenant"

	"github.com/grafana/loki/v3/pkg/ingester-rf1/metastore/metastorepb"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/stats"
	"github.com/grafana/loki/v3/pkg/storage/wal"
	"github.com/grafana/loki/v3/pkg/storage/wal/chunks"
	"github.com/grafana/loki/v3/pkg/storage/wal/index"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/spanlogger"
	util_validation "github.com/grafana/loki/v3/pkg/util/validation"
)

// TODO: Replace with the querier.Querier interface once we have support for all the methods.

var nowFunc = func() time.Time { return time.Now() }

type PartialQuerier interface {
	logql.Querier
	Label(ctx context.Context, req *logproto.LabelRequest) (*logproto.LabelResponse, error)
}

var _ querier.Querier = (*Querier)(nil)

type BlockStorage interface {
	GetObjectRange(ctx context.Context, objectKey string, off, length int64) (io.ReadCloser, error)
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

	chks, err := q.matchingChunks(ctx, tenantID, req.Start.UnixNano(), req.End.UnixNano(), matchers...)
	if err != nil {
		return nil, err
	}

	return NewChunksEntryIterator(ctx,
		q.blockStorage,
		chks,
		pipeline,
		req.Direction,
		req.Start.UnixNano(),
		req.End.UnixNano()), nil
}

func (q *Querier) SelectSamples(ctx context.Context, req logql.SelectSampleParams) (iter.SampleIterator, error) {
	// todo request validation and delete markers.
	tenantID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}
	expr, err := req.Expr()
	if err != nil {
		return nil, err
	}
	selector, err := expr.Selector()
	if err != nil {
		return nil, err
	}
	matchers := selector.Matchers()
	// todo: not sure if Extractor is thread safe

	extractor, err := expr.Extractor()
	if err != nil {
		return nil, err
	}

	chks, err := q.matchingChunks(ctx, tenantID, req.Start.UnixNano(), req.End.UnixNano(), matchers...)
	if err != nil {
		return nil, err
	}

	return NewChunksSampleIterator(ctx,
		q.blockStorage,
		chks,
		extractor,
		req.Start.UnixNano(),
		req.End.UnixNano()), nil
}

func (q *Querier) Label(ctx context.Context, req *logproto.LabelRequest) (*logproto.LabelResponse, error) {
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

	var matchers []*labels.Matcher
	if req.Query != "" {
		matchers, err = syntax.ParseMatchers(req.Query, true)
		if err != nil {
			return nil, err
		}
	}

	lbls, err := q.matchingLabels(ctx, userID, req.Start.UnixNano(), req.End.UnixNano(), matchers...)
	return &logproto.LabelResponse{Values: lbls}, err
}

func (q *Querier) Series(ctx context.Context, req *logproto.SeriesRequest) (*logproto.SeriesResponse, error) {
	// todo request validation and delete markers.
	tenantID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

	var matchers []*labels.Matcher
	from, through := util.RoundToMilliseconds(req.Start, req.End)

	matchers = []*labels.Matcher{}
	series, err := q.matchingSeries(ctx, tenantID, from.UnixNano(), through.UnixNano(), matchers...)
	if err != nil {
		return nil, err
	}

	result := make([]logproto.SeriesIdentifier, 0, len(series))
	for _, lbls := range series {
		result = append(result, logproto.SeriesIdentifierFromLabels(lbls))
	}
	return &logproto.SeriesResponse{
		Series: result,
	}, nil
}

func (q *Querier) Tail(ctx context.Context, req *logproto.TailRequest, categorizedLabels bool) (*querier.Tailer, error) {
	//TODO implement me
	panic("implement me")
}

func (q *Querier) IndexStats(ctx context.Context, req *loghttp.RangeQuery) (*stats.Stats, error) {
	//TODO implement me
	panic("implement me")
}

func (q *Querier) IndexShards(ctx context.Context, req *loghttp.RangeQuery, targetBytesPerShard uint64) (*logproto.ShardsResponse, error) {
	//TODO implement me
	panic("implement me")
}

func (q *Querier) Volume(ctx context.Context, req *logproto.VolumeRequest) (*logproto.VolumeResponse, error) {
	//TODO implement me
	panic("implement me")
}

func (q *Querier) DetectedFields(ctx context.Context, req *logproto.DetectedFieldsRequest) (*logproto.DetectedFieldsResponse, error) {
	//TODO implement me
	panic("implement me")
}

func (q *Querier) Patterns(ctx context.Context, req *logproto.QueryPatternsRequest) (*logproto.QueryPatternsResponse, error) {
	//TODO implement me
	panic("implement me")
}

func (q *Querier) DetectedLabels(ctx context.Context, req *logproto.DetectedLabelsRequest) (*logproto.DetectedLabelsResponse, error) {
	//TODO implement me
	panic("implement me")
}

func (q *Querier) WithPatternQuerier(patternQuerier querier.PatterQuerier) {
	//TODO implement me
	panic("implement me")
}

func (q *Querier) matchingChunks(ctx context.Context, tenantID string, from, through int64, matchers ...*labels.Matcher) ([]ChunkData, error) {
	sp, ctx := opentracing.StartSpanFromContext(ctx, "matchingChunks")
	defer sp.Finish()
	// todo support sharding
	var (
		lazyChunks []ChunkData
		mtx        sync.Mutex
	)

	err := q.forSeries(ctx, &metastorepb.ListBlocksForQueryRequest{
		TenantId:  tenantID,
		StartTime: from,
		EndTime:   through,
	}, func(id string, lbs *labels.ScratchBuilder, chk *chunks.Meta) error {
		mtx.Lock()
		lazyChunks = append(lazyChunks, newChunkData(id, lbs, chk))
		mtx.Unlock()
		return nil
	}, matchers...)
	if err != nil {
		return nil, err
	}
	if sp != nil {
		sp.LogKV("matchedChunks", len(lazyChunks))
	}
	return lazyChunks, nil
}

func (q *Querier) matchingSeries(ctx context.Context, tenantID string, from, through int64, matchers ...*labels.Matcher) ([]labels.Labels, error) {
	sp, ctx := opentracing.StartSpanFromContext(ctx, "matchingSeries")
	defer sp.Finish()
	// todo support sharding
	var (
		series = make([]labels.Labels, 0, 1024)
		found  = make(map[uint64]struct{})
		mtx    sync.Mutex
	)

	err := q.forSeries(ctx, &metastorepb.ListBlocksForQueryRequest{
		TenantId:  tenantID,
		StartTime: from,
		EndTime:   through,
	}, func(id string, lbs *labels.ScratchBuilder, chk *chunks.Meta) error {
		mtx.Lock()
		defer mtx.Unlock()
		lbls := lbs.Labels()
		lblHash, _ := lbls.HashWithoutLabels([]byte(index.TenantLabel))
		if _, ok := found[lblHash]; ok {
			return nil
		}
		found[lblHash] = struct{}{}
		// Remove tenant labels
		lbs.Sort()
		newLbs := lbs.Labels()
		j := 0
		for _, l := range newLbs {
			if l.Name != index.TenantLabel {
				newLbs[j] = l
				j++
			}
		}
		newLbs = newLbs[:j]
		series = append(series, newLbs)
		return nil
	}, matchers...)
	if err != nil {
		return nil, err
	}
	if sp != nil {
		sp.LogKV("matchedSeries", len(series))
	}
	sort.Slice(series, func(i, j int) bool {
		return labels.Compare(series[i], series[j]) < 0
	})
	return series, nil
}

func (q *Querier) matchingLabels(ctx context.Context, tenantID string, from, through int64, matchers ...*labels.Matcher) ([]string, error) {
	sp, ctx := opentracing.StartSpanFromContext(ctx, "matchingChunks")
	defer sp.Finish()
	// todo support sharding
	var (
		uniqLabels = make([]string, 0, 32)
		found      = make(map[string]struct{})
		mtx        sync.Mutex
	)

	err := q.forSeries(ctx, &metastorepb.ListBlocksForQueryRequest{
		TenantId:  tenantID,
		StartTime: from,
		EndTime:   through,
	}, func(id string, lbs *labels.ScratchBuilder, chk *chunks.Meta) error {
		mtx.Lock()
		defer mtx.Unlock()
		for _, lbl := range lbs.Labels() {
			if lbl.Name == index.TenantLabel {
				continue
			}
			if _, ok := found[lbl.Name]; ok {
				continue
			}
			found[lbl.Name] = struct{}{}
			uniqLabels = append(uniqLabels, lbl.Name)
		}
		return nil
	}, matchers...)
	if err != nil {
		return nil, err
	}
	if sp != nil {
		sp.LogKV("matchedLabels", len(uniqLabels))
	}
	sort.Strings(uniqLabels)
	return uniqLabels, nil
}

func (q *Querier) forSeries(ctx context.Context, req *metastorepb.ListBlocksForQueryRequest, fn func(string, *labels.ScratchBuilder, *chunks.Meta) error, matchers ...*labels.Matcher) error {
	// copy matchers to avoid modifying the original slice.
	ms := make([]*labels.Matcher, 0, len(matchers)+1)
	ms = append(ms, matchers...)
	ms = append(ms, labels.MustNewMatcher(labels.MatchEqual, index.TenantLabel, req.TenantId))

	return q.forIndices(ctx, req, func(ir *index.Reader, id string) error {
		bufLbls := labels.ScratchBuilder{}
		chunks := make([]chunks.Meta, 0, 1)
		p, err := ir.PostingsForMatchers(ctx, ms...)
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
			reader, err := q.blockStorage.GetObjectRange(ctx, wal.Dir+meta.Id, meta.IndexRef.Offset, meta.IndexRef.Length)
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

func validateQueryTimeRangeLimits(ctx context.Context, userID string, limits querier.TimeRangeLimits, from, through time.Time) (time.Time, time.Time, error) {
	now := nowFunc()
	// Clamp the time range based on the max query lookback.
	maxQueryLookback := limits.MaxQueryLookback(ctx, userID)
	if maxQueryLookback > 0 && from.Before(now.Add(-maxQueryLookback)) {
		origStartTime := from
		from = now.Add(-maxQueryLookback)

		level.Debug(spanlogger.FromContext(ctx)).Log(
			"msg", "the start time of the query has been manipulated because of the 'max query lookback' setting",
			"original", origStartTime,
			"updated", from)

	}
	maxQueryLength := limits.MaxQueryLength(ctx, userID)
	if maxQueryLength > 0 && (through).Sub(from) > maxQueryLength {
		return time.Time{}, time.Time{}, httpgrpc.Errorf(http.StatusBadRequest, util_validation.ErrQueryTooLong, (through).Sub(from), model.Duration(maxQueryLength))
	}
	if through.Before(from) {
		return time.Time{}, time.Time{}, httpgrpc.Errorf(http.StatusBadRequest, util_validation.ErrQueryTooOld, model.Duration(maxQueryLookback))
	}
	return from, through, nil
}
