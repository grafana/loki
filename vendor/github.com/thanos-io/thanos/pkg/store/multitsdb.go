// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package store

import (
	"context"
	"fmt"
	"sync"

	"github.com/go-kit/kit/log"
	grpc_opentracing "github.com/grpc-ecosystem/go-grpc-middleware/tracing/opentracing"
	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/thanos-io/thanos/pkg/component"
	"github.com/thanos-io/thanos/pkg/store/storepb"
	"github.com/thanos-io/thanos/pkg/tracing"
)

// MultiTSDBStore implements the Store interface backed by multiple TSDBStore instances.
type MultiTSDBStore struct {
	logger     log.Logger
	component  component.SourceStoreAPI
	tsdbStores func() map[string]*TSDBStore
}

// NewMultiTSDBStore creates a new MultiTSDBStore.
func NewMultiTSDBStore(logger log.Logger, _ prometheus.Registerer, component component.SourceStoreAPI, tsdbStores func() map[string]*TSDBStore) *MultiTSDBStore {
	if logger == nil {
		logger = log.NewNopLogger()
	}
	return &MultiTSDBStore{
		logger:     logger,
		component:  component,
		tsdbStores: tsdbStores,
	}
}

// Info returns store merged information about the underlying TSDBStore instances.
func (s *MultiTSDBStore) Info(ctx context.Context, req *storepb.InfoRequest) (*storepb.InfoResponse, error) {
	stores := s.tsdbStores()

	resp := &storepb.InfoResponse{
		StoreType: s.component.ToProto(),
	}
	if len(stores) == 0 {
		return resp, nil
	}

	infos := make([]*storepb.InfoResponse, 0, len(stores))
	for tenant, store := range stores {
		info, err := store.Info(ctx, req)
		if err != nil {
			return nil, errors.Wrapf(err, "get info for tenant %s", tenant)
		}
		infos = append(infos, info)
	}

	resp.MinTime = infos[0].MinTime
	resp.MaxTime = infos[0].MaxTime

	for i := 1; i < len(infos); i++ {
		if resp.MinTime > infos[i].MinTime {
			resp.MinTime = infos[i].MinTime
		}
		if resp.MaxTime < infos[i].MaxTime {
			resp.MaxTime = infos[i].MaxTime
		}
	}

	// We can rely on every underlying TSDB to only have one labelset, so this
	// will always allocate the correct length immediately.
	resp.LabelSets = make([]storepb.LabelSet, 0, len(infos))
	for _, info := range infos {
		resp.LabelSets = append(resp.LabelSets, info.LabelSets...)
	}

	return resp, nil
}

type tenantSeriesSetServer struct {
	grpc.ServerStream

	ctx context.Context

	warnCh warnSender
	recv   chan *storepb.Series
	cur    *storepb.Series

	err    error
	tenant string
}

func newTenantSeriesSetServer(
	ctx context.Context,
	tenant string,
	warnCh warnSender,
) *tenantSeriesSetServer {
	return &tenantSeriesSetServer{
		ctx:    ctx,
		tenant: tenant,
		warnCh: warnCh,
		recv:   make(chan *storepb.Series),
	}
}

func (s *tenantSeriesSetServer) Context() context.Context {
	return s.ctx
}

func (s *tenantSeriesSetServer) Series(store *TSDBStore, r *storepb.SeriesRequest) {
	var err error
	tracing.DoInSpan(s.ctx, "multitsdb_tenant_series", func(_ context.Context) {
		err = store.Series(r, s)
	})

	if err != nil {
		if r.PartialResponseDisabled {
			s.err = errors.Wrapf(err, "get series for tenant %s", s.tenant)
		} else {
			// Consistently prefix tenant specific warnings as done in various other places.
			err = errors.New(prefixTenantWarning(s.tenant, err.Error()))
			s.warnCh.send(storepb.NewWarnSeriesResponse(err))
		}
	}

	close(s.recv)
}

func (s *tenantSeriesSetServer) Send(r *storepb.SeriesResponse) error {
	series := r.GetSeries()
	chunks := make([]storepb.AggrChunk, len(series.Chunks))
	copy(chunks, series.Chunks)
	s.recv <- &storepb.Series{
		Labels: series.Labels,
		Chunks: chunks,
	}
	return nil
}

func (s *tenantSeriesSetServer) Next() (ok bool) {
	s.cur, ok = <-s.recv
	return ok
}

