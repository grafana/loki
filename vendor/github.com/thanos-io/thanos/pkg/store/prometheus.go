// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package store

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"sync"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/storage/remote"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/thanos-io/thanos/pkg/component"
	thanoshttp "github.com/thanos-io/thanos/pkg/http"
	"github.com/thanos-io/thanos/pkg/promclient"
	"github.com/thanos-io/thanos/pkg/runutil"
	"github.com/thanos-io/thanos/pkg/store/storepb"
	"github.com/thanos-io/thanos/pkg/store/storepb/prompb"
	"github.com/thanos-io/thanos/pkg/tracing"
)

// PrometheusStore implements the store node API on top of the Prometheus remote read API.
type PrometheusStore struct {
	logger         log.Logger
	base           *url.URL
	client         *promclient.Client
	buffers        sync.Pool
	component      component.StoreAPI
	externalLabels func() labels.Labels
	timestamps     func() (mint int64, maxt int64)

	remoteReadAcceptableResponses []prompb.ReadRequest_ResponseType
}

const initialBufSize = 32 * 1024 // 32KB seems like a good minimum starting size for sync pool size.

// NewPrometheusStore returns a new PrometheusStore that uses the given HTTP client
// to talk to Prometheus.
// It attaches the provided external labels to all results.
func NewPrometheusStore(
	logger log.Logger,
	client *promclient.Client,
	baseURL *url.URL,
	component component.StoreAPI,
	externalLabels func() labels.Labels,
	timestamps func() (mint int64, maxt int64),
) (*PrometheusStore, error) {
	if logger == nil {
		logger = log.NewNopLogger()
	}
	p := &PrometheusStore{
		logger:                        logger,
		base:                          baseURL,
		client:                        client,
		component:                     component,
		externalLabels:                externalLabels,
		timestamps:                    timestamps,
		remoteReadAcceptableResponses: []prompb.ReadRequest_ResponseType{prompb.ReadRequest_STREAMED_XOR_CHUNKS, prompb.ReadRequest_SAMPLES},
		buffers: sync.Pool{New: func() interface{} {
			b := make([]byte, 0, initialBufSize)
			return &b
		}},
	}
	return p, nil
}

