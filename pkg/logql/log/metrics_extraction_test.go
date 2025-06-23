package log

import (
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_labelSampleExtractor_Extract(t *testing.T) {
	tests := []struct {
		name               string
		ex                 SampleExtractor
		in                 labels.Labels
		structuredMetadata labels.Labels
		want               float64
		wantLbs            labels.Labels
		wantOk             bool
		line               string
	}{
		{
			name: "convert float",
			ex: mustSampleExtractor(LabelExtractorWithStages(
				"foo", ConvertFloat, nil, false, false, nil, NoopStage,
			)),
			in:      labels.FromStrings("foo", "15.0"),
			want:    15,
			wantLbs: labels.EmptyLabels(),
			wantOk:  true,
		},
		{
			name: "convert float as vector with no grouping",
			ex: mustSampleExtractor(LabelExtractorWithStages(
				"foo", ConvertFloat, nil, false, true, nil, NoopStage,
			)),
			in:      labels.FromStrings("foo", "15.0", "bar", "buzz"),
			want:    15,
			wantLbs: labels.EmptyLabels(),
			wantOk:  true,
		},
		{
			name: "convert float without",
			ex: mustSampleExtractor(LabelExtractorWithStages(
				"foo", ConvertFloat, []string{"bar", "buzz"}, true, false, nil, NoopStage,
			)),
			in: labels.FromStrings("foo", "10",
				"bar", "foo",
				"buzz", "blip",
				"namespace", "dev",
			),
			want:    10,
			wantLbs: labels.FromStrings("namespace", "dev"),
			wantOk:  true,
		},
		{
			name: "convert float with",
			ex: mustSampleExtractor(LabelExtractorWithStages(
				"foo", ConvertFloat, []string{"bar", "buzz"}, false, false, nil, NoopStage,
			)),
			in: labels.FromStrings("foo", "0.6",
				"bar", "foo",
				"buzz", "blip",
				"namespace", "dev",
			),
			want: 0.6,
			wantLbs: labels.FromStrings("bar", "foo",
				"buzz", "blip",
			),
			wantOk: true,
		},
		{
			name: "convert float with structured metadata",
			ex: mustSampleExtractor(LabelExtractorWithStages(
				"foo", ConvertFloat, nil, false, false, nil, NoopStage,
			)),
			in:                 labels.EmptyLabels(),
			structuredMetadata: labels.FromStrings("foo", "15.0"),
			want:               15,
			wantLbs:            labels.EmptyLabels(),
			wantOk:             true,
		},
		{
			name: "convert float as vector with structured metadata with no grouping",
			ex: mustSampleExtractor(LabelExtractorWithStages(
				"foo", ConvertFloat, nil, false, true, nil, NoopStage,
			)),
			in:                 labels.FromStrings("bar", "buzz"),
			structuredMetadata: labels.FromStrings("foo", "15.0", "buzz", "blip"),
			want:               15,
			wantLbs:            labels.EmptyLabels(),
			wantOk:             true,
		},
		{
			name: "convert float with structured metadata and grouping",
			ex: mustSampleExtractor(LabelExtractorWithStages(
				"foo", ConvertFloat, []string{"bar", "buzz"}, false, false, nil, NoopStage,
			)),
			in:                 labels.FromStrings("bar", "buzz", "namespace", "dev"),
			structuredMetadata: labels.FromStrings("foo", "15.0", "buzz", "blip"),
			want:               15,
			wantLbs:            labels.FromStrings("bar", "buzz", "buzz", "blip"),
			wantOk:             true,
		},
		{
			name: "convert float with structured metadata and grouping without",
			ex: mustSampleExtractor(LabelExtractorWithStages(
				"foo", ConvertFloat, []string{"bar", "buzz"}, true, false, nil, NoopStage,
			)),
			in:                 labels.FromStrings("bar", "buzz", "namespace", "dev"),
			structuredMetadata: labels.FromStrings("foo", "15.0", "buzz", "blip"),
			want:               15,
			wantLbs:            labels.FromStrings("namespace", "dev"),
			wantOk:             true,
		},
		{
			name: "convert duration with",
			ex: mustSampleExtractor(LabelExtractorWithStages(
				"foo", ConvertDuration, []string{"bar", "buzz"}, false, false, nil, NoopStage,
			)),
			in: labels.FromStrings("foo", "500ms",
				"bar", "foo",
				"buzz", "blip",
				"namespace", "dev",
			),
			want: 0.5,
			wantLbs: labels.FromStrings("bar", "foo",
				"buzz", "blip",
			),
			wantOk: true,
		},
		{
			name: "convert duration with structured metadata",
			ex: mustSampleExtractor(LabelExtractorWithStages(
				"foo", ConvertDuration, []string{"bar", "buzz"}, false, false, nil, NoopStage,
			)),
			in: labels.FromStrings(
				"bar", "foo",
				"namespace", "dev",
			),
			structuredMetadata: labels.FromStrings("foo", "500ms", "buzz", "blip"),
			want:               0.5,
			wantLbs: labels.FromStrings("bar", "foo",
				"buzz", "blip",
			),
			wantOk: true,
		},
		{
			name: "convert bytes",
			ex: mustSampleExtractor(LabelExtractorWithStages(
				"foo", ConvertBytes, []string{"bar", "buzz"}, false, false, nil, NoopStage,
			)),
			in: labels.FromStrings("foo", "13 MiB",
				"bar", "foo",
				"buzz", "blip",
				"namespace", "dev",
			),
			want: 13 * 1024 * 1024,
			wantLbs: labels.FromStrings("bar", "foo",
				"buzz", "blip",
			),
			wantOk: true,
		},
		{
			name: "convert bytes without spaces",
			ex: mustSampleExtractor(LabelExtractorWithStages(
				"foo", ConvertBytes, []string{"bar", "buzz"}, false, false, nil, NoopStage,
			)),
			in: labels.FromStrings("foo", "13MiB",
				"bar", "foo",
				"buzz", "blip",
				"namespace", "dev",
			),
			want: 13 * 1024 * 1024,
			wantLbs: labels.FromStrings("bar", "foo",
				"buzz", "blip",
			),
			wantOk: true,
		},
		{
			name: "convert bytes with structured metadata",
			ex: mustSampleExtractor(LabelExtractorWithStages(
				"foo", ConvertBytes, []string{"bar", "buzz"}, false, false, nil, NoopStage,
			)),
			in: labels.FromStrings(
				"bar", "foo",
				"namespace", "dev",
			),
			structuredMetadata: labels.FromStrings("foo", "13 MiB", "buzz", "blip"),
			want:               13 * 1024 * 1024,
			wantLbs: labels.FromStrings("bar", "foo",
				"buzz", "blip",
			),
			wantOk: true,
		},
		{
			name: "not convertable",
			ex: mustSampleExtractor(LabelExtractorWithStages(
				"foo", ConvertFloat, []string{"bar", "buzz"}, false, false, nil, NoopStage,
			)),
			in: labels.FromStrings("foo", "not_a_number",
				"bar", "foo",
			),
			wantLbs: labels.FromStrings("__error__", "SampleExtractionErr",
				"__error_details__", "strconv.ParseFloat: parsing \"not_a_number\": invalid syntax",
				"bar", "foo",
				"foo", "not_a_number",
			),
			wantOk: true,
		},
		{
			name: "not convertable with structured metadata",
			ex: mustSampleExtractor(LabelExtractorWithStages(
				"foo", ConvertFloat, []string{"bar", "buzz"}, false, false, nil, NoopStage,
			)),
			in:                 labels.FromStrings("bar", "foo"),
			structuredMetadata: labels.FromStrings("foo", "not_a_number"),
			wantLbs: labels.FromStrings("__error__", "SampleExtractionErr",
				"__error_details__", "strconv.ParseFloat: parsing \"not_a_number\": invalid syntax",
				"bar", "foo",
				"foo", "not_a_number",
			),
			wantOk: true,
		},
		{
			name: "dynamic label, convert duration",
			ex: mustSampleExtractor(LabelExtractorWithStages(
				"foo", ConvertDuration, []string{"bar", "buzz"}, false, false, []Stage{NewLogfmtParser(false, false)}, NoopStage,
			)),
			in:      labels.FromStrings("bar", "foo"),
			want:    0.1234,
			wantLbs: labels.FromStrings("bar", "foo"),
			wantOk:  true,
			line:    "foo=123.4ms",
		},
		{
			name: "dynamic label, not convertable",
			ex: mustSampleExtractor(LabelExtractorWithStages(
				"foo", ConvertDuration, []string{"bar", "buzz"}, false, false, []Stage{NewLogfmtParser(false, false)}, NoopStage,
			)),
			in: labels.FromStrings("bar", "foo"),
			wantLbs: labels.FromStrings("__error__", "SampleExtractionErr",
				"__error_details__", "time: invalid duration \"not_a_number\"",
				"bar", "foo",
				"foo", "not_a_number",
			),
			wantOk: true,
			line:   "foo=not_a_number",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			samples, ok := tt.ex.ForStream(tt.in).Process(0, []byte(tt.line), tt.structuredMetadata)
			require.Equal(t, tt.wantOk, ok)
			if ok {
				require.Len(t, samples, 1, "Expected exactly one sample")
				require.Equal(t, tt.want, samples[0].Value)
				require.Equal(t, tt.wantLbs, samples[0].Labels.Labels())
			}

			samples, ok = tt.ex.ForStream(tt.in).ProcessString(0, tt.line, tt.structuredMetadata)
			require.Equal(t, tt.wantOk, ok)
			if ok {
				require.Len(t, samples, 1, "Expected exactly one sample")
				require.Equal(t, tt.want, samples[0].Value)
				require.Equal(t, tt.wantLbs, samples[0].Labels.Labels())
			}
		})
	}
}

