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
			outval, outlbs, ok := tt.ex.ForStream(tt.in).Process(0, []byte(tt.line), tt.structuredMetadata...)
			require.Equal(t, tt.wantOk, ok)
			require.Equal(t, tt.want, outval)
			require.Equal(t, tt.wantLbs, outlbs.Labels())

			outval, outlbs, ok = tt.ex.ForStream(tt.in).ProcessString(0, tt.line, tt.structuredMetadata...)
			require.Equal(t, tt.wantOk, ok)
			require.Equal(t, tt.want, outval)
			require.Equal(t, tt.wantLbs, outlbs.Labels())
		})
	}
}

func Test_Extract_ExpectedLabels(t *testing.T) {
	ex := mustSampleExtractor(LabelExtractorWithStages("duration", ConvertDuration, []string{"foo"}, false, false, []Stage{NewJSONParser()}, NoopStage))

	f, lbs, ok := ex.ForStream(labels.FromStrings("bar", "foo")).ProcessString(0, `{"duration":"20ms","foo":"json"}`)
	require.True(t, ok)
	require.Equal(t, (20 * time.Millisecond).Seconds(), f)
	require.Equal(t, labels.FromStrings("foo", "json"), lbs.Labels())

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
				v, lbs, ok := tc.extractor.ForStream(labels.FromStrings("bar", "foo")).ProcessString(0, line.logLine)
				skipped := !ok
				assert.Equal(t, line.skip, skipped, "line", line.logLine)
				if !skipped {
					assert.Equal(t, line.sample, v)

					// lbs shouldn't have __error__ = SampleExtractionError
					assert.Empty(t, lbs.Labels())
					return
				}

				// if line is skipped, `lbs` will be nil.
				assert.Nil(t, lbs, "line", line.logLine)
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
	f, l, ok := sse.Process(0, []byte(`foo`))
	require.True(t, ok)
	require.Equal(t, 1., f)
	assertLabelResult(t, lbs, l)

	f, l, ok = sse.ProcessString(0, `foo`)
	require.True(t, ok)
	require.Equal(t, 1., f)
	assertLabelResult(t, lbs, l)

	stage := mustFilter(NewFilter("foo", LineMatchEqual)).ToStage()
	se, err = NewLineSampleExtractor(BytesExtractor, []Stage{stage}, []string{"namespace"}, false, false)
	require.NoError(t, err)

	sse = se.ForStream(lbs)
	f, l, ok = sse.Process(0, []byte(`foo`))
	require.True(t, ok)
	require.Equal(t, 3., f)
	assertLabelResult(t, labels.FromStrings("namespace", "dev"), l)

	sse = se.ForStream(lbs)
	_, _, ok = sse.Process(0, []byte(`nope`))
	require.False(t, ok)
}

func TestNewLineSampleExtractorWithStructuredMetadata(t *testing.T) {
	lbs := labels.FromStrings("foo", "bar")
	structuredMetadata := labels.FromStrings("user", "bob")
	expectedLabelsResults := append(lbs, structuredMetadata...)
	se, err := NewLineSampleExtractor(CountExtractor, []Stage{
		NewStringLabelFilter(labels.MustNewMatcher(labels.MatchEqual, "foo", "bar")),
		NewStringLabelFilter(labels.MustNewMatcher(labels.MatchEqual, "user", "bob")),
	}, nil, false, false)
	require.NoError(t, err)

	sse := se.ForStream(lbs)
	f, l, ok := sse.Process(0, []byte(`foo`), structuredMetadata...)
	require.True(t, ok)
	require.Equal(t, 1., f)
	assertLabelResult(t, expectedLabelsResults, l)

	f, l, ok = sse.ProcessString(0, `foo`, structuredMetadata...)
	require.True(t, ok)
	require.Equal(t, 1., f)
	assertLabelResult(t, expectedLabelsResults, l)

	// test duplicated structured metadata with stream labels
	expectedLabelsResults = append(lbs, labels.Label{
		Name: "foo_extracted", Value: "baz",
	})
	expectedLabelsResults = append(expectedLabelsResults, structuredMetadata...)
	f, l, ok = sse.Process(0, []byte(`foo`), append(structuredMetadata, labels.Label{
		Name: "foo", Value: "baz",
	})...)
	require.True(t, ok)
	require.Equal(t, 1., f)
	assertLabelResult(t, expectedLabelsResults, l)

	f, l, ok = sse.ProcessString(0, `foo`, append(structuredMetadata, labels.Label{
		Name: "foo", Value: "baz",
	})...)
	require.True(t, ok)
	require.Equal(t, 1., f)
	assertLabelResult(t, expectedLabelsResults, l)

	se, err = NewLineSampleExtractor(BytesExtractor, []Stage{
		NewStringLabelFilter(labels.MustNewMatcher(labels.MatchEqual, "foo", "bar")),
		NewStringLabelFilter(labels.MustNewMatcher(labels.MatchEqual, "user", "bob")),
		mustFilter(NewFilter("foo", LineMatchEqual)).ToStage(),
	}, []string{"foo"}, false, false)
	require.NoError(t, err)

	sse = se.ForStream(lbs)
	f, l, ok = sse.Process(0, []byte(`foo`), structuredMetadata...)
	require.True(t, ok)
	require.Equal(t, 3., f)
	assertLabelResult(t, labels.FromStrings("foo", "bar"), l)

	sse = se.ForStream(lbs)
	_, _, ok = sse.Process(0, []byte(`nope`))
	require.False(t, ok)
}

