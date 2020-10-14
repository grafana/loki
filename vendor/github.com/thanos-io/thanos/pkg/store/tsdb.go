// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package store

import (
	"context"
	"io"
	"math"
	"sort"

	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/storage"
	storetestutil "github.com/thanos-io/thanos/pkg/store/storepb/testutil"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/thanos-io/thanos/pkg/component"
	"github.com/thanos-io/thanos/pkg/promclient"
	"github.com/thanos-io/thanos/pkg/runutil"
	"github.com/thanos-io/thanos/pkg/store/storepb"
)

type TSDBReader interface {
	storage.ChunkQueryable
	StartTime() (int64, error)
}

// TSDBStore implements the store API against a local TSDB instance.
// It attaches the provided external labels to all results. It only responds with raw data
// and does not support downsampling.
type TSDBStore struct {
	logger           log.Logger
	db               TSDBReader
	component        component.StoreAPI
	externalLabels   labels.Labels
	maxBytesPerFrame int
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
		logger:           logger,
		db:               db,
		component:        component,
		externalLabels:   externalLabels,
		maxBytesPerFrame: storetestutil.RemoteReadFrameLimit,
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

// CloseDelegator allows to delegate close (releasing resources used by request to the server).
// This is useful when we invoke StoreAPI within another StoreAPI and results are ephemeral until copied.
type CloseDelegator interface {
	Delegate(io.Closer)
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

	q, err := s.db.ChunkQuerier(context.Background(), r.MinTime, r.MaxTime)
	if err != nil {
		return status.Error(codes.Internal, err.Error())
	}

	if cd, ok := srv.(CloseDelegator); ok {
		cd.Delegate(q)
	} else {
		defer runutil.CloseWithLogOnErr(s.logger, q, "close tsdb chunk querier series")
	}

	set := q.Select(false, nil, matchers...)

	// Stream at most one series per frame; series may be split over multiple frames according to maxBytesInFrame.
	for set.Next() {
		series := set.At()
		seriesLabels := storepb.Series{Labels: s.translateAndExtendLabels(series.Labels(), s.externalLabels)}
		if r.SkipChunks {
			if err := srv.Send(storepb.NewSeriesResponse(&seriesLabels)); err != nil {
				return status.Error(codes.Aborted, err.Error())
			}
			continue
		}

		bytesLeftForChunks := s.maxBytesPerFrame
		for _, lbl := range seriesLabels.Labels {
			bytesLeftForChunks -= lbl.Size()
		}
		frameBytesLeft := bytesLeftForChunks

		seriesChunks := []storepb.AggrChunk{}
		chIter := series.Iterator()
		isNext := chIter.Next()
		for isNext {
			chk := chIter.At()
			if chk.Chunk == nil {
				return status.Errorf(codes.Internal, "TSDBStore: found not populated chunk returned by SeriesSet at ref: %v", chk.Ref)
			}

			c := storepb.AggrChunk{
				MinTime: chk.MinTime,
				MaxTime: chk.MaxTime,
				Raw: &storepb.Chunk{
					Type: storepb.Chunk_Encoding(chk.Chunk.Encoding() - 1), // Proto chunk encoding is one off to TSDB one.
					Data: chk.Chunk.Bytes(),
				},
			}
			frameBytesLeft -= c.Size()
			seriesChunks = append(seriesChunks, c)

			// We are fine with minor inaccuracy of max bytes per frame. The inaccuracy will be max of full chunk size.
			isNext = chIter.Next()
			if frameBytesLeft > 0 && isNext {
				continue
			}
			if err := srv.Send(storepb.NewSeriesResponse(&storepb.Series{Labels: seriesLabels.Labels, Chunks: seriesChunks})); err != nil {
				return status.Error(codes.Aborted, err.Error())
			}

			if isNext {
				frameBytesLeft = bytesLeftForChunks
				seriesChunks = make([]storepb.AggrChunk, 0, len(seriesChunks))
			}
		}
		if err := chIter.Err(); err != nil {
			return status.Error(codes.Internal, errors.Wrap(err, "chunk iter").Error())
		}

	}
	if err := set.Err(); err != nil {
		return status.Error(codes.Internal, err.Error())
	}
	for _, w := range set.Warnings() {
		if err := srv.Send(storepb.NewWarnSeriesResponse(w)); err != nil {
			return status.Error(codes.Aborted, err.Error())
		}
	}
	return nil
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
	q, err := s.db.ChunkQuerier(ctx, r.Start, r.End)
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
	q, err := s.db.ChunkQuerier(ctx, r.Start, r.End)
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
