package queryrangebase

import (
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/prometheus/prometheus/storage"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/querier/series"
)

// FromResult transforms a promql query result into a samplestream
func FromResult(res *promql.Result) ([]SampleStream, error) {
	if res.Err != nil {
		// The error could be wrapped by the PromQL engine. We get the error's cause in order to
		// correctly parse the error in parent callers (eg. gRPC response status code extraction).
		return nil, errors.Cause(res.Err)
	}

	return FromValue((res.Value))
}

func FromValue(value parser.Value) ([]SampleStream, error) {
	switch v := value.(type) {
	case promql.Scalar:
		return []SampleStream{
			{
				Samples: []logproto.LegacySample{
					{
						Value:       v.V,
						TimestampMs: v.T,
					},
				},
			},
		}, nil

	case promql.Vector:
		res := make([]SampleStream, 0, len(v))
		for _, sample := range v {
			res = append(res, SampleStream{
				Labels: mapLabels(sample.Metric),
				Samples: []logproto.LegacySample{
					{
						Value:       sample.F,
						TimestampMs: sample.T,
					},
				},
			})
		}
		return res, nil

	case promql.Matrix:
		res := make([]SampleStream, 0, len(v))
		for _, series := range v {
			res = append(res, SampleStream{
				Labels:  mapLabels(series.Metric),
				Samples: mapPoints(series.Floats...),
			})
		}
		return res, nil

	}

	return nil, errors.Errorf("Unexpected value type: [%s]", value.Type())
}

func mapLabels(ls labels.Labels) []logproto.LabelAdapter {
	result := make([]logproto.LabelAdapter, 0, len(ls))
	for _, l := range ls {
		result = append(result, logproto.LabelAdapter(l))
	}

	return result
}

func mapPoints(pts ...promql.FPoint) []logproto.LegacySample {
	result := make([]logproto.LegacySample, 0, len(pts))

	for _, pt := range pts {
		result = append(result, logproto.LegacySample{
			Value:       pt.F,
			TimestampMs: pt.T,
		})
	}

	return result
}

// ResponseToSamples is needed to map back from api response to the underlying series data
func ResponseToSamples(resp Response) ([]SampleStream, error) {
	promRes, ok := resp.(*PrometheusResponse)
	if !ok {
		return nil, errors.Errorf("error invalid response type: %T, expected: %T", resp, &PrometheusResponse{})
	}
	if promRes.Error != "" {
		return nil, errors.New(promRes.Error)
	}
	switch promRes.Data.ResultType {
	case string(parser.ValueTypeVector), string(parser.ValueTypeMatrix):
		return promRes.Data.Result, nil
	}

	return nil, errors.Errorf(
		"Invalid promql.Value type: [%s]. Only %s and %s supported",
		promRes.Data.ResultType,
		parser.ValueTypeVector,
		parser.ValueTypeMatrix,
	)
}

// NewSeriesSet returns an in memory storage.SeriesSet from a []SampleStream
// As NewSeriesSet uses NewConcreteSeriesSet to implement SeriesSet, result will be sorted by label names.
func NewSeriesSet(results []SampleStream) storage.SeriesSet {
	set := make([]storage.Series, 0, len(results))

	for _, stream := range results {
		samples := make([]model.SamplePair, 0, len(stream.Samples))
		for _, sample := range stream.Samples {
			samples = append(samples, model.SamplePair{
				Timestamp: model.Time(sample.TimestampMs),
				Value:     model.SampleValue(sample.Value),
			})
		}

		ls := make([]labels.Label, 0, len(stream.Labels))
		for _, l := range stream.Labels {
			ls = append(ls, labels.Label(l))
		}
		set = append(set, series.NewConcreteSeries(ls, samples))
	}
	return series.NewConcreteSeriesSet(set)
}
