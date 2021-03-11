package marshal

import (
	"reflect"
	"testing"

	"github.com/grafana/loki/pkg/loghttp"
)

func TestNewLabelSet(t *testing.T) {

	tests := []struct {
		lbs     string
		want    loghttp.LabelSet
		wantErr bool
	}{
		{`{1="foo"}`, nil, true},
		{`{_1="foo"}`, loghttp.LabelSet{"_1": "foo"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.lbs, func(t *testing.T) {
			got, err := NewLabelSet(tt.lbs)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewLabelSet() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewLabelSet() = %v, want %v", got, tt.want)
			}
		})
	}
}
