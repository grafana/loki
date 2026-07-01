package logproto

// Serialization micro-benchmarks for the repeated-message-field shapes that
// carry (wiresmith.options.pointer) = true.
//
// Purpose: measure whether []*T on hot serialized types causes meaningful
// per-element heap allocation on Unmarshal vs the []T alternative.
//
// Payload sizing rationale:
//   - FilterChunkRefResponse (bloom-gateway): 1 000 series × 10 ShortRef each
//     (realistic for a single bloom-shard response; large deployments see 10k+
//     series but 1 000 is the p50 shard size).
//   - ChunkRefGroup (store path): 5 000 ChunkRef per group (p90 chunk count for
//     a high-cardinality stream over a 1-hour window).
//   - QueryPatternsResponse: 100 PatternSeries × 500 PatternSample each
//     (typical 8-hour query at 1-minute step over 100 patterns).
//   - QuantileSketchMatrix: 100 QuantileSketchVector × 50 centroids in each
//     TDigest sample (conservative; real t-digests have 100-300 centroids).
//
// Benchmarks are split Marshal / Unmarshal / Size so the pprof -alloc_objects
// view isolates decode-side per-element allocs from encode-side.

import (
	"testing"

	"github.com/prometheus/common/model"
)

// ---- FilterChunkRefResponse (bloom-gateway) --------------------------------
// Encodes []*GroupedChunkRefs where each GroupedChunkRefs.Refs = []*ShortRef.

func makeFilterChunkRefResponse(nSeries, nRefsPerSeries int) *FilterChunkRefResponse {
	groups := make([]*GroupedChunkRefs, nSeries)
	for i := range groups {
		refs := make([]*ShortRef, nRefsPerSeries)
		for j := range refs {
			refs[j] = &ShortRef{
				From:     model.Time(int64(i*1000 + j)),
				Through:  model.Time(int64(i*1000 + j + 100)),
				Checksum: uint32(i*nRefsPerSeries + j),
			}
		}
		groups[i] = &GroupedChunkRefs{
			Fingerprint: uint64(i),
			Tenant:      "tenant-a",
			Refs:        refs,
		}
	}
	return &FilterChunkRefResponse{ChunkRefs: groups}
}

func BenchmarkFilterChunkRefResponse_Marshal(b *testing.B) {
	msg := makeFilterChunkRefResponse(1000, 10)
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		_, _ = msg.Marshal()
	}
}

func BenchmarkFilterChunkRefResponse_Size(b *testing.B) {
	msg := makeFilterChunkRefResponse(1000, 10)
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		_ = msg.Size()
	}
}

func BenchmarkFilterChunkRefResponse_Unmarshal(b *testing.B) {
	msg := makeFilterChunkRefResponse(1000, 10)
	data, err := msg.Marshal()
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		var out FilterChunkRefResponse
		if err := out.Unmarshal(data); err != nil {
			b.Fatal(err)
		}
	}
}

// ---- ChunkRefGroup (store / shard path) ------------------------------------
// Encodes []*ChunkRef.

func makeChunkRefGroup(nRefs int) *ChunkRefGroup {
	refs := make([]*ChunkRef, nRefs)
	for i := range refs {
		refs[i] = &ChunkRef{
			Fingerprint: uint64(i),
			UserID:      "tenant-a",
			From:        model.Time(int64(i * 60000)),
			Through:     model.Time(int64(i*60000 + 3600000)),
			Checksum:    uint32(i),
		}
	}
	return &ChunkRefGroup{Refs: refs}
}

func BenchmarkChunkRefGroup_Marshal(b *testing.B) {
	msg := makeChunkRefGroup(5000)
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		_, _ = msg.Marshal()
	}
}

func BenchmarkChunkRefGroup_Size(b *testing.B) {
	msg := makeChunkRefGroup(5000)
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		_ = msg.Size()
	}
}

func BenchmarkChunkRefGroup_Unmarshal(b *testing.B) {
	msg := makeChunkRefGroup(5000)
	data, err := msg.Marshal()
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		var out ChunkRefGroup
		if err := out.Unmarshal(data); err != nil {
			b.Fatal(err)
		}
	}
}

// ---- QueryPatternsResponse (pattern service) --------------------------------
// Encodes []*PatternSeries where each PatternSeries.Samples = []*PatternSample.

func makeQueryPatternsResponse(nSeries, nSamples int) *QueryPatternsResponse {
	series := make([]PatternSeries, nSeries)
	for i := range series {
		samples := make([]PatternSample, nSamples)
		for j := range samples {
			samples[j] = PatternSample{
				Timestamp: model.Time(int64(j * 60000)),
				Value:     int64(j + i),
			}
		}
		series[i] = PatternSeries{
			Pattern: "pattern " + string(rune('a'+i%26)),
			Samples: samples,
		}
	}
	return &QueryPatternsResponse{Series: series}
}

func BenchmarkQueryPatternsResponse_Marshal(b *testing.B) {
	msg := makeQueryPatternsResponse(100, 500)
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		_, _ = msg.Marshal()
	}
}

func BenchmarkQueryPatternsResponse_Unmarshal(b *testing.B) {
	msg := makeQueryPatternsResponse(100, 500)
	data, err := msg.Marshal()
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		var out QueryPatternsResponse
		if err := out.Unmarshal(data); err != nil {
			b.Fatal(err)
		}
	}
}

// ---- QuantileSketchMatrix --------------------------------------------------
// Encodes []*QuantileSketchVector → []*QuantileSketchSample → []*TDigest_Centroid.

func makeQuantileSketchMatrix(nVectors, nCentroids int) *QuantileSketchMatrix {
	vecs := make([]*QuantileSketchVector, nVectors)
	for i := range vecs {
		centroids := make([]*TDigest_Centroid, nCentroids)
		for c := range centroids {
			centroids[c] = &TDigest_Centroid{
				Mean:   float64(c) * 0.1,
				Weight: 1.0,
			}
		}
		samples := []*QuantileSketchSample{
			{
				F: &QuantileSketch{
					Sketch: &QuantileSketch_Tdigest{
						Tdigest: TDigest{
							Min:         0.0,
							Max:         float64(nCentroids),
							Compression: 100.0,
							Processed:   centroids,
						},
					},
				},
				TimestampMs: int64(i * 60000),
				Metric: []*LabelPair{
					{Name: "job", Value: "querier"},
				},
			},
		}
		vecs[i] = &QuantileSketchVector{Samples: samples}
	}
	return &QuantileSketchMatrix{Values: vecs}
}

func BenchmarkQuantileSketchMatrix_Marshal(b *testing.B) {
	msg := makeQuantileSketchMatrix(100, 50)
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		_, _ = msg.Marshal()
	}
}

func BenchmarkQuantileSketchMatrix_Unmarshal(b *testing.B) {
	msg := makeQuantileSketchMatrix(100, 50)
	data, err := msg.Marshal()
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		var out QuantileSketchMatrix
		if err := out.Unmarshal(data); err != nil {
			b.Fatal(err)
		}
	}
}
