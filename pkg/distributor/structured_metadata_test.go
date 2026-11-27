package distributor

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/prometheus/otlptranslator"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/grafana/loki/v3/pkg/validation"
)

// labelNameKeptByNamer reports whether the namer that normalizeStructuredMetadata uses returns name
// itself without an error, which labelNameIsNormalized must agree with.
func labelNameKeptByNamer(name string) bool {
	namer := otlptranslator.LabelNamer{}
	got, err := namer.Build(name)
	return err == nil && got == name
}

func TestLabelNameIsNormalized(t *testing.T) {
	for _, tc := range []struct {
		name string
		want bool
	}{
		{name: "trace_id", want: true},
		{name: "a", want: true},
		{name: "Abc09", want: true},
		{name: "a_", want: true},
		{name: "_a", want: true},
		{name: "__name__", want: true},
		{name: "___a__", want: true},
		{name: "__a___", want: true},
		{name: "", want: false},
		{name: "_", want: false},
		{name: "____", want: false},
		{name: "_____", want: false},
		{name: "1a", want: false},
		{name: "a__b", want: false},
		{name: "__a", want: false},
		{name: "a__", want: false},
		{name: "__a__b__", want: false},
		{name: "____a__", want: false},
		{name: "http.method", want: false},
		{name: "a-b", want: false},
		{name: "é", want: false},
		{name: "a b", want: false},
		{name: "a_b_c", want: true},
		{name: "k8s_pod_name", want: true},
		{name: "A", want: true},
		{name: "_1", want: true},
		{name: "_a_b_", want: true},
		{name: "__a__", want: true},
		{name: "__1__", want: true},
		{name: "__a_b__", want: true},
		{name: "___a___", want: true},
		{name: "__", want: false},
		{name: "___", want: false},
		{name: "a___", want: false},
		{name: "1", want: false},
		{name: "a\x00", want: false},
		{name: "a\tb", want: false},
		{name: "a\xffb", want: false},
		{name: "aé", want: false},
		{name: "٣", want: false},
		{name: "Ⅻ", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, labelNameIsNormalized(tc.name))
			require.Equal(t, labelNameKeptByNamer(tc.name), labelNameIsNormalized(tc.name), "must agree with otlptranslator")
		})
	}
}

func FuzzLabelNameIsNormalized(f *testing.F) {
	for _, seed := range []string{"trace_id", "__name__", "___a__", "a__b", "1a", "_", "____", "http.method", "é", "__a___"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, name string) {
		if got, want := labelNameIsNormalized(name), labelNameKeptByNamer(name); got != want {
			t.Fatalf("labelNameIsNormalized(%q) = %t, but otlptranslator keeps it: %t", name, got, want)
		}
	})
}

// slowPathEntryMetadata is how processStreamEntries normalized an entry's structured metadata
// before normalizeEntryStructuredMetadata, which must produce the same result.
func slowPathEntryMetadata(d *Distributor, metadata []logproto.LabelAdapter) ([]logproto.LabelAdapter, labels.Labels, error) {
	structuredMetadata, _, err := d.normalizeStructuredMetadata(metadata, "test", constants.Loki, false)
	if err != nil {
		return metadata, structuredMetadata, err
	}
	return logproto.CopyToLabelAdapters(metadata, structuredMetadata), structuredMetadata, nil
}

// requireSameAsSlowPath checks that normalizeEntryStructuredMetadata leaves the entry's metadata,
// returns the labels and the error, and counts the sanitizations exactly as the slow path does.
func requireSameAsSlowPath(t testing.TB, metadata []logproto.LabelAdapter) {
	t.Helper()
	// slices.Clone keeps an empty slice non-nil, so both paths see nil and empty metadata as given.
	slowDistributor := &Distributor{m: newMetrics(prometheus.NewRegistry())}
	wantMetadata, wantLabels, wantErr := slowPathEntryMetadata(slowDistributor, slices.Clone(metadata))
	wantSanitized := testutil.ToFloat64(slowDistributor.m.tenantPushSanitizedStructuredMetadata.WithLabelValues("test", constants.Loki))

	for _, withLabels := range []bool{true, false} {
		d := &Distributor{m: newMetrics(prometheus.NewRegistry())}
		entry := logproto.Entry{StructuredMetadata: slices.Clone(metadata)}
		var b labels.ScratchBuilder
		gotLabels, gotErr := d.normalizeEntryStructuredMetadata(&entry, "test", constants.Loki, withLabels, &b)

		if wantErr != nil {
			require.EqualError(t, gotErr, wantErr.Error())
			continue
		}
		require.NoError(t, gotErr)
		require.Equal(t, wantMetadata, []logproto.LabelAdapter(entry.StructuredMetadata))
		if withLabels {
			require.Equal(t, wantLabels.String(), gotLabels.String())
		} else if structuredMetadataIsNormalized(metadata) {
			require.True(t, gotLabels.IsEmpty(), "labels are only built when asked for")
		}
		require.Equal(t, wantSanitized, testutil.ToFloat64(d.m.tenantPushSanitizedStructuredMetadata.WithLabelValues("test", constants.Loki)))
	}
}

