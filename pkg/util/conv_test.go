package util

import (
	"reflect"
	"testing"
	"time"

	"github.com/prometheus/common/model"
)

func TestRoundToMilliseconds(t *testing.T) {
	tests := []struct {
		name        string
		from        time.Time
		through     time.Time
		wantFrom    model.Time
		wantThrough model.Time
	}{
		{
			"0",
			time.Unix(0, 0),
			time.Unix(0, 1),
			model.Time(0),
			model.Time(1),
		},
		{
			"equal",
			time.Unix(0, time.Millisecond.Nanoseconds()),
			time.Unix(0, time.Millisecond.Nanoseconds()),
			model.Time(1),
			model.Time(1),
		},
		{
			"exact",
			time.Unix(0, time.Millisecond.Nanoseconds()),
			time.Unix(0, 2*time.Millisecond.Nanoseconds()),
			model.Time(1),
			model.Time(2),
		},
		{
			"rounding",
			time.Unix(0, time.Millisecond.Nanoseconds()+10),
			time.Unix(0, 2*time.Millisecond.Nanoseconds()+10),
			model.Time(1),
			model.Time(3),
		},
		{
			"rounding large number in nanoseconds",
			time.Unix(0, 1643958368442000064),
			time.Unix(0, 1643958368443000064),
			model.Time(1643958368442),
			model.Time(1643958368444),
		},
		{
			"already rounded large number in nanoseconds",
			time.Unix(0, 1643958368442000000),
			time.Unix(0, 1643958368443000000),
			model.Time(1643958368442),
			model.Time(1643958368443),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			from, through := RoundToMilliseconds(tt.from, tt.through)
			if !reflect.DeepEqual(from, tt.wantFrom) {
				t.Errorf("RoundToMilliseconds() from = %v, want %v", from, tt.wantFrom)
			}
			if !reflect.DeepEqual(through, tt.wantThrough) {
				t.Errorf("RoundToMilliseconds() through = %v, want %v", through, tt.wantThrough)
			}
		})
	}
}

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