func TestFilteringSampleExtractor(t *testing.T) {
	se := NewFilteringSampleExtractor([]PipelineFilter{
		newPipelineFilter(2, 4, labels.FromStrings("foo", "bar", "bar", "baz"), nil, "e"),
		newPipelineFilter(3, 5, labels.FromStrings("baz", "foo"), nil, "e"),
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
		{"it is after the timerange", 6, "line", labels.FromStrings("baz", "foo"), nil, true},
		{"it is before the timerange", 1, "line", labels.FromStrings("baz", "foo"), nil, true},
		{"it doesn't match the filter", 3, "all good", labels.FromStrings("baz", "foo"), nil, true},
		{"it doesn't match all the selectors", 3, "line", labels.FromStrings("foo", "bar"), nil, true},
		{"it doesn't match any selectors", 3, "line", labels.FromStrings("beep", "boop"), nil, true},
		{"it matches all selectors", 3, "line", labels.FromStrings("foo", "bar", "bar", "baz"), nil, false},
		{"it doesn't match all structured metadata", 3, "line", labels.FromStrings("foo", "baz"), labels.FromStrings("user", "alice"), true},
		{"it matches all structured metadata", 3, "line", labels.FromStrings("foo", "baz"), labels.FromStrings("user", "bob"), false},
		{"it tries all the filters", 5, "line", labels.FromStrings("baz", "foo"), nil, false},
	}

	for _, test := range tt {
		t.Run(test.name, func(t *testing.T) {
			_, _, ok := se.ForStream(test.labels).Process(test.ts, []byte(test.line), test.structuredMetadata...)
			require.Equal(t, test.ok, ok)

			_, _, ok = se.ForStream(test.labels).ProcessString(test.ts, test.line, test.structuredMetadata...)
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
	structuredMetadata ...labels.Label,
) (float64, LabelsResult, bool) {
	builder := NewBaseLabelsBuilder().ForLabels(labels.FromStrings("foo", "bar"), 0)
	builder.Add(StructuredMetadataLabel, structuredMetadata...)
	return 1.0, builder.LabelsResult(), true
}

func (p *stubStreamExtractor) ProcessString(
	_ int64,
	_ string,
	structuredMetadata ...labels.Label,
) (float64, LabelsResult, bool) {
	builder := NewBaseLabelsBuilder().ForLabels(labels.FromStrings("foo", "bar"), 0)
	builder.Add(StructuredMetadataLabel, structuredMetadata...)
	return 1.0, builder.LabelsResult(), true
}

func (p *stubStreamExtractor) ReferencedStructuredMetadata() bool {
	return false
}

func TestVariantsStreamSampleExtractorWrapper(t *testing.T) {
	tests := []struct {
		name               string
		index              int
		input              string
		labels             labels.Labels
		structuredMetadata labels.Labels
		want               float64
		wantLbs            labels.Labels
		wantBaseLbs        labels.Labels
	}{
		{
			name:        "extraction with variant 0",
			index:       0,
			input:       "test line",
			labels:      labels.FromStrings("foo", "bar"),
			want:        1.0,
			wantLbs:     labels.FromStrings("foo", "bar", "__variant__", "0"),
			wantBaseLbs: labels.FromStrings("foo", "bar", "__variant__", "0"),
		},
		{
			name:        "extraction with variant 1",
			index:       1,
			input:       "test line",
			labels:      labels.FromStrings("foo", "bar"),
			want:        1.0,
			wantLbs:     labels.FromStrings("foo", "bar", "__variant__", "1"),
			wantBaseLbs: labels.FromStrings("foo", "bar", "__variant__", "1"),
		},
		{
			name:               "with structured metadata",
			index:              2,
			input:              "test line",
			labels:             labels.FromStrings("foo", "bar"),
			structuredMetadata: labels.FromStrings("meta", "data"),
			want:               1.0,
			wantLbs: labels.FromStrings(
				"foo",
				"bar",
				"__variant__",
				"2",
				"meta",
				"data",
			),
			wantBaseLbs: labels.FromStrings("foo", "bar", "__variant__", "2"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create base extractor that always returns 1.0 and the input labels
			baseExtractor := &stubStreamExtractor{}
			wrapped := NewVariantsStreamSampleExtractorWrapper(tt.index, baseExtractor)

			// Test Process
			val, lbs, ok := wrapped.Process(0, []byte(tt.input), tt.structuredMetadata...)
			require.Equal(t, true, ok)
			require.Equal(t, tt.want, val)
			require.Equal(t, tt.wantLbs, lbs.Labels())

			// Test ProcessString
			val, lbs, ok = wrapped.ProcessString(0, tt.input, tt.structuredMetadata...)
			require.Equal(t, true, ok)
			require.Equal(t, tt.want, val)
			require.Equal(t, tt.wantLbs, lbs.Labels())

			// Test BaseLabels
			baseLbs := wrapped.BaseLabels()
			require.Equal(t, tt.wantBaseLbs, baseLbs.Labels())
		})
	}
}
