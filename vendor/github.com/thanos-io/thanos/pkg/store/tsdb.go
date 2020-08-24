// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package store

import (
	"context"
	"math"
	"sort"

	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/thanos-io/thanos/pkg/component"
	"github.com/thanos-io/thanos/pkg/promclient"
	"github.com/thanos-io/thanos/pkg/runutil"
	"github.com/thanos-io/thanos/pkg/store/storepb"
)

type TSDBReader interface {
	storage.Queryable
	StartTime() (int64, error)
}

// TSDBStore implements the store API against a local TSDB instance.
// It attaches the provided external labels to all results. It only responds with raw data
// and does not support downsampling.
type TSDBStore struct {
	logger         log.Logger
	db             TSDBReader
	component      component.StoreAPI
	externalLabels labels.Labels
}

// ReadWriteTSDBStore is a TSDBStore that can also be written to.
type ReadWriteTSDBStore struct {
	storepb.StoreServer
	storepb.WriteableStoreServer
}

// NewTSDBStore creates a new TSDBStore.
func NewTSDBStore(logger log.Logger, _ prometheus.Registerer, db TSDBReader, component component.StoreAPI, externalLabels labels.Labels) *TSDBStore {
	if logger == nil {
		logger = log.NewNopLogger()
	}
	return &TSDBStore{
		logger:         logger,
		db:             db,
		component:      component,
		externalLabels: externalLabels,
	}
}

// Info returns store information about the Prometheus instance.
func (s *TSDBStore) Info(_ context.Context, _ *storepb.InfoRequest) (*storepb.InfoResponse, error) {
	minTime, err := s.db.StartTime()
	if err != nil {
		return nil, errors.Wrap(err, "TSDB min Time")
	}

	res := &storepb.InfoResponse{
		Labels:    make([]storepb.Label, 0, len(s.externalLabels)),
		StoreType: s.component.ToProto(),
		MinTime:   minTime,
		MaxTime:   math.MaxInt64,
	}
	for _, l := range s.externalLabels {
		res.Labels = append(res.Labels, storepb.Label{
			Name:  l.Name,
			Value: l.Value,
		})
	}

	// Until we deprecate the single labels in the reply, we just duplicate
	// them here for migration/compatibility purposes.
	res.LabelSets = []storepb.LabelSet{}
	if len(res.Labels) > 0 {
		res.LabelSets = append(res.LabelSets, storepb.LabelSet{
			Labels: res.Labels,
		})
	}
	return res, nil
}

// Series returns all series for a requested time range and label matcher. The returned data may
// exceed the requested time bounds.
func (s *TSDBStore) Series(r *storepb.SeriesRequest, srv storepb.Store_SeriesServer) error {
	match, newMatchers, err := matchesExternalLabels(r.Matchers, s.externalLabels)
	if err != nil {
		return status.Error(codes.InvalidArgument, err.Error())
	}

	if !match {
		return nil
	}

	if len(newMatchers) == 0 {
		return status.Error(codes.InvalidArgument, errors.New("no matchers specified (excluding external labels)").Error())
	}

	matchers, err := promclient.TranslateMatchers(newMatchers)
	if err != nil {
		return status.Error(codes.InvalidArgument, err.Error())
	}

	q, err := s.db.Querier(context.Background(), r.MinTime, r.MaxTime)
	if err != nil {
		return status.Error(codes.Internal, err.Error())
	}
	defer runutil.CloseWithLogOnErr(s.logger, q, "close tsdb querier series")

	var (
		set        = q.Select(false, nil, matchers...)
		respSeries storepb.Series
	)
	for set.Next() {
		series := set.At()

		respSeries.Labels = s.translateAndExtendLabels(series.Labels(), s.externalLabels)

		if !r.SkipChunks {
			// TODO(fabxc): An improvement over this trivial approach would be to directly
			// use the chunks provided by TSDB in the response.
			c, err := s.encodeChunks(series.Iterator(), MaxSamplesPerChunk)
			if err != nil {
				return status.Errorf(codes.Internal, "encode chunk: %s", err)
			}

			respSeries.Chunks = append(respSeries.Chunks[:0], c...)
		}

		if err := srv.Send(storepb.NewSeriesResponse(&respSeries)); err != nil {
			return status.Error(codes.Aborted, err.Error())
		}
	}
	if err := set.Err(); err != nil {
		return status.Error(codes.Internal, err.Error())
	}
	return nil
}

func (s *TSDBStore) encodeChunks(it chunkenc.Iterator, maxSamplesPerChunk int) (chks []storepb.AggrChunk, err error) {
	var (
		chkMint int64
		chk     *chunkenc.XORChunk
		app     chunkenc.Appender
		isNext  = it.Next()
	)

	for isNext {
		if chk == nil {
			chk = chunkenc.NewXORChunk()
			app, err = chk.Appender()
			if err != nil {
				return nil, err
			}
			chkMint, _ = it.At()
		}

		app.Append(it.At())
		chkMaxt, _ := it.At()

		isNext = it.Next()
		if isNext && chk.NumSamples() < maxSamplesPerChunk {
			continue
		}

		// Cut the chunk.
		chks = append(chks, storepb.AggrChunk{
			MinTime: chkMint,
			MaxTime: chkMaxt,
			Raw:     &storepb.Chunk{Type: storepb.Chunk_XOR, Data: chk.Bytes()},
		})
		chk = nil
	}
	if it.Err() != nil {
		return nil, errors.Wrap(it.Err(), "read TSDB series")
	}

	return chks, nil

}

// translateAndExtendLabels transforms a metrics into a protobuf label set. It additionally
// attaches the given labels to it, overwriting existing ones on collision.
func (s *TSDBStore) translateAndExtendLabels(m, extend labels.Labels) []storepb.Label {
	lset := make([]storepb.Label, 0, len(m)+len(extend))

	for _, l := range m {
		if extend.Get(l.Name) != "" {
			continue
		}
		lset = append(lset, storepb.Label{
			Name:  l.Name,
			Value: l.Value,
		})
	}
	for _, l := range extend {
		lset = append(lset, storepb.Label{
			Name:  l.Name,
			Value: l.Value,
		})
	}
	sort.Slice(lset, func(i, j int) bool {
		return lset[i].Name < lset[j].Name
	})
	return lset
}

// LabelNames returns all known label names.
func (s *TSDBStore) LabelNames(ctx context.Context, r *storepb.LabelNamesRequest) (
	*storepb.LabelNamesResponse, error,
) {
	q, err := s.db.Querier(ctx, r.Start, r.End)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	defer runutil.CloseWithLogOnErr(s.logger, q, "close tsdb querier label names")

	res, _, err := q.LabelNames()
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &storepb.LabelNamesResponse{Names: res}, nil
}

// LabelValues returns all known label values for a given label name.
func (s *TSDBStore) LabelValues(ctx context.Context, r *storepb.LabelValuesRequest) (
	*storepb.LabelValuesResponse, error,
) {
	q, err := s.db.Querier(ctx, r.Start, r.End)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	defer runutil.CloseWithLogOnErr(s.logger, q, "close tsdb querier label values")

	res, _, err := q.LabelValues(r.Label)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &storepb.LabelValuesResponse{Values: res}, nil
}
