// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package store

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"math"
	"sort"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/gogo/protobuf/jsonpb"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/tsdb/fileutil"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/thanos-io/thanos/pkg/component"
	"github.com/thanos-io/thanos/pkg/runutil"
	"github.com/thanos-io/thanos/pkg/store/labelpb"
	"github.com/thanos-io/thanos/pkg/store/storepb"
)

// LocalStore implements the store API against single file with stream of proto-based SeriesResponses in JSON format.
// Inefficient implementation for quick StoreAPI view.
// Chunk order is exactly the same as in a given file.
type LocalStore struct {
	logger    log.Logger
	extLabels labels.Labels

	info *storepb.InfoResponse
	c    io.Closer

	// TODO(bwplotka): This is very naive in-memory DB. We can support much larger files, by
	// indexing labels, symbolizing strings and get chunk refs only without storing protobufs in memory.
	// For small debug purposes, this is good enough.
	series       []*storepb.Series
	sortedChunks [][]int
}

// TODO(bwplotka): Add remote read so Prometheus users can use this. Potentially after streaming will be added
// https://github.com/prometheus/prometheus/issues/5926.
// TODO(bwplotka): Consider non mmaped version of this, as well different versions.
func NewLocalStoreFromJSONMmappableFile(
	logger log.Logger,
	component component.StoreAPI,
	extLabels labels.Labels,
	path string,
	split bufio.SplitFunc,
) (*LocalStore, error) {
	f, err := fileutil.OpenMmapFile(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			runutil.CloseWithErrCapture(&err, f, "json file %s close", path)
		}
	}()

	s := &LocalStore{
		logger:    logger,
		extLabels: extLabels,
		c:         f,
		info: &storepb.InfoResponse{
			LabelSets: []labelpb.ZLabelSet{
				{Labels: labelpb.ZLabelsFromPromLabels(extLabels)},
			},
			StoreType: component.ToProto(),
			MinTime:   math.MaxInt64,
			MaxTime:   math.MinInt64,
		},
	}

	// Do quick pass for in-mem index.
	content := f.Bytes()
	contentStart := bytes.Index(content, []byte("{"))
	if contentStart != -1 {
		content = content[contentStart:]
	}

	if idx := bytes.LastIndex(content, []byte("}")); idx != -1 {
		content = content[:idx+1]
	}

	skanner := NewNoCopyScanner(content, split)
	resp := &storepb.SeriesResponse{}
	for skanner.Scan() {
		if err := jsonpb.Unmarshal(bytes.NewReader(skanner.Bytes()), resp); err != nil {
			return nil, errors.Wrapf(err, "unmarshal storepb.SeriesResponse frame for file %s", path)
		}
		series := resp.GetSeries()
		if series == nil {
			level.Warn(logger).Log("msg", "not a valid series", "frame", resp.String())
			continue
		}
		chks := make([]int, 0, len(series.Chunks))
		// Sort chunks in separate slice by MinTime for easier lookup. Find global max and min.
		for ci, c := range series.Chunks {
			if s.info.MinTime > c.MinTime {
				s.info.MinTime = c.MinTime
			}
			if s.info.MaxTime < c.MaxTime {
				s.info.MaxTime = c.MaxTime
			}
			chks = append(chks, ci)
		}

		sort.Slice(chks, func(i, j int) bool {
			return series.Chunks[chks[i]].MinTime < series.Chunks[chks[j]].MinTime
		})
		s.series = append(s.series, series)
		s.sortedChunks = append(s.sortedChunks, chks)
	}

	if err := skanner.Err(); err != nil {
		return nil, errors.Wrapf(err, "scanning file %s", path)
	}
	level.Info(logger).Log("msg", "loading JSON file succeeded", "file", path, "info", s.info.String(), "series", len(s.series))
	return s, nil
}

// ScanGRPCCurlProtoStreamMessages allows to tokenize each streamed gRPC message from grpcurl tool.
func ScanGRPCCurlProtoStreamMessages(data []byte, atEOF bool) (advance int, token []byte, err error) {
	var delim = []byte(`}
{`)
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if idx := bytes.LastIndex(data, delim); idx != -1 {
		return idx + 2, data[:idx+1], nil
	}
	// If we're at EOF, let's return all.
	if atEOF {
		return len(data), data, nil
	}
	// Incomplete; get more bytes.
	return len(delim), nil, nil
}

