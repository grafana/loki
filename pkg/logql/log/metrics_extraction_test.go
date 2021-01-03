package log

import (
	"sort"
	"testing"

	"github.com/prometheus/prometheus/pkg/labels"
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

			outval, outlbs, ok := tt.ex.ForStream(tt.in).Process([]byte(""))
			require.Equal(t, tt.wantOk, ok)
			require.Equal(t, tt.want, outval)
			require.Equal(t, tt.wantLbs, outlbs.Labels())

			outval, outlbs, ok = tt.ex.ForStream(tt.in).ProcessString("")
			require.Equal(t, tt.wantOk, ok)
			require.Equal(t, tt.want, outval)
			require.Equal(t, tt.wantLbs, outlbs.Labels())
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
	lbs := labels.Labels{
		{Name: "namespace", Value: "dev"},
		{Name: "cluster", Value: "us-central1"},
	}
	sort.Sort(lbs)
	sse := se.ForStream(lbs)
	f, l, ok := sse.Process([]byte(`foo`))
	require.True(t, ok)
	require.Equal(t, 1., f)
	assertLabelResult(t, lbs, l)

	f, l, ok = sse.ProcessString(`foo`)
	require.True(t, ok)
	require.Equal(t, 1., f)
	assertLabelResult(t, lbs, l)

	filter, err := NewFilter("foo", labels.MatchEqual)
	require.NoError(t, err)

	se, err = NewLineSampleExtractor(BytesExtractor, []Stage{filter.ToStage()}, []string{"namespace"}, false, false)
	require.NoError(t, err)
	sse = se.ForStream(lbs)
	f, l, ok = sse.Process([]byte(`foo`))
	require.True(t, ok)
	require.Equal(t, 3., f)
	assertLabelResult(t, labels.Labels{labels.Label{Name: "namespace", Value: "dev"}}, l)
	sse = se.ForStream(lbs)
	_, _, ok = sse.Process([]byte(`nope`))
	require.False(t, ok)
}
