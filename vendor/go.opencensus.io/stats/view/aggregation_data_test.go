// Copyright 2018, OpenCensus Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

package view

import (
	"reflect"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"go.opencensus.io/exemplar"
)

func TestDataClone(t *testing.T) {
	dist := newDistributionData([]float64{1, 2, 3, 4})
	dist.Count = 7
	dist.Max = 11
	dist.Min = 1
	dist.CountPerBucket = []int64{0, 2, 3, 2}
	dist.Mean = 4
	dist.SumOfSquaredDev = 1.2

	tests := []struct {
		name string
		src  AggregationData
	}{
		{
			name: "count data",
			src:  &CountData{Value: 5},
		},
		{
			name: "distribution data",
			src:  dist,
		},
		{
			name: "sum data",
			src:  &SumData{Value: 65.7},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.src.clone()
			if !reflect.DeepEqual(got, tt.src) {
				t.Errorf("AggregationData.clone() = %v, want %v", got, tt.src)
			}
			// TODO(jbd): Make sure that data is deep copied.
			if got == tt.src {
				t.Errorf("AggregationData.clone() returned the same pointer")
			}
		})
	}
}

func TestDistributionData_addSample(t *testing.T) {
	dd := newDistributionData([]float64{0, 1, 2})
	t1, _ := time.Parse("Mon Jan 2 15:04:05 -0700 MST 2006", "Mon Jan 2 15:04:05 -0700 MST 2006")
	e1 := &exemplar.Exemplar{
		Attachments: exemplar.Attachments{
			"tag:X": "Y",
			"tag:A": "B",
		},
		Timestamp: t1,
		Value:     0.5,
	}
	dd.addSample(e1)

	want := &DistributionData{
		Count:              1,
		CountPerBucket:     []int64{0, 1, 0, 0},
		ExemplarsPerBucket: []*exemplar.Exemplar{nil, e1, nil, nil},
		Max:                0.5,
		Min:                0.5,
		Mean:               0.5,
		SumOfSquaredDev:    0,
	}
	if diff := cmpDD(dd, want); diff != "" {
		t.Fatalf("Unexpected DistributionData -got +want: %s", diff)
	}

	t2 := t1.Add(time.Microsecond)
	e2 := &exemplar.Exemplar{
		Attachments: exemplar.Attachments{
			"tag:X": "Y",
		},
		Timestamp: t2,
		Value:     0.7,
	}
	dd.addSample(e2)

	// Previous exemplar should be preserved, since it has more annotations.
	want = &DistributionData{
		Count:              2,
		CountPerBucket:     []int64{0, 2, 0, 0},
		ExemplarsPerBucket: []*exemplar.Exemplar{nil, e1, nil, nil},
		Max:                0.7,
		Min:                0.5,
		Mean:               0.6,
		SumOfSquaredDev:    0,
	}
	if diff := cmpDD(dd, want); diff != "" {
		t.Fatalf("Unexpected DistributionData -got +want: %s", diff)
	}

	t3 := t2.Add(time.Microsecond)
	e3 := &exemplar.Exemplar{
		Attachments: exemplar.Attachments{
			exemplar.KeyTraceID: "abcd",
		},
		Timestamp: t3,
		Value:     0.2,
	}
	dd.addSample(e3)

	// Exemplar should be replaced since it has a trace_id.
	want = &DistributionData{
		Count:              3,
		CountPerBucket:     []int64{0, 3, 0, 0},
		ExemplarsPerBucket: []*exemplar.Exemplar{nil, e3, nil, nil},
		Max:                0.7,
		Min:                0.2,
		Mean:               0.4666666666666667,
		SumOfSquaredDev:    0,
	}
	if diff := cmpDD(dd, want); diff != "" {
		t.Fatalf("Unexpected DistributionData -got +want: %s", diff)
	}
}

func cmpDD(got, want *DistributionData) string {
	return cmp.Diff(got, want, cmpopts.IgnoreFields(DistributionData{}, "SumOfSquaredDev"), cmpopts.IgnoreUnexported(DistributionData{}))
}
