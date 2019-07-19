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
