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

package trace_test

import (
	"context"
	"testing"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/trace"
)

func TestTraceExemplar(t *testing.T) {
	m := stats.Float64("measure."+t.Name(), "", stats.UnitDimensionless)
	v := &view.View{
		Measure:     m,
		Aggregation: view.Distribution(0, 1, 2, 3),
	}
	view.Register(v)
	ctx := context.Background()
	ctx, span := trace.StartSpan(ctx, t.Name(), trace.WithSampler(trace.AlwaysSample()))
	stats.Record(ctx, m.M(1.5))
	span.End()

	rows, err := view.RetrieveData(v.Name)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("len(rows) = 0; want > 0")
	}
	dd := rows[0].Data.(*view.DistributionData)
	if got := len(dd.ExemplarsPerBucket); got < 3 {
		t.Fatalf("len(dd.ExemplarsPerBucket) = %d; want >= 2", got)
	}
	exemplar := dd.ExemplarsPerBucket[2]
	if exemplar == nil {
		t.Fatal("Expected exemplar")
	}
	if got, want := exemplar.Value, 1.5; got != want {
		t.Fatalf("exemplar.Value = %v; got %v", got, want)
	}
	if _, ok := exemplar.Attachments["trace_id"]; !ok {
		t.Fatalf("exemplar.Attachments = %v; want trace_id key", exemplar.Attachments)
	}
	if _, ok := exemplar.Attachments["span_id"]; !ok {
		t.Fatalf("exemplar.Attachments = %v; want span_id key", exemplar.Attachments)
	}
}

func TestTraceExemplar_notSampled(t *testing.T) {
	m := stats.Float64("measure."+t.Name(), "", stats.UnitDimensionless)
	v := &view.View{
		Measure:     m,
		Aggregation: view.Distribution(0, 1, 2, 3),
	}
	view.Register(v)
	ctx := context.Background()
	ctx, span := trace.StartSpan(ctx, t.Name(), trace.WithSampler(trace.NeverSample()))
	stats.Record(ctx, m.M(1.5))
	span.End()

	rows, err := view.RetrieveData(v.Name)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("len(rows) = 0; want > 0")
	}
	dd := rows[0].Data.(*view.DistributionData)
	if got := len(dd.ExemplarsPerBucket); got < 3 {
		t.Fatalf("len(buckets) = %d; want >= 2", got)
	}
	exemplar := dd.ExemplarsPerBucket[2]
	if exemplar == nil {
		t.Fatal("Expected exemplar")
	}
	if got, want := exemplar.Value, 1.5; got != want {
		t.Fatalf("exemplar.Value = %v; got %v", got, want)
	}
	if _, ok := exemplar.Attachments["trace_id"]; ok {
		t.Fatalf("exemplar.Attachments = %v; want no trace_id", exemplar.Attachments)
	}
	if _, ok := exemplar.Attachments["span_id"]; ok {
		t.Fatalf("exemplar.Attachments = %v; want span_id key", exemplar.Attachments)
	}
}