func Test_Extract_ExpectedLabels(t *testing.T) {
	ex := mustSampleExtractor(LabelExtractorWithStages("duration", ConvertDuration, []string{"foo"}, false, false, []Stage{NewJSONParser(false)}, NoopStage))

	samples, ok := ex.ForStream(labels.FromStrings("bar", "foo")).ProcessString(0, `{"duration":"20ms","foo":"json"}`, labels.EmptyLabels())
	require.True(t, ok)
	require.Len(t, samples, 1, "Expected exactly one sample")
	require.Equal(t, (20 * time.Millisecond).Seconds(), samples[0].Value)
	require.Equal(t, labels.FromStrings("foo", "json"), samples[0].Labels.Labels())

}
func TestLabelExtractorWithStages(t *testing.T) {

	// A helper type to check if particular logline should be skipped
	// during `ProcessLine` or got correct sample value extracted.
	type checkLine struct {
		logLine string
		skip    bool
		sample  float64
	}

	tests := []struct {
		name       string
		extractor  SampleExtractor
		checkLines []checkLine
		shouldFail bool
	}{
		{
			name: "with just logfmt and stringlabelfilter",
			// {foo="bar"} | logfmt | subqueries != "0" (note: "0", a stringlabelfilter)
			extractor: mustSampleExtractor(
				LabelExtractorWithStages("subqueries", ConvertFloat, []string{"foo"}, false, false, []Stage{NewLogfmtParser(false, false), NewStringLabelFilter(labels.MustNewMatcher(labels.MatchNotEqual, "subqueries", "0"))}, NoopStage),
			),
			checkLines: []checkLine{
				{logLine: "msg=hello subqueries=5", skip: false, sample: 5},
				{logLine: "msg=hello subqueries=0", skip: true},
				{logLine: "msg=hello ", skip: true}, // log lines doesn't contain the `subqueries` label
			},
		},
		{
			name: "with just logfmt and numeric labelfilter",
			// {foo="bar"} | logfmt | subqueries != 0 (note: "0", a numericLabelFilter)
			extractor: mustSampleExtractor(
				LabelExtractorWithStages("subqueries", ConvertFloat, []string{"foo"}, false, false, []Stage{NewLogfmtParser(false, false), NewNumericLabelFilter(LabelFilterNotEqual, "subqueries", 0)}, NoopStage),
			),
			checkLines: []checkLine{
				{logLine: "msg=hello subqueries=5", skip: false, sample: 5},
				{logLine: "msg=hello subqueries=0", skip: true},
				{logLine: "msg=hello ", skip: true}, // log lines doesn't contain the `subqueries` label
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for _, line := range tc.checkLines {
				samples, ok := tc.extractor.ForStream(labels.FromStrings("bar", "foo")).ProcessString(0, line.logLine, labels.EmptyLabels())
				skipped := !ok
				assert.Equal(t, line.skip, skipped, "line", line.logLine)
				if !skipped {
					require.Len(t, samples, 1, "Expected exactly one sample")
					assert.Equal(t, line.sample, samples[0].Value)

					// lbs shouldn't have __error__ = SampleExtractionError
					assert.Empty(t, samples[0].Labels.Labels())
					return
				}

				// if line is skipped, samples will be nil
				assert.Nil(t, samples, "line", line.logLine)
			}
		})
	}
}

