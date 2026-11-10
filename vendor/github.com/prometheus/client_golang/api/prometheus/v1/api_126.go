// Copyright The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build !go1.27

// Package v1 provides bindings to the Prometheus HTTP API v1:
// http://prometheus.io/docs/querying/api/
package v1

import (
	gojson "encoding/json"
	"errors"
	"fmt"
	"strconv"
	"unsafe"

	jsoniter "github.com/json-iterator/go"

	"github.com/prometheus/common/model"
)

func init() {
	jsoniter.RegisterTypeDecoderFunc("model.SamplePair", unmarshalSamplePairJSON)
	jsoniter.RegisterTypeDecoderFunc("model.SampleHistogramPair", unmarshalSampleHistogramPairJSON)
	jsoniter.RegisterTypeDecoderFunc("model.SampleStream", unmarshalSampleStreamJSON) // Only needed for benchmark.
}

func unmarshalSamplePairJSON(ptr unsafe.Pointer, iter *jsoniter.Iterator) {
	p := (*model.SamplePair)(ptr)
	if !iter.ReadArray() {
		iter.ReportError("unmarshal model.SamplePair", "SamplePair must be [timestamp, value]")
		return
	}
	t := iter.ReadNumber()
	if err := p.Timestamp.UnmarshalJSON([]byte(t)); err != nil {
		iter.ReportError("unmarshal model.SamplePair", err.Error())
		return
	}
	if !iter.ReadArray() {
		iter.ReportError("unmarshal model.SamplePair", "SamplePair missing value")
		return
	}

	f, err := strconv.ParseFloat(iter.ReadString(), 64)
	if err != nil {
		iter.ReportError("unmarshal model.SamplePair", err.Error())
		return
	}
	p.Value = model.SampleValue(f)

	if iter.ReadArray() {
		iter.ReportError("unmarshal model.SamplePair", "SamplePair has too many values, must be [timestamp, value]")
		return
	}
}

func unmarshalSampleHistogramPairJSON(ptr unsafe.Pointer, iter *jsoniter.Iterator) {
	p := (*model.SampleHistogramPair)(ptr)
	if !iter.ReadArray() {
		iter.ReportError("unmarshal model.SampleHistogramPair", "SampleHistogramPair must be [timestamp, {histogram}]")
		return
	}
	t := iter.ReadNumber()
	if err := p.Timestamp.UnmarshalJSON([]byte(t)); err != nil {
		iter.ReportError("unmarshal model.SampleHistogramPair", err.Error())
		return
	}
	if !iter.ReadArray() {
		iter.ReportError("unmarshal model.SampleHistogramPair", "SamplePair missing histogram")
		return
	}
	h := &model.SampleHistogram{}
	p.Histogram = h
	for key := iter.ReadObject(); key != ""; key = iter.ReadObject() {
		switch key {
		case "count":
			f, err := strconv.ParseFloat(iter.ReadString(), 64)
			if err != nil {
				iter.ReportError("unmarshal model.SampleHistogramPair", "count of histogram is not a float")
				return
			}
			h.Count = model.FloatString(f)
		case "sum":
			f, err := strconv.ParseFloat(iter.ReadString(), 64)
			if err != nil {
				iter.ReportError("unmarshal model.SampleHistogramPair", "sum of histogram is not a float")
				return
			}
			h.Sum = model.FloatString(f)
		case "buckets":
			for iter.ReadArray() {
				b, err := unmarshalHistogramBucket(iter)
				if err != nil {
					iter.ReportError("unmarshal model.HistogramBucket", err.Error())
					return
				}
				h.Buckets = append(h.Buckets, b)
			}
		default:
			iter.ReportError("unmarshal model.SampleHistogramPair", fmt.Sprint("unexpected key in histogram:", key))
			return
		}
	}
	if iter.ReadArray() {
		iter.ReportError("unmarshal model.SampleHistogramPair", "SampleHistogramPair has too many values, must be [timestamp, {histogram}]")
		return
	}
}

func unmarshalSampleStreamJSON(ptr unsafe.Pointer, iter *jsoniter.Iterator) {
	ss := (*model.SampleStream)(ptr)
	for key := iter.ReadObject(); key != ""; key = iter.ReadObject() {
		switch key {
		case "metric":
			metricString := iter.ReadAny().ToString()
			if err := jsoniter.UnmarshalFromString(metricString, &ss.Metric); err != nil {
				iter.ReportError("unmarshal model.SampleStream", err.Error())
				return
			}
		case "values":
			for iter.ReadArray() {
				v := model.SamplePair{}
				unmarshalSamplePairJSON(unsafe.Pointer(&v), iter)
				ss.Values = append(ss.Values, v)
			}
		case "histograms":
			for iter.ReadArray() {
				h := model.SampleHistogramPair{}
				unmarshalSampleHistogramPairJSON(unsafe.Pointer(&h), iter)
				ss.Histograms = append(ss.Histograms, h)
			}
		default:
			iter.ReportError("unmarshal model.SampleStream", fmt.Sprint("unexpected key:", key))
			return
		}
	}
}

func unmarshalHistogramBucket(iter *jsoniter.Iterator) (*model.HistogramBucket, error) {
	b := model.HistogramBucket{}
	if !iter.ReadArray() {
		return nil, errors.New("HistogramBucket must be [boundaries, lower, upper, count]")
	}
	boundaries, err := iter.ReadNumber().Int64()
	if err != nil {
		return nil, err
	}
	b.Boundaries = int32(boundaries)
	if !iter.ReadArray() {
		return nil, errors.New("HistogramBucket must be [boundaries, lower, upper, count]")
	}
	f, err := strconv.ParseFloat(iter.ReadString(), 64)
	if err != nil {
		return nil, err
	}
	b.Lower = model.FloatString(f)
	if !iter.ReadArray() {
		return nil, errors.New("HistogramBucket must be [boundaries, lower, upper, count]")
	}
	f, err = strconv.ParseFloat(iter.ReadString(), 64)
	if err != nil {
		return nil, err
	}
	b.Upper = model.FloatString(f)
	if !iter.ReadArray() {
		return nil, errors.New("HistogramBucket must be [boundaries, lower, upper, count]")
	}
	f, err = strconv.ParseFloat(iter.ReadString(), 64)
	if err != nil {
		return nil, err
	}
	b.Count = model.FloatString(f)
	if iter.ReadArray() {
		return nil, errors.New("HistogramBucket has too many values, must be [boundaries, lower, upper, count]")
	}
	return &b, nil
}

func (qr *queryResult) UnmarshalJSON(b []byte) error {
	v := struct {
		Type   model.ValueType   `json:"resultType"`
		Result gojson.RawMessage `json:"result"`
	}{}

	err := gojson.Unmarshal(b, &v)
	if err != nil {
		return err
	}

	switch v.Type {
	case model.ValScalar:
		var sv model.Scalar
		err = jsoniter.Unmarshal(v.Result, &sv)
		qr.v = &sv

	case model.ValVector:
		var vv model.Vector
		err = jsoniter.Unmarshal(v.Result, &vv)
		qr.v = vv

	case model.ValMatrix:
		var mv model.Matrix
		err = jsoniter.Unmarshal(v.Result, &mv)
		qr.v = mv

	default:
		err = fmt.Errorf("unexpected value type %q", v.Type)
	}
	return err
}
