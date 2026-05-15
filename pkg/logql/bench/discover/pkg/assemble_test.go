package discover

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/loghttp"

	bench "github.com/grafana/loki/v3/pkg/logql/bench"
	"github.com/grafana/loki/v3/pkg/logql/bench/discover/pkg/tsdb"
)

// TestAssembleStatistics verifies that assembleStatistics correctly counts
// streams by format and by service, and sets TotalStreams from AllSelectors.
func TestAssembleStatistics(t *testing.T) {
	tests := []struct {
		name          string
		discover      *tsdb.StructuralResult
		classify      *ClassifyResult
		wantTotal     int
		wantByFormat  map[bench.LogFormat]int
		wantByService map[string]int
	}{
		{
			name: "empty inputs",
			discover: &tsdb.StructuralResult{
				AllSelectors:  []string{},
				ByServiceName: map[string][]string{},
			},
			classify: &ClassifyResult{
				ByFormat:   map[bench.LogFormat][]string{},
				ByLabelKey: map[string][]string{},
			},
			wantTotal:     0,
			wantByFormat:  map[bench.LogFormat]int{},
			wantByService: map[string]int{},
		},
		{
			name: "two streams by format and service",
			discover: &tsdb.StructuralResult{
				AllSelectors: []string{`{env="prod",app="a"}`, `{env="prod",app="b"}`},
				ByServiceName: map[string][]string{
					"svc-a": {`{env="prod",app="a"}`},
					"svc-b": {`{env="prod",app="b"}`},
				},
			},
			classify: &ClassifyResult{
				ByFormat: map[bench.LogFormat][]string{
					bench.LogFormatJSON:   {`{env="prod",app="a"}`},
					bench.LogFormatLogfmt: {`{env="prod",app="b"}`},
				},
				ByLabelKey: map[string][]string{},
			},
			wantTotal:     2,
			wantByFormat:  map[bench.LogFormat]int{bench.LogFormatJSON: 1, bench.LogFormatLogfmt: 1},
			wantByService: map[string]int{"svc-a": 1, "svc-b": 1},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stats := assembleStatistics(tc.discover, tc.classify)
			assert.Equal(t, tc.wantTotal, stats.TotalStreams)
			assert.Equal(t, tc.wantByFormat, stats.StreamsByFormat)
			assert.Equal(t, tc.wantByService, stats.StreamsByService)
		})
	}
}