func (s *tenantSeriesSetServer) At() ([]storepb.Label, []storepb.AggrChunk) {
	if s.cur == nil {
		return nil, nil
	}
	return s.cur.Labels, s.cur.Chunks
}

func (s *tenantSeriesSetServer) Err() error {
	return s.err
}

// Series returns all series for a requested time range and label matcher. The
// returned data may exceed the requested time bounds. The data returned may
// have been read and merged from multiple underlying TSDBStore instances.
func (s *MultiTSDBStore) Series(r *storepb.SeriesRequest, srv storepb.Store_SeriesServer) error {
	stores := s.tsdbStores()
	if len(stores) == 0 {
		return nil
	}

	var (
		g, gctx   = errgroup.WithContext(srv.Context())
		span, ctx = tracing.StartSpan(gctx, "multitsdb_series")
		// Allow to buffer max 10 series response.
		// Each might be quite large (multi chunk long series given by sidecar).
		respSender, respRecv, closeFn = newRespCh(gctx, 10)
	)
	defer span.Finish()

	g.Go(func() error {
		var (
			seriesSet []storepb.SeriesSet
			wg        = &sync.WaitGroup{}
		)

		defer func() {
			wg.Wait()
			closeFn()
		}()

		for tenant, store := range stores {
			store := store
			seriesCtx, cancelSeries := context.WithCancel(ctx)
			seriesCtx = grpc_opentracing.ClientAddContextTags(seriesCtx, opentracing.Tags{
				"tenant": tenant,
			})
			defer cancelSeries()
			ss := newTenantSeriesSetServer(seriesCtx, tenant, respSender)
			wg.Add(1)
			go func() {
				defer wg.Done()
				ss.Series(store, r)
			}()

			seriesSet = append(seriesSet, ss)
		}

		mergedSet := storepb.MergeSeriesSets(seriesSet...)
		for mergedSet.Next() {
			var series storepb.Series
			series.Labels, series.Chunks = mergedSet.At()
			respSender.send(storepb.NewSeriesResponse(&series))
		}
		return mergedSet.Err()
	})

	for resp := range respRecv {
		if err := srv.Send(resp); err != nil {
			return status.Error(codes.Unknown, errors.Wrap(err, "send series response").Error())
		}
	}

	return g.Wait()
}

// LabelNames returns all known label names.
func (s *MultiTSDBStore) LabelNames(ctx context.Context, req *storepb.LabelNamesRequest) (*storepb.LabelNamesResponse, error) {
	span, ctx := tracing.StartSpan(ctx, "multitsdb_label_names")
	defer span.Finish()

	names := map[string]struct{}{}
	warnings := map[string]struct{}{}

	stores := s.tsdbStores()
	for tenant, store := range stores {
		r, err := store.LabelNames(ctx, req)
		if err != nil {
			return nil, errors.Wrapf(err, "get label names for tenant %s", tenant)
		}

		for _, l := range r.Names {
			names[l] = struct{}{}
		}

		for _, l := range r.Warnings {
			warnings[prefixTenantWarning(tenant, l)] = struct{}{}
		}
	}

	return &storepb.LabelNamesResponse{
		Names:    keys(names),
		Warnings: keys(warnings),
	}, nil
}

func prefixTenantWarning(tenant, s string) string {
	return fmt.Sprintf("[%s] %s", tenant, s)
}

func keys(m map[string]struct{}) []string {
	res := make([]string, 0, len(m))
	for k := range m {
		res = append(res, k)
	}

	return res
}

// LabelValues returns all known label values for a given label name.
func (s *MultiTSDBStore) LabelValues(ctx context.Context, req *storepb.LabelValuesRequest) (*storepb.LabelValuesResponse, error) {
	span, ctx := tracing.StartSpan(ctx, "multitsdb_label_values")
	defer span.Finish()

	values := map[string]struct{}{}
	warnings := map[string]struct{}{}

	stores := s.tsdbStores()
	for tenant, store := range stores {
		r, err := store.LabelValues(ctx, req)
		if err != nil {
			return nil, errors.Wrapf(err, "get label values for tenant %s", tenant)
		}

		for _, l := range r.Values {
			values[l] = struct{}{}
		}

		for _, l := range r.Warnings {
			warnings[prefixTenantWarning(tenant, l)] = struct{}{}
		}
	}

	return &storepb.LabelValuesResponse{
		Values:   keys(values),
		Warnings: keys(warnings),
	}, nil
}
