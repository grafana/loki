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
			outval, outlbs, ok := tt.ex.Process([]byte(""), tt.in)
			require.Equal(t, tt.wantOk, ok)
			require.Equal(t, tt.want, outval)
			require.Equal(t, tt.wantLbs, outlbs)
		})
	}
}

func mustSampleExtractor(ex SampleExtractor, err error) SampleExtractor {
	if err != nil {
		panic(err)
	}
	return ex
}
