package remote

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/common/config"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logcli/client"
	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logqlmodel"
	"github.com/grafana/loki/pkg/querier"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/pkg/storage/stores/index/stats"
)

// ReadConfig is the configuration for reading from remote storage.
// code from /github.com/prometheus/prometheus/config/config.go:868
type ReadConfig struct {
	URL   *config.URL `yaml:"url"`
	Name  string      `yaml:"name,omitempty"`
	OrgID string      `yaml:"orgID,omitempty"`

	HTTPClientConfig config.HTTPClientConfig `yaml:",inline"`

	//todo:@liguozhong support http herder
	Headers map[string]string `yaml:"headers,omitempty"`
	//todo:@liguozhong support RemoteTimeout
	RemoteTimeout time.Duration `yaml:"remote_timeout,omitempty"`
}

func NewQuerier(name string, remoteReadConfig ReadConfig) (DetailQuerier, error) {
	client := &client.DefaultClient{
		OrgID:   remoteReadConfig.OrgID,
		Address: remoteReadConfig.URL.String(),
		TLSConfig: config.TLSConfig{
			InsecureSkipVerify: remoteReadConfig.HTTPClientConfig.TLSConfig.InsecureSkipVerify,
		},
	}
	if remoteReadConfig.HTTPClientConfig.BasicAuth != nil {
		client.Username = remoteReadConfig.HTTPClientConfig.BasicAuth.Username
		client.Password = strings.TrimSpace(string(remoteReadConfig.HTTPClientConfig.BasicAuth.Password))
	}

	return &Querier{
		client: client,
		name:   name,
	}, nil
}

type Querier struct {
	client client.Client
	codec  queryrangebase.Codec
	name   string
}

func (q Querier) SelectLogDetails(ctx context.Context, params logql.SelectLogParams) (iter.EntryIterator, loghttp.Streams, error) {
	response, err := q.client.QueryRange(ctx, params.Selector, int(params.Limit), params.Start, params.End, params.Direction, 0, 0, false)
	if err != nil {
		return nil, nil, err
	}
	if response.Status != loghttp.QueryStatusSuccess {
		return nil, nil, errors.Errorf("remote read Querier selectLogs fail,response.Status %v", response.Status)
	}

	streams, ok := response.Data.Result.(loghttp.Streams)
	if !ok {
		return nil, nil, errors.New("remote read Querier selectLogs fail,value cast (loghttp.Streams) fail")
	}
	return iter.NewStreamsIterator(streams.ToProto(), params.Direction), streams, nil
}

func (q Querier) SelectLogs(ctx context.Context, params logql.SelectLogParams) (iter.EntryIterator, error) {
	iter, _, err := q.SelectLogDetails(ctx, params)
	return iter, err
}

func (q Querier) SelectSamples(ctx context.Context, params logql.SelectSampleParams) (iter.SampleIterator, error) {
	response, err := q.client.QueryRange(ctx, params.Selector, 1, params.Start, params.End, logproto.FORWARD, 0, 0, false)
	if err != nil {
		return nil, err
	}
	if response.Status != loghttp.QueryStatusSuccess {
		return nil, errors.Errorf("remote read Querier selectSamples fail,response.Status %v", response.Status)
	}

	value := response.Data.Result
	switch value.Type() {
	case logqlmodel.ValueTypeStreams:
		return nil, fmt.Errorf("Unable to parse unsupported type: %s ", value.Type())
	case loghttp.ResultTypeMatrix:
		return iter.NewSampleQueryResponseIterator(toSampleQueryResponse(value.(loghttp.Matrix))), nil
	case loghttp.ResultTypeVector:
		return iter.NewSampleQueryResponseIterator(vectorToSampleQueryResponse(value.(loghttp.Vector))), nil
	default:
		return nil, fmt.Errorf("Unable to parse unsupported type: %s ", value.Type())
	}
}

func toSampleQueryResponse(m loghttp.Matrix) *logproto.SampleQueryResponse {
	res := &logproto.SampleQueryResponse{
		Series: make([]logproto.Series, 0, len(m)),
	}

	if len(m) == 0 {
		return res
	}
	for _, stream := range m {
		samples := make([]logproto.Sample, 0, len(stream.Values))
		for _, s := range stream.Values {
			samples = append(samples, logproto.Sample{
				Value:     float64(s.Value),
				Timestamp: int64(s.Timestamp),
			})
		}
		series := logproto.Series{
			Samples: samples,
			Labels:  stream.Metric.String(),
		}
		res.Series = append(res.Series, series)
	}
	return res
}

func vectorToSampleQueryResponse(v loghttp.Vector) *logproto.SampleQueryResponse {
	res := &logproto.SampleQueryResponse{
		Series: make([]logproto.Series, 0, len(v)),
	}
	if len(v) == 0 {
		return res
	}
	for _, s := range v {

		samples := make([]logproto.Sample, 0)
		samples = append(samples, logproto.Sample{
			Value:     float64(s.Value),
			Timestamp: int64(s.Timestamp),
		})

		series := logproto.Series{
			Samples: samples,
			Labels:  s.Metric.String(),
		}
		res.Series = append(res.Series, series)
	}
	return res
}

func (q Querier) Label(ctx context.Context, req *logproto.LabelRequest) (*logproto.LabelResponse, error) {
	var values []string
	if req.Values {
		lvs, err := q.client.ListLabelValues(ctx, req.Name, false, *req.GetStart(), *req.GetEnd())
		if err != nil {
			return nil, err
		}
		values = lvs.Data
	} else {
		lvs, err := q.client.ListLabelNames(ctx, false, *req.GetStart(), *req.GetEnd())
		if err != nil {
			return nil, err
		}
		values = lvs.Data
	}
	return &logproto.LabelResponse{Values: values}, nil
}

func (q Querier) Series(ctx context.Context, req *logproto.SeriesRequest) (*logproto.SeriesResponse, error) {
	series, err := q.client.Series(ctx, req.GetGroups(), req.GetStart(), req.GetEnd(), false)
	if err != nil {
		return nil, err
	}
	identifiers := make([]logproto.SeriesIdentifier, 0)
	for _, s := range series.Data {
		identifiers = append(identifiers, logproto.SeriesIdentifier{Labels: s.Map()})
	}
	return &logproto.SeriesResponse{Series: identifiers}, nil
}

func (q Querier) Tail(_ context.Context, _ *logproto.TailRequest) (*querier.Tailer, error) {
	panic("unsupported func")
}

func (q Querier) IndexStats(_ context.Context, _ *loghttp.RangeQuery) (*stats.Stats, error) {
	//TODO implement me
	return &stats.Stats{}, nil
}

func (q Querier) Volume(_ context.Context, _ *logproto.VolumeRequest) (*logproto.VolumeResponse, error) {
	//	_, err := q.client.GetVolume(req.GetQuery(), time.UnixMilli(req.GetStart()), time.UnixMilli(req.GetEnd()), time.Duration(req.Step), int(req.Limit), false)
	//TODO implement me
	result := &logproto.VolumeResponse{}
	return result, nil
}
