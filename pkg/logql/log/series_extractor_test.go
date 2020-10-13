package logql

import (
	"sort"
	"testing"

	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/require"
)

func Test_labelSampleExtractor_Extract(t *testing.T) {
	tests := []struct {
		name    string
		ex      *labelSampleExtractor
		in      labels.Labels
		want    float64
		wantLbs labels.Labels
		wantOk  bool
	}{
		{
			"convert float",
			newLabelSampleExtractor("foo", "", nil, nil),
			labels.Labels{labels.Label{Name: "foo", Value: "15.0"}},
			15,
			labels.Labels{},
			true,
		},
		{
			"convert float without",
			newLabelSampleExtractor("foo",
				"",
				nil,
				&grouping{without: true, groups: []string{"bar", "buzz"}},
			),
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
			newLabelSampleExtractor("foo",
				"",
				nil,
				&grouping{without: false, groups: []string{"bar", "buzz"}},
			),
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
			newLabelSampleExtractor("foo",
				OpConvDuration,
				nil,
				&grouping{without: false, groups: []string{"bar", "buzz"}},
			),
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
			"convert duration_seconds with",
			newLabelSampleExtractor("foo",
				OpConvDurationSeconds,
				nil,
				&grouping{without: false, groups: []string{"bar", "buzz"}},
			),
			labels.Labels{
				{Name: "foo", Value: "250ms"},
				{Name: "bar", Value: "foo"},
				{Name: "buzz", Value: "blip"},
				{Name: "namespace", Value: "dev"},
			},
			0.25,
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
			ok, outval, outlbs := tt.ex.Extract([]byte(""), tt.in)
			require.Equal(t, tt.wantOk, ok)
			require.Equal(t, tt.want, outval)
			require.Equal(t, tt.wantLbs, outlbs)
		})
	}
}
