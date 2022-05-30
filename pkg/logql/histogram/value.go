package histogram

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/buger/jsonparser"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql/parser"
)

type Vector []Sample
type Matrix []Series

func (h Vector) String() string {
	entries := make([]string, len(h))
	for i, s := range h {
		entries[i] = s.String()
	}
	return strings.Join(entries, "\n")

}

func (s Sample) String() string {
	return fmt.Sprintf("%s => %s", s.Metric, s.Point)
}

func (p Point) String() string {
	return fmt.Sprintf("%v @[%v]", p.V, p.T)
}

func (h Vector) Type() parser.ValueType { return "histogramvector" }

func (s Series) String() string {
	vals := make([]string, len(s.Points))
	for i, v := range s.Points {
		vals[i] = v.String()
	}
	return fmt.Sprintf("%s =>\n%s", s.Metric, strings.Join(vals, "\n"))
}

func (h Matrix) String() string {

	strs := make([]string, len(h))

	for i, ss := range h {
		strs[i] = ss.String()
	}

	return strings.Join(strs, "\n")

}

func (h Matrix) Type() parser.ValueType { return "histogrammatrix" }
func (m Matrix) Len() int               { return len(m) }
func (m Matrix) Less(i, j int) bool     { return labels.Compare(m[i].Metric, m[j].Metric) < 0 }
func (m Matrix) Swap(i, j int)          { m[i], m[j] = m[j], m[i] }

type Series struct {
	Metric labels.Labels `json:"metric"`
	Points []Point       `json:"histvalues"`
}

type Sample struct {
	Point
	Metric labels.Labels
}

type Point struct {
	T int64
	V []SampleValue
}

type SampleValue struct {
	UpperBound float64
	Count      int64
}

// MarshalJSON implements json.Marshaler.
func (v SampleValue) MarshalJSON() ([]byte, error) {
	vv := struct {
		UpperBound float64
		Count      int64
	}{
		UpperBound: v.UpperBound,
		Count:      v.Count,
	}
	return json.Marshal(vv)
}

// UnmarshalJSON implements json.Unmarshaler.
func (v *SampleValue) UnmarshalJSON(b []byte) error {
	vv := struct {
		UpperBound float64
		Count      int64
	}{
		UpperBound: v.UpperBound,
		Count:      v.Count,
	}

	if err := json.Unmarshal(b, &vv); err != nil {
		return err
	}

	v.UpperBound = vv.UpperBound
	v.Count = vv.Count
	return nil
}

func (v SampleValue) String() string {
	return fmt.Sprintf("%f => %d", v.UpperBound, v.Count)
}

// SamplePair pairs a SampleValue with a Timestamp.
type SamplePair struct {
	Timestamp model.Time
	Value     []SampleValue
}

// MarshalJSON implements json.Marshaler.
func (s SamplePair) MarshalJSON() ([]byte, error) {
	t, err := json.Marshal(s.Timestamp)
	if err != nil {
		return nil, err
	}
	v, err := json.Marshal(s.Value)
	if err != nil {
		return nil, err
	}
	return []byte(fmt.Sprintf("[%s,%s]", t, v)), nil
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *SamplePair) UnmarshalJSON(b []byte) error {
	var parseError error
	_, err := jsonparser.ArrayEach(b, func(value []byte, dataType jsonparser.ValueType, offset int, _ error) {
		switch dataType.String() {
		case "number":
			var t model.Time
			if err := t.UnmarshalJSON(value); err != nil {
				parseError = err
			}
			s.Timestamp = t
		case "array":
			_, err := jsonparser.ArrayEach(value, func(value []byte, ty jsonparser.ValueType, _ int, _ error) {
				if ty == jsonparser.Null {
					return
				}
				var sv SampleValue
				if err := sv.UnmarshalJSON(value); err != nil {
					parseError = err
					return
				}
				s.Value = append(s.Value, sv)
			})
			parseError = err
		}
	})

	if parseError != nil {
		return parseError
	}
	return err
}

func (s SamplePair) String() string {
	vals := make([]string, len(s.Value))
	for i, v := range s.Value {
		vals[i] = v.String()
	}
	return fmt.Sprintf("[%s] @[%s]", strings.Join(vals, ","), s.Timestamp)
}

// SampleStream is a stream of Values belonging to an attached COWMetric.
type Stream struct {
	Metric model.Metric `json:"metric"`
	Values []SamplePair `json:"histvalues"`
}

func (ss Stream) String() string {
	vals := make([]string, len(ss.Values))
	for i, v := range ss.Values {
		vals[i] = v.String()
	}
	return fmt.Sprintf("%s =>\n%s", ss.Metric, strings.Join(vals, "\n"))
}