func mustSampleExtractor(ex SampleExtractor, err error) SampleExtractor {
	if err != nil {
		panic(err)
	}
	return ex
}

func TestNewLineSampleExtractor(t *testing.T) {
	se, err := NewLineSampleExtractor(CountExtractor, nil, nil, false, false)
	require.NoError(t, err)

	lbs := labels.FromStrings("namespace", "dev",
		"cluster", "us-central1",
	)

	sse := se.ForStream(lbs)
	samples, ok := sse.Process(0, []byte(`foo`), labels.EmptyLabels())
	require.True(t, ok)
	require.Len(t, samples, 1, "Expected exactly one sample")
	require.Equal(t, 1., samples[0].Value)
	assertLabelResult(t, lbs, samples[0].Labels)

	samples, ok = sse.ProcessString(0, `foo`, labels.EmptyLabels())
	require.True(t, ok)
	require.Len(t, samples, 1, "Expected exactly one sample")
	require.Equal(t, 1., samples[0].Value)
	assertLabelResult(t, lbs, samples[0].Labels)

	stage := mustFilter(NewFilter("foo", LineMatchEqual)).ToStage()
	se, err = NewLineSampleExtractor(BytesExtractor, []Stage{stage}, []string{"namespace"}, false, false)
	require.NoError(t, err)

	sse = se.ForStream(lbs)
	samples, ok = sse.Process(0, []byte(`foo`), labels.EmptyLabels())
	require.True(t, ok)
	require.Len(t, samples, 1, "Expected exactly one sample")
	require.Equal(t, 3., samples[0].Value)
	assertLabelResult(t, labels.FromStrings("namespace", "dev"), samples[0].Labels)

	sse = se.ForStream(lbs)
	_, ok = sse.Process(0, []byte(`nope`), labels.EmptyLabels())
	require.False(t, ok)
}