func TestNormalizeEntryStructuredMetadata(t *testing.T) {
	many := make([]logproto.LabelAdapter, 0, 20)
	for i := 19; i >= 0; i-- {
		many = append(many, logproto.LabelAdapter{Name: fmt.Sprintf("attr_%02d", i), Value: "v"})
	}
	// Duplicates carry other values, so the test sees the order the sort leaves them in.
	manyWithDuplicates := append([]logproto.LabelAdapter(nil), many...)
	for i, l := range many[3:9] {
		manyWithDuplicates = append(manyWithDuplicates, logproto.LabelAdapter{Name: l.Name, Value: fmt.Sprintf("dup_%d", i)})
	}

	for _, tc := range []struct {
		name     string
		metadata []logproto.LabelAdapter
		fastPath bool
	}{
		{name: "nil", fastPath: true},
		{name: "empty", metadata: []logproto.LabelAdapter{}, fastPath: true},
		{name: "sorted and valid", metadata: buildNestedAttrs("a", "1", "b", "2"), fastPath: true},
		{name: "unsorted and valid", metadata: buildNestedAttrs("span_id", "s", "trace_id", "t", "http_method", "GET"), fastPath: true},
		{name: "non-ASCII value", metadata: buildNestedAttrs("z", "私", "a", "value"), fastPath: true},
		{name: "duplicate names", metadata: buildNestedAttrs("b", "1", "a", "2", "b", "3"), fastPath: true},
		{name: "many unsorted", metadata: many, fastPath: true},
		{name: "many with duplicate names", metadata: manyWithDuplicates, fastPath: true},
		{name: "reserved name", metadata: buildNestedAttrs("__name__", "x"), fastPath: true},
		{name: "dotted name", metadata: buildNestedAttrs("http.method", "GET", "a", "1")},
		{name: "numeric name", metadata: buildNestedAttrs("1", "value")},
		{name: "repeated underscores", metadata: buildNestedAttrs("a__b", "value")},
		{name: "normalized name replaces an existing name", metadata: buildNestedAttrs("a.b", "normalized", "a_b", "original")},
		{name: "empty value", metadata: buildNestedAttrs("empty", "", "keep", "v")},
		{name: "invalid UTF-8", metadata: buildNestedAttrs("value", "a\xffb")},
		{name: "replacement rune", metadata: buildNestedAttrs("value", "a�b")},
		{name: "empty name", metadata: buildNestedAttrs("", "value")},
		{name: "only underscores", metadata: buildNestedAttrs("service.name", "loki", "__", "value")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.fastPath, structuredMetadataIsNormalized(tc.metadata))
			requireSameAsSlowPath(t, tc.metadata)
		})
	}
}

// encodeFuzzMetadata encodes names and values for FuzzNormalizeEntryStructuredMetadata, each as a
// length byte followed by its bytes. Every string must be shorter than 256 bytes.
func encodeFuzzMetadata(namesAndValues ...string) []byte {
	var data []byte
	for _, s := range namesAndValues {
		data = append(data, byte(len(s)))
		data = append(data, s...)
	}
	return data
}

// decodeFuzzMetadata reads metadata encoded by encodeFuzzMetadata: a name, then a value, each a
// length byte followed by that many bytes. A length past the end of data takes what is left, and a
// missing value is empty, so any input decodes and any byte can appear in a name or a value.
func decodeFuzzMetadata(data []byte) []logproto.LabelAdapter {
	next := func() string {
		if len(data) == 0 {
			return ""
		}
		n := min(int(data[0]), len(data)-1)
		s := string(data[1 : 1+n])
		data = data[1+n:]
		return s
	}
	var metadata []logproto.LabelAdapter
	for len(data) > 0 {
		name := next()
		metadata = append(metadata, logproto.LabelAdapter{Name: name, Value: next()})
	}
	return metadata
}

func TestDecodeFuzzMetadata(t *testing.T) {
	want := buildNestedAttrs("a;b", "c=d", "", "x", "empty", "")
	require.Equal(t, want, decodeFuzzMetadata(encodeFuzzMetadata("a;b", "c=d", "", "x", "empty", "")))
	require.Equal(t, buildNestedAttrs("ab", "c"), decodeFuzzMetadata([]byte("\x02ab\x09c")), "a length past the end takes what is left")
	require.Equal(t, buildNestedAttrs("ab", ""), decodeFuzzMetadata([]byte("\x02ab")), "a missing value is empty")
	require.Nil(t, decodeFuzzMetadata(nil))
}