func TestAssembleMetadata(t *testing.T) {
	from := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)

	// --- Build TSDB-shaped tsdb.StructuralResult ---
	sel1 := `{cluster="prod", namespace="monitoring", pod="loki-0", service_name="loki"}`
	sel2 := `{cluster="prod", namespace="monitoring", pod="mimir-0", service_name="mimir"}`
	sel3 := `{cluster="prod", namespace="default", pod="nginx-abc", service_name="nginx"}`
	sel4 := `{cluster="staging", namespace="default", pod="grafana-0", service_name="grafana"}`

	discoverResult := &tsdb.StructuralResult{
		AllSelectors: []string{sel1, sel2, sel3, sel4},
		ByServiceName: map[string][]string{
			"loki":    {sel1},
			"mimir":   {sel2},
			"nginx":   {sel3},
			"grafana": {sel4},
		},
		LabelSets: map[string]loghttp.LabelSet{
			sel1: {"cluster": "prod", "namespace": "monitoring", "pod": "loki-0", "service_name": "loki"},
			sel2: {"cluster": "prod", "namespace": "monitoring", "pod": "mimir-0", "service_name": "mimir"},
			sel3: {"cluster": "prod", "namespace": "default", "pod": "nginx-abc", "service_name": "nginx"},
			sel4: {"cluster": "staging", "namespace": "default", "pod": "grafana-0", "service_name": "grafana"},
		},
		ByLabelKey: map[string][]string{
			"cluster":      {sel1, sel2, sel3, sel4},
			"namespace":    {sel1, sel2, sel3, sel4},
			"pod":          {sel1, sel2, sel3, sel4},
			"service_name": {sel1, sel2, sel3, sel4},
		},
	}

	// --- Build ClassifyResult ---
	classify := &ClassifyResult{
		ByFormat: map[bench.LogFormat][]string{
			bench.LogFormatLogfmt:       {sel1, sel2},
			bench.LogFormatUnstructured: {sel3},
			bench.LogFormatJSON:         {sel4},
		},
		ByUnwrappableField: map[string][]string{
			"duration": {sel1, sel2, sel4},
			"bytes":    {sel1, sel2},
		},
		ByDetectedField: map[string][]string{
			"level":  {sel1, sel2, sel4},
			"msg":    {sel1, sel2, sel4},
			"caller": {sel1, sel2},
		},
		ByStructuredMetadata: map[string][]string{
			"trace_id": {sel1, sel2},
		},
		ByLabelKey: map[string][]string{
			"cluster":      {sel1, sel2, sel3, sel4},
			"namespace":    {sel1, sel2, sel3, sel4},
			"pod":          {sel1, sel2, sel3, sel4},
			"service_name": {sel1, sel2, sel3, sel4},
		},
	}

	// --- Build KeywordResult ---
	keywords := &KeywordResult{
		ByKeyword: map[string][]string{
			"error":   {sel1, sel2, sel4},
			"warn":    {sel1, sel2},
			"timeout": {sel4},
		},
	}

	// --- Build tsdb.RangeResult with TSDB-derived durations ---
	ranges := &tsdb.RangeResult{
		MetadataBySelector: map[string]*bench.SerializableStreamMetadata{
			sel1: {MinRange: 5 * time.Minute, MinInstantRange: 10 * time.Minute},
			sel2: {MinRange: 5 * time.Minute, MinInstantRange: 10 * time.Minute},
			sel3: {MinRange: 10 * time.Minute, MinInstantRange: 15 * time.Minute},
			sel4: {MinRange: 5 * time.Minute, MinInstantRange: 10 * time.Minute},
		},
	}

	cfg := Config{From: from, To: to}

	// --- Assemble ---
	md := AssembleMetadata(discoverResult, classify, keywords, ranges, cfg)
	require.NotNil(t, md)

	// Verify assembly preserves all fields (ASMB-01).
	assert.Equal(t, bench.MetadataVersion, md.Version)
	assert.Equal(t, discoverResult.AllSelectors, md.AllSelectors)
	assert.Equal(t, discoverResult.ByServiceName, md.ByServiceName)
	assert.Equal(t, classify.ByFormat, md.ByFormat)
	assert.Equal(t, classify.ByUnwrappableField, md.ByUnwrappableField)
	assert.Equal(t, classify.ByDetectedField, md.ByDetectedField)
	assert.Equal(t, classify.ByStructuredMetadata, md.ByStructuredMetadata)
	assert.Equal(t, classify.ByLabelKey, md.ByLabelKey)
	assert.Equal(t, keywords.ByKeyword, md.ByKeyword)
	assert.Equal(t, ranges.MetadataBySelector, md.MetadataBySelector)
	assert.Equal(t, 4, md.Statistics.TotalStreams)

	// Verify TimeRange is populated via effectiveFrom/effectiveTo.
	assert.Equal(t, cfg.effectiveFrom(), md.TimeRange.Start)
	assert.Equal(t, cfg.effectiveTo(), md.TimeRange.End)
	assert.Equal(t, from, md.TimeRange.Start)
	assert.Equal(t, to, md.TimeRange.End)

	// --- Round-trip through SaveMetadata / LoadMetadata (ASMB-02) ---
	tmpDir := t.TempDir()
	require.NoError(t, bench.SaveMetadata(tmpDir, md))

	loaded, err := bench.LoadMetadata(tmpDir)
	require.NoError(t, err)
	require.NotNil(t, loaded)

	assert.Equal(t, bench.MetadataVersion, loaded.Version)
	assert.Equal(t, md.AllSelectors, loaded.AllSelectors)
	assert.Equal(t, md.ByServiceName, loaded.ByServiceName)
	assert.Equal(t, md.ByFormat, loaded.ByFormat)
	assert.Equal(t, md.ByKeyword, loaded.ByKeyword)
	assert.Equal(t, md.ByUnwrappableField, loaded.ByUnwrappableField)
	assert.Equal(t, md.ByDetectedField, loaded.ByDetectedField)
	assert.Equal(t, md.ByStructuredMetadata, loaded.ByStructuredMetadata)
	assert.Equal(t, md.ByLabelKey, loaded.ByLabelKey)

	// Verify MetadataBySelector durations survive JSON serialization.
	require.Len(t, loaded.MetadataBySelector, 4)
	for sel, want := range md.MetadataBySelector {
		got, ok := loaded.MetadataBySelector[sel]
		require.True(t, ok, "selector %q missing from loaded MetadataBySelector", sel)
		assert.Equal(t, want.MinRange, got.MinRange, "MinRange mismatch for %s", sel)
		assert.Equal(t, want.MinInstantRange, got.MinInstantRange, "MinInstantRange mismatch for %s", sel)
	}

	// Verify TimeRange survives round-trip.
	assert.True(t, md.TimeRange.Start.Equal(loaded.TimeRange.Start), "TimeRange.Start mismatch")
	assert.True(t, md.TimeRange.End.Equal(loaded.TimeRange.End), "TimeRange.End mismatch")
}
