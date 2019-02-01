// Copyright 2014 The Prometheus Authors
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

package prometheus

import (
	"fmt"
	"math"
	"testing"

	dto "github.com/prometheus/client_model/go"
)

func TestCounterAdd(t *testing.T) {
	counter := NewCounter(CounterOpts{
		Name:        "test",
		Help:        "test help",
		ConstLabels: Labels{"a": "1", "b": "2"},
	}).(*counter)
	counter.Inc()
	if expected, got := 0.0, math.Float64frombits(counter.valBits); expected != got {
		t.Errorf("Expected %f, got %f.", expected, got)
	}
	if expected, got := uint64(1), counter.valInt; expected != got {
		t.Errorf("Expected %d, got %d.", expected, got)
	}
	counter.Add(42)
	if expected, got := 0.0, math.Float64frombits(counter.valBits); expected != got {
		t.Errorf("Expected %f, got %f.", expected, got)
	}
	if expected, got := uint64(43), counter.valInt; expected != got {
		t.Errorf("Expected %d, got %d.", expected, got)
	}

	counter.Add(24.42)
	if expected, got := 24.42, math.Float64frombits(counter.valBits); expected != got {
		t.Errorf("Expected %f, got %f.", expected, got)
	}
	if expected, got := uint64(43), counter.valInt; expected != got {
		t.Errorf("Expected %d, got %d.", expected, got)
	}

	if expected, got := "counter cannot decrease in value", decreaseCounter(counter).Error(); expected != got {
		t.Errorf("Expected error %q, got %q.", expected, got)
	}

	m := &dto.Metric{}
	counter.Write(m)

	if expected, got := `label:<name:"a" value:"1" > label:<name:"b" value:"2" > counter:<value:67.42 > `, m.String(); expected != got {
		t.Errorf("expected %q, got %q", expected, got)
	}
}

func decreaseCounter(c *counter) (err error) {
	defer func() {
		if e := recover(); e != nil {
			err = e.(error)
		}
	}()
	c.Add(-1)
	return nil
}

func TestCounterVecGetMetricWithInvalidLabelValues(t *testing.T) {
	testCases := []struct {
		desc   string
		labels Labels
	}{
		{
			desc:   "non utf8 label value",
			labels: Labels{"a": "\xFF"},
		},
		{
			desc:   "not enough label values",
			labels: Labels{},
		},
		{
			desc:   "too many label values",
			labels: Labels{"a": "1", "b": "2"},
		},
	}

	for _, test := range testCases {
		counterVec := NewCounterVec(CounterOpts{
			Name: "test",
		}, []string{"a"})

		labelValues := make([]string, len(test.labels))
		for _, val := range test.labels {
			labelValues = append(labelValues, val)
		}

		expectPanic(t, func() {
			counterVec.WithLabelValues(labelValues...)
		}, fmt.Sprintf("WithLabelValues: expected panic because: %s", test.desc))
		expectPanic(t, func() {
			counterVec.With(test.labels)
		}, fmt.Sprintf("WithLabelValues: expected panic because: %s", test.desc))

		if _, err := counterVec.GetMetricWithLabelValues(labelValues...); err == nil {
			t.Errorf("GetMetricWithLabelValues: expected error because: %s", test.desc)
		}
		if _, err := counterVec.GetMetricWith(test.labels); err == nil {
			t.Errorf("GetMetricWith: expected error because: %s", test.desc)
		}
	}
}

func expectPanic(t *testing.T, op func(), errorMsg string) {
	defer func() {
		if err := recover(); err == nil {
			t.Error(errorMsg)
		}
	}()

	op()
}

func TestCounterAddInf(t *testing.T) {
	counter := NewCounter(CounterOpts{
		Name: "test",
		Help: "test help",
	}).(*counter)

	counter.Inc()
	if expected, got := 0.0, math.Float64frombits(counter.valBits); expected != got {
		t.Errorf("Expected %f, got %f.", expected, got)
	}
	if expected, got := uint64(1), counter.valInt; expected != got {
		t.Errorf("Expected %d, got %d.", expected, got)
	}

	counter.Add(math.Inf(1))
	if expected, got := math.Inf(1), math.Float64frombits(counter.valBits); expected != got {
		t.Errorf("valBits expected %f, got %f.", expected, got)
	}
	if expected, got := uint64(1), counter.valInt; expected != got {
		t.Errorf("valInts expected %d, got %d.", expected, got)
	}

	counter.Inc()
	if expected, got := math.Inf(1), math.Float64frombits(counter.valBits); expected != got {
		t.Errorf("Expected %f, got %f.", expected, got)
	}
	if expected, got := uint64(2), counter.valInt; expected != got {
		t.Errorf("Expected %d, got %d.", expected, got)
	}

	m := &dto.Metric{}
	counter.Write(m)

	if expected, got := `counter:<value:inf > `, m.String(); expected != got {
		t.Errorf("expected %q, got %q", expected, got)
	}
}

func TestCounterAddLarge(t *testing.T) {
	counter := NewCounter(CounterOpts{
		Name: "test",
		Help: "test help",
	}).(*counter)

	// large overflows the underlying type and should therefore be stored in valBits.
	large := float64(math.MaxUint64 + 1)
	counter.Add(large)
	if expected, got := large, math.Float64frombits(counter.valBits); expected != got {
		t.Errorf("valBits expected %f, got %f.", expected, got)
	}
	if expected, got := uint64(0), counter.valInt; expected != got {
		t.Errorf("valInts expected %d, got %d.", expected, got)
	}

	m := &dto.Metric{}
	counter.Write(m)

	if expected, got := fmt.Sprintf("counter:<value:%0.16e > ", large), m.String(); expected != got {
		t.Errorf("expected %q, got %q", expected, got)
	}
}

func TestCounterAddSmall(t *testing.T) {
	counter := NewCounter(CounterOpts{
		Name: "test",
		Help: "test help",
	}).(*counter)
	small := 0.000000000001
	counter.Add(small)
	if expected, got := small, math.Float64frombits(counter.valBits); expected != got {
		t.Errorf("valBits expected %f, got %f.", expected, got)
	}
	if expected, got := uint64(0), counter.valInt; expected != got {
		t.Errorf("valInts expected %d, got %d.", expected, got)
	}

	m := &dto.Metric{}
	counter.Write(m)

	if expected, got := fmt.Sprintf("counter:<value:%0.0e > ", small), m.String(); expected != got {
		t.Errorf("expected %q, got %q", expected, got)
	}
}