func TestNewLineSampleExtractorWithStructuredMetadata(t *testing.T) {
	lbs := labels.FromStrings("foo", "bar")
	structuredMetadata := labels.FromStrings("user", "bob")
	expectedLabelsResults := appendLabels(lbs, structuredMetadata)
	se, err := NewLineSampleExtractor(CountExtractor, []Stage{
		NewStringLabelFilter(labels.MustNewMatcher(labels.MatchEqual, "foo", "bar")),
		NewStringLabelFilter(labels.MustNewMatcher(labels.MatchEqual, "user", "bob")),
	}, nil, false, false)
	require.NoError(t, err)

	sse := se.ForStream(lbs)
	samples, ok := sse.Process(0, []byte(`foo`), structuredMetadata)
	require.True(t, ok)
	require.Len(t, samples, 1, "Expected exactly one sample")
	require.Equal(t, 1., samples[0].Value)
	assertLabelResult(t, expectedLabelsResults, samples[0].Labels)

	samples, ok = sse.ProcessString(0, `foo`, structuredMetadata)
	require.True(t, ok)
	require.Len(t, samples, 1, "Expected exactly one sample")
	require.Equal(t, 1., samples[0].Value)
	assertLabelResult(t, expectedLabelsResults, samples[0].Labels)

	// test duplicated structured metadata with stream labels
	expectedLabelsResults = appendLabel(lbs, "foo_extracted", "baz")
	expectedLabelsResults = appendLabels(expectedLabelsResults, structuredMetadata)
	samples, ok = sse.Process(0, []byte(`foo`), appendLabel(structuredMetadata, "foo", "baz"))
	require.True(t, ok)
	require.Len(t, samples, 1, "Expected exactly one sample")
	require.Equal(t, 1., samples[0].Value)
	assertLabelResult(t, expectedLabelsResults, samples[0].Labels)

	samples, ok = sse.ProcessString(0, `foo`, appendLabel(structuredMetadata, "foo", "baz"))
	require.True(t, ok)
	require.Len(t, samples, 1, "Expected exactly one sample")
	require.Equal(t, 1., samples[0].Value)
	assertLabelResult(t, expectedLabelsResults, samples[0].Labels)

	se, err = NewLineSampleExtractor(BytesExtractor, []Stage{
		NewStringLabelFilter(labels.MustNewMatcher(labels.MatchEqual, "foo", "bar")),
		NewStringLabelFilter(labels.MustNewMatcher(labels.MatchEqual, "user", "bob")),
		mustFilter(NewFilter("foo", LineMatchEqual)).ToStage(),
	}, []string{"foo"}, false, false)
	require.NoError(t, err)

	sse = se.ForStream(lbs)
	samples, ok = sse.Process(0, []byte(`foo`), structuredMetadata)
	require.True(t, ok)
	require.Len(t, samples, 1, "Expected exactly one sample")
	require.Equal(t, 3., samples[0].Value)
	assertLabelResult(t, labels.FromStrings("foo", "bar"), samples[0].Labels)

	sse = se.ForStream(lbs)
	_, ok = sse.Process(0, []byte(`nope`), labels.EmptyLabels())
	require.False(t, ok)
}

