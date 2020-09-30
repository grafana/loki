package logql

import (
	"reflect"
	"testing"

	"github.com/prometheus/prometheus/pkg/labels"
)

func Test_labelSampleExtractor_Extract(t *testing.T) {
	tests := []struct {
		name    string
		ex      *labelSampleExtractor
		in      labels.Labels
		want    float64
		wantLbs labels.Labels
	}{
		{
			"convert float",
			newLabelSampleExtractor("foo"),
			labels.Labels{labels.Label{Name: "foo", Value: "15.0"}},
			15,
			labels.Labels{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			outval, outlbs := tt.ex.Extract([]byte(""), tt.in)
			if outval != tt.want {
				t.Errorf("labelSampleExtractor.Extract() val = %v, want %v", outval, tt.want)
			}
			if !reflect.DeepEqual(outlbs, tt.wantLbs) {
				t.Errorf("labelSampleExtractor.Extract() lbs = %v, want %v", outlbs, tt.wantLbs)
			}
		})
	}
}
