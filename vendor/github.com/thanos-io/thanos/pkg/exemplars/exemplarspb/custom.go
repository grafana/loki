// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package exemplarspb

import (
	"encoding/json"
	"math/big"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/thanos-io/thanos/pkg/store/labelpb"
)

// UnmarshalJSON implements json.Unmarshaler.
func (m *Exemplar) UnmarshalJSON(b []byte) error {
	v := struct {
		Labels    labelpb.ZLabelSet
		TimeStamp model.Time
		Value     model.SampleValue
	}{}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}

	m.Labels = v.Labels
	m.Ts = int64(v.TimeStamp)
	m.Value = float64(v.Value)

	return nil
}

// MarshalJSON implements json.Marshaler.
func (m *Exemplar) MarshalJSON() ([]byte, error) {
	v := struct {
		Labels    labels.Labels     `json:"labels"`
		TimeStamp model.Time        `json:"timestamp"`
		Value     model.SampleValue `json:"value"`
	}{
		Labels:    labelpb.ZLabelsToPromLabels(m.Labels.Labels),
		TimeStamp: model.Time(m.Ts),
		Value:     model.SampleValue(m.Value),
	}
	return json.Marshal(v)
}

func NewExemplarsResponse(e *ExemplarData) *ExemplarsResponse {
	return &ExemplarsResponse{
		Result: &ExemplarsResponse_Data{
			Data: e,
		},
	}
}

func NewWarningExemplarsResponse(warning error) *ExemplarsResponse {
	return &ExemplarsResponse{
		Result: &ExemplarsResponse_Warning{
			Warning: warning.Error(),
		},
	}
}

func (s1 *ExemplarData) Compare(s2 *ExemplarData) int {
	return labels.Compare(s1.SeriesLabels.PromLabels(), s2.SeriesLabels.PromLabels())
}

func (s *ExemplarData) SetSeriesLabels(ls labels.Labels) {
	var result labelpb.ZLabelSet

	if len(ls) > 0 {
		result = labelpb.ZLabelSet{Labels: labelpb.ZLabelsFromPromLabels(ls)}
	}

	s.SeriesLabels = result
}

func (e *Exemplar) SetLabels(ls labels.Labels) {
	var result labelpb.ZLabelSet

	if len(ls) > 0 {
		result = labelpb.ZLabelSet{Labels: labelpb.ZLabelsFromPromLabels(ls)}
	}

	e.Labels = result
}

func (e1 *Exemplar) Compare(e2 *Exemplar) int {
	if d := labels.Compare(e1.Labels.PromLabels(), e2.Labels.PromLabels()); d != 0 {
		return d
	}
	if e1.Ts < e2.Ts {
		return 1
	}
	if e1.Ts > e2.Ts {
		return -1
	}

	return big.NewFloat(e1.Value).Cmp(big.NewFloat(e2.Value))
}