func appendLabel(l labels.Labels, name, value string) labels.Labels {
	b := labels.NewBuilder(l)
	b.Set(name, value)
	return b.Labels()
}

func TestFilteringSampleExtractor(t *testing.T) {
	se := NewFilteringSampleExtractor([]PipelineFilter{
		newPipelineFilter(2, 4, labels.FromStrings("foo", "bar", "bar", "baz"), labels.EmptyLabels(), "e"),
		newPipelineFilter(3, 5, labels.FromStrings("baz", "foo"), labels.EmptyLabels(), "e"),
		newPipelineFilter(3, 5, labels.FromStrings("foo", "baz"), labels.FromStrings("user", "bob"), "e"),
	}, newStubExtractor())

	tt := []struct {
		name               string
		ts                 int64
		line               string
		labels             labels.Labels
		structuredMetadata labels.Labels
		ok                 bool
	}{
		{"it is after the timerange", 6, "line", labels.FromStrings("baz", "foo"), labels.EmptyLabels(), true},
		{"it is before the timerange", 1, "line", labels.FromStrings("baz", "foo"), labels.EmptyLabels(), true},
		{"it doesn't match the filter", 3, "all good", labels.FromStrings("baz", "foo"), labels.EmptyLabels(), true},
		{"it doesn't match all the selectors", 3, "line", labels.FromStrings("foo", "bar"), labels.EmptyLabels(), true},
		{"it doesn't match any selectors", 3, "line", labels.FromStrings("beep", "boop"), labels.EmptyLabels(), true},
		{"it matches all selectors", 3, "line", labels.FromStrings("foo", "bar", "bar", "baz"), labels.EmptyLabels(), false},
		{"it doesn't match all structured metadata", 3, "line", labels.FromStrings("foo", "baz"), labels.FromStrings("user", "alice"), true},
		{"it matches all structured metadata", 3, "line", labels.FromStrings("foo", "baz"), labels.FromStrings("user", "bob"), false},
		{"it tries all the filters", 5, "line", labels.FromStrings("baz", "foo"), labels.EmptyLabels(), false},
	}

	for _, test := range tt {
		t.Run(test.name, func(t *testing.T) {
			_, ok := se.ForStream(test.labels).Process(test.ts, []byte(test.line), test.structuredMetadata)
			require.Equal(t, test.ok, ok)

			_, ok = se.ForStream(test.labels).ProcessString(test.ts, test.line, test.structuredMetadata)
			require.Equal(t, test.ok, ok)
		})
	}
}

func newStubExtractor() *stubExtractor {
	return &stubExtractor{
		sp: &stubStreamExtractor{},
	}
}

// A stub always returns the same data
type stubExtractor struct {
	sp *stubStreamExtractor
}

func (p *stubExtractor) ForStream(_ labels.Labels) StreamSampleExtractor {
	return p.sp
}

// A stub always returns the same data
type stubStreamExtractor struct{}

func (p *stubStreamExtractor) BaseLabels() LabelsResult {
	builder := NewBaseLabelsBuilder().ForLabels(labels.FromStrings("foo", "bar"), 0)
	return builder.LabelsResult()
}

func (p *stubStreamExtractor) Process(
	_ int64,
	_ []byte,
	structuredMetadata labels.Labels,
) ([]ExtractedSample, bool) {
	builder := NewBaseLabelsBuilder().ForLabels(labels.FromStrings("foo", "bar"), 0)
	builder.Add(StructuredMetadataLabel, structuredMetadata)
	result := []ExtractedSample{
		{Value: 1.0, Labels: builder.LabelsResult()},
	}
	return result, true
}

func (p *stubStreamExtractor) ProcessString(
	_ int64,
	_ string,
	structuredMetadata labels.Labels,
) ([]ExtractedSample, bool) {
	builder := NewBaseLabelsBuilder().ForLabels(labels.FromStrings("foo", "bar"), 0)
	builder.Add(StructuredMetadataLabel, structuredMetadata)
	result := []ExtractedSample{
		{Value: 1.0, Labels: builder.LabelsResult()},
	}
	return result, true
}

func (p *stubStreamExtractor) ReferencedStructuredMetadata() bool {
	return false
}