// Info returns store information about the Prometheus instance.
// NOTE(bwplotka): MaxTime & MinTime are not accurate nor adjusted dynamically.
// This is fine for now, but might be needed in future.
func (p *PrometheusStore) Info(_ context.Context, _ *storepb.InfoRequest) (*storepb.InfoResponse, error) {
	lset := p.externalLabels()
	mint, maxt := p.timestamps()

	res := &storepb.InfoResponse{
		Labels:    make([]storepb.Label, 0, len(lset)),
		StoreType: p.component.ToProto(),
		MinTime:   mint,
		MaxTime:   maxt,
	}
	for _, l := range lset {
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

func (p *PrometheusStore) getBuffer() *[]byte {
	b := p.buffers.Get()
	return b.(*[]byte)
}

func (p *PrometheusStore) putBuffer(b *[]byte) {
	p.buffers.Put(b)
}

// Series returns all series for a requested time range and label matcher.
func (p *PrometheusStore) Series(r *storepb.SeriesRequest, s storepb.Store_SeriesServer) error {
	externalLabels := p.externalLabels()

	match, newMatchers, err := matchesExternalLabels(r.Matchers, externalLabels)
	if err != nil {
		return status.Error(codes.InvalidArgument, err.Error())
	}

	if !match {
		return nil
	}

	if len(newMatchers) == 0 {
		return status.Error(codes.InvalidArgument, "no matchers specified (excluding external labels)")
	}

	// Don't ask for more than available time. This includes potential `minTime` flag limit.
	availableMinTime, _ := p.timestamps()
	if r.MinTime < availableMinTime {
		r.MinTime = availableMinTime
	}

	if r.SkipChunks {
		labelMaps, err := p.client.SeriesInGRPC(s.Context(), p.base, newMatchers, r.MinTime, r.MaxTime)
		if err != nil {
			return err
		}
		for _, lbm := range labelMaps {
			lset := make([]storepb.Label, 0, len(lbm)+len(externalLabels))
			for k, v := range lbm {
				lset = append(lset, storepb.Label{Name: k, Value: v})
			}
			lset = append(lset, storepb.PromLabelsToLabelsUnsafe(externalLabels)...)
			sort.Slice(lset, func(i, j int) bool {
				return lset[i].Name < lset[j].Name
			})
			if err = s.Send(storepb.NewSeriesResponse(&storepb.Series{Labels: lset})); err != nil {
				return err
			}
		}
		return nil
	}

	q := &prompb.Query{StartTimestampMs: r.MinTime, EndTimestampMs: r.MaxTime}

	for _, m := range newMatchers {
		pm := &prompb.LabelMatcher{Name: m.Name, Value: m.Value}

		switch m.Type {
		case storepb.LabelMatcher_EQ:
			pm.Type = prompb.LabelMatcher_EQ
		case storepb.LabelMatcher_NEQ:
			pm.Type = prompb.LabelMatcher_NEQ
		case storepb.LabelMatcher_RE:
			pm.Type = prompb.LabelMatcher_RE
		case storepb.LabelMatcher_NRE:
			pm.Type = prompb.LabelMatcher_NRE
		default:
			return errors.New("unrecognized matcher type")
		}
		q.Matchers = append(q.Matchers, pm)
	}

	queryPrometheusSpan, ctx := tracing.StartSpan(s.Context(), "query_prometheus")

	httpResp, err := p.startPromRemoteRead(ctx, q)
	if err != nil {
		queryPrometheusSpan.Finish()
		return errors.Wrap(err, "query Prometheus")
	}

	// Negotiate content. We requested streamed chunked response type, but still we need to support old versions of
	// remote read.
	contentType := httpResp.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "application/x-protobuf") {
		return p.handleSampledPrometheusResponse(s, httpResp, queryPrometheusSpan, externalLabels)
	}

	if !strings.HasPrefix(contentType, "application/x-streamed-protobuf; proto=prometheus.ChunkedReadResponse") {
		return errors.Errorf("not supported remote read content type: %s", contentType)
	}
	return p.handleStreamedPrometheusResponse(s, httpResp, queryPrometheusSpan, externalLabels)
}

func (p *PrometheusStore) handleSampledPrometheusResponse(s storepb.Store_SeriesServer, httpResp *http.Response, querySpan opentracing.Span, externalLabels labels.Labels) error {
	ctx := s.Context()

	level.Debug(p.logger).Log("msg", "started handling ReadRequest_SAMPLED response type.")

	resp, err := p.fetchSampledResponse(ctx, httpResp)
	querySpan.Finish()
	if err != nil {
		return err
	}

	span, _ := tracing.StartSpan(ctx, "transform_and_respond")
	defer span.Finish()
	span.SetTag("series_count", len(resp.Results[0].Timeseries))

	for _, e := range resp.Results[0].Timeseries {
		lset := p.translateAndExtendLabels(e.Labels, externalLabels)

		if len(e.Samples) == 0 {
			// As found in https://github.com/thanos-io/thanos/issues/381
			// Prometheus can give us completely empty time series. Ignore these with log until we figure out that
			// this is expected from Prometheus perspective.
			level.Warn(p.logger).Log(
				"msg",
				"found timeseries without any chunk. See https://github.com/thanos-io/thanos/issues/381 for details",
				"lset",
				fmt.Sprintf("%v", lset),
			)
			continue
		}

		aggregatedChunks, err := p.chunkSamples(e, MaxSamplesPerChunk)
		if err != nil {
			return err
		}

		if err := s.Send(storepb.NewSeriesResponse(&storepb.Series{
			Labels: lset,
			Chunks: aggregatedChunks,
		})); err != nil {
			return err
		}
	}
	level.Debug(p.logger).Log("msg", "handled ReadRequest_SAMPLED request.", "series", len(resp.Results[0].Timeseries))
	return nil
}

func (p *PrometheusStore) handleStreamedPrometheusResponse(s storepb.Store_SeriesServer, httpResp *http.Response, querySpan opentracing.Span, externalLabels labels.Labels) error {
	level.Debug(p.logger).Log("msg", "started handling ReadRequest_STREAMED_XOR_CHUNKS streamed read response.")

	framesNum := 0
	seriesNum := 0

	defer func() {
		querySpan.SetTag("frames", framesNum)
		querySpan.SetTag("series", seriesNum)
		querySpan.Finish()
	}()
	defer runutil.CloseWithLogOnErr(p.logger, httpResp.Body, "prom series request body")

	var (
		lastSeries string
		currSeries string
		tmp        []string
		data       = p.getBuffer()
	)
	defer p.putBuffer(data)

	// TODO(bwplotka): Put read limit as a flag.
	stream := remote.NewChunkedReader(httpResp.Body, remote.DefaultChunkedReadLimit, *data)
	for {
		res := &prompb.ChunkedReadResponse{}
		err := stream.NextProto(res)
		if err == io.EOF {
			break
		}
		if err != nil {
			return errors.Wrap(err, "next proto")
		}

		if len(res.ChunkedSeries) != 1 {
			level.Warn(p.logger).Log("msg", "Prometheus ReadRequest_STREAMED_XOR_CHUNKS returned non 1 series in frame", "series", len(res.ChunkedSeries))
		}

		framesNum++
		for _, series := range res.ChunkedSeries {
			{
				// Calculate hash of series for counting.
				tmp = tmp[:0]
				for _, l := range series.Labels {
					tmp = append(tmp, l.String())
				}
				currSeries = strings.Join(tmp, ";")
				if currSeries != lastSeries {
					seriesNum++
					lastSeries = currSeries
				}
			}

			thanosChks := make([]storepb.AggrChunk, len(series.Chunks))
			for i, chk := range series.Chunks {
				thanosChks[i] = storepb.AggrChunk{
					MaxTime: chk.MaxTimeMs,
					MinTime: chk.MinTimeMs,
					Raw: &storepb.Chunk{
						Data: chk.Data,
						// Prometheus ChunkEncoding vs ours https://github.com/thanos-io/thanos/blob/master/pkg/store/storepb/types.proto#L19
						// has one difference. Prometheus has Chunk_UNKNOWN Chunk_Encoding = 0 vs we start from
						// XOR as 0. Compensate for that here:
						Type: storepb.Chunk_Encoding(chk.Type - 1),
					},
				}
				// Drop the reference to data from non protobuf for GC.
				series.Chunks[i].Data = nil
			}

			if err := s.Send(storepb.NewSeriesResponse(&storepb.Series{
				Labels: p.translateAndExtendLabels(series.Labels, externalLabels),
				Chunks: thanosChks,
			})); err != nil {
				return err
			}
		}
	}
	level.Debug(p.logger).Log("msg", "handled ReadRequest_STREAMED_XOR_CHUNKS request.", "frames", framesNum, "series", seriesNum)
	return nil
}

func (p *PrometheusStore) fetchSampledResponse(ctx context.Context, resp *http.Response) (_ *prompb.ReadResponse, err error) {
	defer runutil.ExhaustCloseWithLogOnErr(p.logger, resp.Body, "prom series request body")

	b := p.getBuffer()
	buf := bytes.NewBuffer(*b)
	defer p.putBuffer(b)
	if _, err := io.Copy(buf, resp.Body); err != nil {
		return nil, errors.Wrap(err, "copy response")
	}

	sb := p.getBuffer()
	var decomp []byte
	tracing.DoInSpan(ctx, "decompress_response", func(ctx context.Context) {
		decomp, err = snappy.Decode(*sb, buf.Bytes())
	})
	defer p.putBuffer(sb)
	if err != nil {
		return nil, errors.Wrap(err, "decompress response")
	}

	var data prompb.ReadResponse
	tracing.DoInSpan(ctx, "unmarshal_response", func(ctx context.Context) {
		err = proto.Unmarshal(decomp, &data)
	})
	if err != nil {
		return nil, errors.Wrap(err, "unmarshal response")
	}
	if len(data.Results) != 1 {
		return nil, errors.Errorf("unexpected result size %d", len(data.Results))
	}

	return &data, nil
}

func (p *PrometheusStore) chunkSamples(series *prompb.TimeSeries, maxSamplesPerChunk int) (chks []storepb.AggrChunk, err error) {
	samples := series.Samples

	for len(samples) > 0 {
		chunkSize := len(samples)
		if chunkSize > maxSamplesPerChunk {
			chunkSize = maxSamplesPerChunk
		}

		enc, cb, err := p.encodeChunk(samples[:chunkSize])
		if err != nil {
			return nil, status.Error(codes.Unknown, err.Error())
		}

		chks = append(chks, storepb.AggrChunk{
			MinTime: int64(samples[0].Timestamp),
			MaxTime: int64(samples[chunkSize-1].Timestamp),
			Raw:     &storepb.Chunk{Type: enc, Data: cb},
		})

		samples = samples[chunkSize:]
	}

	return chks, nil
}

func (p *PrometheusStore) startPromRemoteRead(ctx context.Context, q *prompb.Query) (presp *http.Response, _ error) {
	reqb, err := proto.Marshal(&prompb.ReadRequest{
		Queries:               []*prompb.Query{q},
		AcceptedResponseTypes: p.remoteReadAcceptableResponses,
	})
	if err != nil {
		return nil, errors.Wrap(err, "marshal read request")
	}

	qjson, err := json.Marshal(q)
	if err != nil {
		return nil, errors.Wrap(err, "json encode query for tracing")
	}

	u := *p.base
	u.Path = path.Join(u.Path, "api/v1/read")

	preq, err := http.NewRequest("POST", u.String(), bytes.NewReader(snappy.Encode(nil, reqb)))
	if err != nil {
		return nil, errors.Wrap(err, "unable to create request")
	}
	preq.Header.Add("Content-Encoding", "snappy")
	preq.Header.Set("Content-Type", "application/x-stream-protobuf")
	preq.Header.Set("X-Prometheus-Remote-Read-Version", "0.1.0")

	preq.Header.Set("User-Agent", thanoshttp.ThanosUserAgent)
	tracing.DoInSpan(ctx, "query_prometheus_request", func(ctx context.Context) {
		preq = preq.WithContext(ctx)
		presp, err = p.client.Do(preq)
	}, opentracing.Tag{Key: "prometheus.query", Value: string(qjson)})
	if err != nil {
		return nil, errors.Wrap(err, "send request")
	}
	if presp.StatusCode/100 != 2 {
		// Best effort read.
		b, err := ioutil.ReadAll(presp.Body)
		if err != nil {
			level.Error(p.logger).Log("msg", "failed to read response from non 2XX remote read request", "err", err)
		}
		_ = presp.Body.Close()
		return nil, errors.Errorf("request failed with code %s; msg %s", presp.Status, string(b))
	}

	return presp, nil
}

// matchesExternalLabels filters out external labels matching from matcher if exists as the local storage does not have them.
// It also returns false if given matchers are not matching external labels.
func matchesExternalLabels(ms []storepb.LabelMatcher, externalLabels labels.Labels) (bool, []storepb.LabelMatcher, error) {
	if len(externalLabels) == 0 {
		return true, ms, nil
	}

	var newMatcher []storepb.LabelMatcher
	for _, m := range ms {
		// Validate all matchers.
		tm, err := promclient.TranslateMatcher(m)
		if err != nil {
			return false, nil, err
		}

		extValue := externalLabels.Get(m.Name)
		if extValue == "" {
			// Agnostic to external labels.
			newMatcher = append(newMatcher, m)
			continue
		}

		if !tm.Matches(extValue) {
			// External label does not match. This should not happen - it should be filtered out on query node,
			// but let's do that anyway here.
			return false, nil, nil
		}
	}

	return true, newMatcher, nil
}

// encodeChunk translates the sample pairs into a chunk.
// TODO(kakkoyun): Linter - result 0 (github.com/thanos-io/thanos/pkg/store/storepb.Chunk_Encoding) is always 0.
func (p *PrometheusStore) encodeChunk(ss []prompb.Sample) (storepb.Chunk_Encoding, []byte, error) { //nolint:unparam
	c := chunkenc.NewXORChunk()

	a, err := c.Appender()
	if err != nil {
		return 0, nil, err
	}
	for _, s := range ss {
		a.Append(s.Timestamp, s.Value)
	}
	return storepb.Chunk_XOR, c.Bytes(), nil
}

// translateAndExtendLabels transforms a metrics into a protobuf label set. It additionally
// attaches the given labels to it, overwriting existing ones on collision.
// Both input labels are expected to be sorted.
//
// NOTE(bwplotka): Don't use modify passed slices as we reuse underlying memory.
func (p *PrometheusStore) translateAndExtendLabels(m []prompb.Label, extend labels.Labels) []storepb.Label {
	pbLabels := storepb.PrompbLabelsToLabelsUnsafe(m)
	pbExtend := storepb.PromLabelsToLabelsUnsafe(extend)

	lset := make([]storepb.Label, 0, len(m)+len(extend))
	ei := 0

Outer:
	for _, l := range pbLabels {
		for ei < len(pbExtend) {
			if l.Name < pbExtend[ei].Name {
				break
			}
			lset = append(lset, pbExtend[ei])
			ei++
			if l.Name == pbExtend[ei-1].Name {
				continue Outer
			}
		}
		lset = append(lset, l)
	}
	return storepb.ExtendLabels(lset, extend)
}

// LabelNames returns all known label names.
func (p *PrometheusStore) LabelNames(ctx context.Context, r *storepb.LabelNamesRequest) (*storepb.LabelNamesResponse, error) {
	lbls, err := p.client.LabelNamesInGRPC(ctx, p.base, r.Start, r.End)
	if err != nil {
		return nil, err
	}
	return &storepb.LabelNamesResponse{Names: lbls}, nil
}

// LabelValues returns all known label values for a given label name.
func (p *PrometheusStore) LabelValues(ctx context.Context, r *storepb.LabelValuesRequest) (*storepb.LabelValuesResponse, error) {
	externalLset := p.externalLabels()

	// First check for matching external label which has priority.
	if l := externalLset.Get(r.Label); l != "" {
		return &storepb.LabelValuesResponse{Values: []string{l}}, nil
	}

	vals, err := p.client.LabelValuesInGRPC(ctx, p.base, r.Label, r.Start, r.End)
	if err != nil {
		return nil, err
	}
	sort.Strings(vals)
	return &storepb.LabelValuesResponse{Values: vals}, nil
}
