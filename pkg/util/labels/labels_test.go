package labels

import (
	"reflect"
	"testing"

	"github.com/prometheus/common/model"
)

func TestModelLabelSetToMap(t *testing.T) {

	tests := []struct {
		name string
		m    model.LabelSet
		want map[string]string
	}{
		{
			"nil",
			nil,
			map[string]string{},
		},
		{
			"one",
			model.LabelSet{model.LabelName("foo"): model.LabelValue("bar")},
			map[string]string{"foo": "bar"},
		},
		{
			"two",
			model.LabelSet{
				model.LabelName("foo"):  model.LabelValue("bar"),
				model.LabelName("buzz"): model.LabelValue("fuzz"),
			},
			map[string]string{
				"foo":  "bar",
				"buzz": "fuzz",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ModelLabelSetToMap(tt.m); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ModelLabelSetToMap() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMapToModelLabelSet(t *testing.T) {
	tests := []struct {
		name string
		args map[string]string
		want model.LabelSet
	}{
		{"nil", nil, model.LabelSet{}},
		{
			"one",
			map[string]string{"foo": "bar"},
			model.LabelSet{model.LabelName("foo"): model.LabelValue("bar")},
		},
		{
			"two",
			map[string]string{
				"foo":  "bar",
				"buzz": "fuzz",
			},
			model.LabelSet{
				model.LabelName("foo"):  model.LabelValue("bar"),
				model.LabelName("buzz"): model.LabelValue("fuzz"),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MapToModelLabelSet(tt.args); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("MapToModelLabelSet() = %v, want %v", got, tt.want)
			}
		})
	}
}
