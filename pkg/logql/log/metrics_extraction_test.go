package log

import (
	"sort"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
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
	}{
		{
			"convert float",
			mustSampleExtractor(LabelExtractorWithStages(
				"foo", ConvertFloat, nil, false, false, nil, NoopStage,
			)),
			labels.Labels{labels.Label{Name: "foo", Value: "15.0"}},
			15,
			labels.Labels{},
			true,
		},
		{
			"convert float as vector with no grouping",
			mustSampleExtractor(LabelExtractorWithStages(
				"foo", ConvertFloat, nil, false, true, nil, NoopStage,
			)),
			labels.Labels{labels.Label{Name: "foo", Value: "15.0"}, labels.Label{Name: "bar", Value: "buzz"}},
			15,
			labels.Labels{},
			true,
		},
		{
			"convert float without",
			mustSampleExtractor(LabelExtractorWithStages(
				"foo", ConvertFloat, []string{"bar", "buzz"}, true, false, nil, NoopStage,
			)),
			labels.Labels{
				{Name: "foo", Value: "10"},
				{Name: "bar", Value: "foo"},
				{Name: "buzz", Value: "blip"},
				{Name: "namespace", Value: "dev"},
			},
			10,
			labels.Labels{
				{Name: "namespace", Value: "dev"},
			},
			true,
		},
		{
			"convert float with",
			mustSampleExtractor(LabelExtractorWithStages(
				"foo", ConvertFloat, []string{"bar", "buzz"}, false, false, nil, NoopStage,
			)),
			labels.Labels{
				{Name: "foo", Value: "0.6"},
				{Name: "bar", Value: "foo"},
				{Name: "buzz", Value: "blip"},
				{Name: "namespace", Value: "dev"},
			},
			0.6,
			labels.Labels{
				{Name: "bar", Value: "foo"},
				{Name: "buzz", Value: "blip"},
			},
			true,
		},
		{
			"convert duration with",
			mustSampleExtractor(LabelExtractorWithStages(
				"foo", ConvertDuration, []string{"bar", "buzz"}, false, false, nil, NoopStage,
			)),
			labels.Labels{
				{Name: "foo", Value: "500ms"},
				{Name: "bar", Value: "foo"},
				{Name: "buzz", Value: "blip"},
				{Name: "namespace", Value: "dev"},
			},
			0.5,
			labels.Labels{
				{Name: "bar", Value: "foo"},
				{Name: "buzz", Value: "blip"},
			},
			true,
		},
		{
			"convert bytes",
			mustSampleExtractor(LabelExtractorWithStages(
				"foo", ConvertBytes, []string{"bar", "buzz"}, false, false, nil, NoopStage,
			)),
			labels.Labels{
				{Name: "foo", Value: "13 MiB"},
				{Name: "bar", Value: "foo"},
				{Name: "buzz", Value: "blip"},
				{Name: "namespace", Value: "dev"},
			},
			13 * 1024 * 1024,
			labels.Labels{
				{Name: "bar", Value: "foo"},
				{Name: "buzz", Value: "blip"},
			},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sort.Sort(tt.in)

			outval, outlbs, ok := tt.ex.ForStream(tt.in).Process(0, []byte(""))
			require.Equal(t, tt.wantOk, ok)
			require.Equal(t, tt.want, outval)
			require.Equal(t, tt.wantLbs, outlbs.Labels())

			outval, outlbs, ok = tt.ex.ForStream(tt.in).ProcessString(0, "")
			require.Equal(t, tt.wantOk, ok)
			require.Equal(t, tt.want, outval)
			require.Equal(t, tt.wantLbs, outlbs.Labels())
		})
	}
}

func Test_Extract_ExpectedLabels(t *testing.T) {
	ex := mustSampleExtractor(LabelExtractorWithStages("duration", ConvertDuration, []string{"foo"}, false, false, []Stage{NewJSONParser()}, NoopStage))

	f, lbs, ok := ex.ForStream(labels.Labels{{Name: "bar", Value: "foo"}}).ProcessString(0, `{"duration":"20ms","foo":"json"}`)
	require.True(t, ok)
	require.Equal(t, (20 * time.Millisecond).Seconds(), f)
	require.Equal(t, labels.Labels{{Name: "foo", Value: "json"}}, lbs.Labels())
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

	lbs := labels.Labels{
		{Name: "namespace", Value: "dev"},
		{Name: "cluster", Value: "us-central1"},
	}
	sort.Sort(lbs)

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
	assertLabelResult(t, labels.Labels{labels.Label{Name: "namespace", Value: "dev"}}, l)

	sse = se.ForStream(lbs)
	_, _, ok = sse.Process(0, []byte(`nope`))
	require.False(t, ok)
}

func TestFilteringSampleExtractor(t *testing.T) {
	se := NewFilteringSampleExtractor([]PipelineFilter{
		newPipelineFilter(2, 4, labels.Labels{{Name: "foo", Value: "bar"}, {Name: "bar", Value: "baz"}}, "e"),
		newPipelineFilter(3, 5, labels.Labels{{Name: "baz", Value: "foo"}}, "e"),
	}, newStubExtractor())

	tt := []struct {
		name   string
		ts     int64
		line   string
		labels labels.Labels
		ok     bool
	}{
		{"it is before the timerange", 6, "line", labels.Labels{{Name: "baz", Value: "foo"}}, true},
		{"it is before the timerange", 1, "line", labels.Labels{{Name: "baz", Value: "foo"}}, true},
		{"it doesn't match the filter", 3, "all good", labels.Labels{{Name: "baz", Value: "foo"}}, true},
		{"it doesn't match all the selectors", 3, "line", labels.Labels{{Name: "foo", Value: "bar"}}, true},
		{"it doesn't match any selectors", 3, "line", labels.Labels{{Name: "beep", Value: "boop"}}, true},
		{"it matches all selectors", 3, "line", labels.Labels{{Name: "foo", Value: "bar"}, {Name: "bar", Value: "baz"}}, false},
		{"it tries all the filters", 5, "line", labels.Labels{{Name: "baz", Value: "foo"}}, false},
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