func FuzzNormalizeEntryStructuredMetadata(f *testing.F) {
	f.Add(encodeFuzzMetadata("trace_id", "4bf9", "span_id", "00f0", "severity_text", "INFO"))
	f.Add(encodeFuzzMetadata("b", "1", "a", "2", "b", "3"))
	f.Add(encodeFuzzMetadata("http.method", "GET", "a__b", "1", "__name__", "x", "1a", "2"))
	f.Add(encodeFuzzMetadata("empty", "", "value", "a\xffb", "", "x"))
	f.Add(encodeFuzzMetadata("a;b", "c=d", "query", "a=1;b=2"))
	// More than 12 entries, past the length up to which slices.SortFunc uses insertion sort, with
	// duplicate names.
	f.Add(encodeFuzzMetadata(
		"m", "1", "l", "2", "k", "3", "j", "4", "i", "5", "h", "6", "g", "7",
		"f", "8", "e", "9", "d", "10", "c", "11", "b", "12", "a", "13", "m", "14", "f", "15",
	))
	f.Fuzz(func(t *testing.T, data []byte) {
		requireSameAsSlowPath(t, decodeFuzzMetadata(data))
	})
}

// otlpLikeAttrs is the structured metadata of a typical OTLP log record after the push parser has
// normalized its names: compliant names, in attribute order rather than sorted.
var otlpLikeAttrs = [][2]string{
	{"trace_id", "4bf92f3577b34da6a3ce929d0e0e4736"},
	{"span_id", "00f067aa0ba902b7"},
	{"severity_text", "INFO"},
	{"severity_number", "9"},
	{"http_method", "POST"},
	{"http_status_code", "200"},
	{"user_id", "a8d1c2"},
	{"k8s_pod_name", "checkout-7d9f8b6c4d-x2lqz"},
}

func benchmarkStream(numEntries int, attrs [][2]string) logproto.InternalStreamAdapter {
	now := time.Now()
	entries := make([]logproto.Entry, numEntries)
	for i := range entries {
		var sm []logproto.LabelAdapter
		if len(attrs) > 0 {
			sm = make([]logproto.LabelAdapter, len(attrs))
			for j, a := range attrs {
				sm[j] = logproto.LabelAdapter{Name: a[0], Value: a[1]}
			}
		}
		entries[i] = logproto.Entry{
			Timestamp:          now,
			Line:               fmt.Sprintf(`ts=2026-11-26T10:00:00Z level=info msg="request served" path=/api/v1/push status=200 duration=%dms`, i%500),
			StructuredMetadata: sm,
		}
	}
	return logproto.InternalStreamAdapter{
		Labels:       `{service_name="checkout"}`,
		ResourceLogs: []logproto.ResourceLogs{{ScopeLogs: []logproto.ScopeLogs{{Entries: entries}}}},
	}
}

// BenchmarkProcessStreamEntries measures validating and normalizing one stream of 1,000 entries,
// which is the per-entry part of a push.
func BenchmarkProcessStreamEntries(b *testing.B) {
	dotted := append([][2]string(nil), otlpLikeAttrs...)
	dotted[4] = [2]string{"http.method", "POST"}

	for _, discoverLevels := range []bool{true, false} {
		for _, tc := range []struct {
			name  string
			attrs [][2]string
		}{
			{name: "no_metadata"},
			{name: "one_valid_attr", attrs: otlpLikeAttrs[:1]},
			{name: "otlp_like_8_valid_attrs", attrs: otlpLikeAttrs},
			{name: "otlp_like_8_attrs_one_dotted_name", attrs: dotted},
		} {
			b.Run(fmt.Sprintf("discover_levels=%t/%s", discoverLevels, tc.name), func(b *testing.B) {
				lim := &validation.Limits{}
				flagext.DefaultValues(lim)
				lim.DiscoverLogLevels = discoverLevels
				overrides, err := validation.NewOverrides(*lim, nil)
				require.NoError(b, err)
				v, err := NewValidator(overrides, nil)
				require.NoError(b, err)
				d := &Distributor{validator: v, m: newMetrics(prometheus.NewRegistry())}
				lbs := labels.FromStrings("service_name", "checkout")
				ctx := context.Background()

				b.ReportAllocs()
				for range b.N {
					b.StopTimer()
					stream := benchmarkStream(1000, tc.attrs)
					vCtx := v.getValidationContextForTime(time.Now(), "test")
					var validationErrors util.GroupedErrors
					b.StartTimer()

					stats, err := d.processStreamEntries(ctx, vCtx, &stream, lbs, "744", "", constants.OTLP, newFieldDetector(ctx, vCtx), &validationErrors)
					if err != nil || stats.entriesKept != 1000 {
						b.Fatalf("unexpected result: kept %d entries, err %v", stats.entriesKept, err)
					}
				}
			})
		}
	}
}
