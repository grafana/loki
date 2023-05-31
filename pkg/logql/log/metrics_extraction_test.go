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
		name    string
		ex      SampleExtractor
		in      labels.Labels
		want    float64
		wantLbs labels.Labels
		wantOk  bool
		line    string
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
			name: "dynamic label, convert duration",
			ex: mustSampleExtractor(LabelExtractorWithStages(
				"foo", ConvertDuration, []string{"bar", "buzz"}, false, false, []Stage{NewLogfmtParser()}, NoopStage,
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
				"foo", ConvertDuration, []string{"bar", "buzz"}, false, false, []Stage{NewLogfmtParser()}, NoopStage,
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
			outval, outlbs, ok := tt.ex.ForStream(tt.in).Process(0, []byte(tt.line))
			require.Equal(t, tt.wantOk, ok)
			require.Equal(t, tt.want, outval)
			require.Equal(t, tt.wantLbs, outlbs.Labels())

			outval, outlbs, ok = tt.ex.ForStream(tt.in).ProcessString(0, tt.line)
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
				LabelExtractorWithStages("subqueries", ConvertFloat, []string{"foo"}, false, false, []Stage{NewLogfmtParser(), NewStringLabelFilter(labels.MustNewMatcher(labels.MatchNotEqual, "subqueries", "0"))}, NoopStage),
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
				LabelExtractorWithStages("subqueries", ConvertFloat, []string{"foo"}, false, false, []Stage{NewLogfmtParser(), NewNumericLabelFilter(LabelFilterNotEqual, "subqueries", 0)}, NoopStage),
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

	stage := mustFilter(NewFilter("foo", labels.MatchEqual)).ToStage()
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

func TestFilteringSampleExtractor(t *testing.T) {
	se := NewFilteringSampleExtractor([]PipelineFilter{
		newPipelineFilter(2, 4, labels.FromStrings("foo", "bar", "bar", "baz"), "e"),
		newPipelineFilter(3, 5, labels.FromStrings("baz", "foo"), "e"),
	}, newStubExtractor())

	tt := []struct {
		name   string
		ts     int64
		line   string
		labels labels.Labels
		ok     bool
	}{
		{"it is after the timerange", 6, "line", labels.FromStrings("baz", "foo"), true},
		{"it is before the timerange", 1, "line", labels.FromStrings("baz", "foo"), true},
		{"it doesn't match the filter", 3, "all good", labels.FromStrings("baz", "foo"), true},
		{"it doesn't match all the selectors", 3, "line", labels.FromStrings("foo", "bar"), true},
		{"it doesn't match any selectors", 3, "line", labels.FromStrings("beep", "boop"), true},
		{"it matches all selectors", 3, "line", labels.FromStrings("foo", "bar", "bar", "baz"), false},
		{"it tries all the filters", 5, "line", labels.FromStrings("baz", "foo"), false},
	}

	for _, test := range tt {
		t.Run(test.name, func(t *testing.T) {
			_, _, ok := se.ForStream(test.labels).Process(test.ts, []byte(test.line))
			require.Equal(t, test.ok, ok)

			_, _, ok = se.ForStream(test.labels).ProcessString(test.ts, test.line)
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

func (p *stubExtractor) ForStream(labels labels.Labels) StreamSampleExtractor {
	return p.sp
}

// A stub always returns the same data
type stubStreamExtractor struct{}

func (p *stubStreamExtractor) BaseLabels() LabelsResult {
	return nil
}

func (p *stubStreamExtractor) Process(ts int64, line []byte) (float64, LabelsResult, bool) {
	return 0, nil, true
}

func (p *stubStreamExtractor) ProcessString(ts int64, line string) (float64, LabelsResult, bool) {
	return 0, nil, true
}
