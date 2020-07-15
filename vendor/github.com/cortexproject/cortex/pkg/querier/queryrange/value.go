package queryrange

import (
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/prometheus/prometheus/storage"

	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/cortexproject/cortex/pkg/querier/series"
)

// FromResult transforms a promql query result into a samplestream
func FromResult(res *promql.Result) ([]SampleStream, error) {
	if res.Err != nil {
		return nil, res.Err
	}
	switch v := res.Value.(type) {
	case promql.Scalar:
		return []SampleStream{
			{
				Samples: []client.Sample{
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
				Labels:  mapLabels(sample.Metric),
				Samples: mapPoints(sample.Point),
			})
		}
		return res, nil

	case promql.Matrix:
		res := make([]SampleStream, 0, len(v))
		for _, series := range v {
			res = append(res, SampleStream{
				Labels:  mapLabels(series.Metric),
				Samples: mapPoints(series.Points...),
			})
		}
		return res, nil

	}

	return nil, errors.Errorf("Unexpected value type: [%s]", res.Value.Type())
}

func mapLabels(ls labels.Labels) []client.LabelAdapter {
	result := make([]client.LabelAdapter, 0, len(ls))
	for _, l := range ls {
		result = append(result, client.LabelAdapter(l))
	}

	return result
}

func mapPoints(pts ...promql.Point) []client.Sample {
	result := make([]client.Sample, 0, len(pts))

	for _, pt := range pts {
		result = append(result, client.Sample{
			Value:       pt.V,
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
	case parser.ValueTypeVector, parser.ValueTypeMatrix:
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