// Info returns store information about the Prometheus instance.
func (s *LocalStore) Info(_ context.Context, _ *storepb.InfoRequest) (*storepb.InfoResponse, error) {
	return s.info, nil
}

// Series returns all series for a requested time range and label matcher. The returned data may
// exceed the requested time bounds.
func (s *LocalStore) Series(r *storepb.SeriesRequest, srv storepb.Store_SeriesServer) error {
	match, matchers, err := matchesExternalLabels(r.Matchers, s.extLabels)
	if err != nil {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	if !match {
		return nil
	}
	if len(matchers) == 0 {
		return status.Error(codes.InvalidArgument, errors.New("no matchers specified (excluding external labels)").Error())
	}

	var chosen []int
	for si, series := range s.series {
		lbls := labelpb.ZLabelsToPromLabels(series.Labels)
		var noMatch bool
		for _, m := range matchers {
			extValue := lbls.Get(m.Name)
			if extValue == "" {
				continue
			}
			if !m.Matches(extValue) {
				noMatch = true
				break
			}
		}
		if noMatch {
			continue
		}

		chosen = chosen[:0]
		resp := &storepb.Series{
			Labels: series.Labels,
			Chunks: make([]storepb.AggrChunk, 0, len(s.sortedChunks[si])),
		}

		for _, ci := range s.sortedChunks[si] {
			if series.Chunks[ci].MaxTime < r.MinTime {
				continue
			}
			if series.Chunks[ci].MinTime > r.MaxTime {
				continue
			}
			chosen = append(chosen, ci)
		}

		sort.Ints(chosen)
		for _, ci := range chosen {
			resp.Chunks = append(resp.Chunks, series.Chunks[ci])
		}

		if err := srv.Send(storepb.NewSeriesResponse(resp)); err != nil {
			return status.Error(codes.Aborted, err.Error())
		}
	}
	return nil
}

// LabelNames returns all known label names.
func (s *LocalStore) LabelNames(_ context.Context, _ *storepb.LabelNamesRequest) (
	*storepb.LabelNamesResponse, error,
) {
	// TODO(bwplotka): Consider precomputing.
	names := map[string]struct{}{}
	for _, series := range s.series {
		for _, l := range series.Labels {
			names[l.Name] = struct{}{}
		}
	}
	resp := &storepb.LabelNamesResponse{}
	for n := range names {
		resp.Names = append(resp.Names, n)
	}
	return resp, nil
}

// LabelValues returns all known label values for a given label name.
func (s *LocalStore) LabelValues(_ context.Context, r *storepb.LabelValuesRequest) (
	*storepb.LabelValuesResponse, error,
) {
	vals := map[string]struct{}{}
	for _, series := range s.series {
		lbls := labelpb.ZLabelsToPromLabels(series.Labels)
		val := lbls.Get(r.Label)
		if val == "" {
			continue
		}
		vals[val] = struct{}{}
	}
	resp := &storepb.LabelValuesResponse{}
	for val := range vals {
		resp.Values = append(resp.Values, val)
	}
	return resp, nil
}

func (s *LocalStore) Close() (err error) {
	return s.c.Close()
}

type noCopyScanner struct {
	b         []byte
	splitFunc bufio.SplitFunc

	start, end int
	err        error

	token []byte
}

// NewNoCopyScanner returns bufio.Scanner-like scanner that is meant to be used on already allocated byte slice (or mmapped)
// one. Returned tokens are shared.
func NewNoCopyScanner(b []byte, splitFunc bufio.SplitFunc) *noCopyScanner {
	return &noCopyScanner{
		b:         b,
		splitFunc: splitFunc,
		start:     0,
		end:       0,
	}
}

func (s *noCopyScanner) Scan() bool {
	if s.start >= len(s.b) {
		return false
	}

	advance := 1
	for s.end+advance < len(s.b) {
		s.end += advance

		advance, s.token, s.err = s.splitFunc(s.b[s.start:s.end], false)
		if s.err != nil {
			return false
		}

		if len(s.token) > 0 {
			s.start += advance
			s.end = s.start
			return true
		}
	}

	_, s.token, s.err = s.splitFunc(s.b[s.start:], true)
	if s.err != nil {
		return false
	}
	s.start = len(s.b)
	return len(s.token) > 0
}

func (s *noCopyScanner) Bytes() []byte {
	return s.token
}

func (s *noCopyScanner) Err() error {
	return s.err
}
