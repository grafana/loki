package log

import (
	"testing"
	"time"

	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/require"
)

func TestBinary_Filter(t *testing.T) {

	tests := []struct {
		f       *BinaryLabelFilter
		lbs     Labels
		want    bool
		wantLbs Labels
	}{
		{
			NewAndLabelFilter(NewNumericLabelFilter(LabelFilterEqual, "foo", 5), NewDurationLabelFilter(LabelFilterEqual, "bar", 1*time.Second)),
			Labels{"foo": "5", "bar": "1s"},
			true,
			Labels{"foo": "5", "bar": "1s"},
		},
		{
			NewAndLabelFilter(NewNumericLabelFilter(LabelFilterEqual, "foo", 5), NewBytesLabelFilter(LabelFilterEqual, "bar", 42)),
			Labels{"foo": "5", "bar": "42B"},
			true,
			Labels{"foo": "5", "bar": "42B"},
		},
		{
			NewAndLabelFilter(
				NewNumericLabelFilter(LabelFilterEqual, "foo", 5),
				NewDurationLabelFilter(LabelFilterEqual, "bar", 1*time.Second),
			),
			Labels{"foo": "6", "bar": "1s"},
			false,
			Labels{"foo": "6", "bar": "1s"},
		},
		{
			NewAndLabelFilter(
				NewNumericLabelFilter(LabelFilterEqual, "foo", 5),
				NewDurationLabelFilter(LabelFilterEqual, "bar", 1*time.Second),
			),
			Labels{"foo": "5", "bar": "2s"},
			false,
			Labels{"foo": "5", "bar": "2s"},
		},
		{
			NewAndLabelFilter(
				NewStringLabelFilter(labels.MustNewMatcher(labels.MatchEqual, "foo", "5")),
				NewDurationLabelFilter(LabelFilterEqual, "bar", 1*time.Second),
			),
			Labels{"foo": "5", "bar": "1s"},
			true,
			Labels{"foo": "5", "bar": "1s"},
		},
		{
			NewAndLabelFilter(
				NewStringLabelFilter(labels.MustNewMatcher(labels.MatchEqual, "foo", "5")),
				NewDurationLabelFilter(LabelFilterEqual, "bar", 1*time.Second),
			),
			Labels{"foo": "6", "bar": "1s"},
			false,
			Labels{"foo": "6", "bar": "1s"},
		},
		{
			NewAndLabelFilter(
				NewOrLabelFilter(
					NewDurationLabelFilter(LabelFilterGreaterThan, "duration", 1*time.Second),
					NewNumericLabelFilter(LabelFilterNotEqual, "status", 200),
				),
				NewStringLabelFilter(labels.MustNewMatcher(labels.MatchNotEqual, "method", "POST")),
			),
			Labels{
				"duration": "2s",
				"status":   "200",
				"method":   "GET",
			},
			true,
			Labels{
				"duration": "2s",
				"status":   "200",
				"method":   "GET",
			},
		},
		{
			NewAndLabelFilter(
				NewOrLabelFilter(
					NewDurationLabelFilter(LabelFilterGreaterThan, "duration", 1*time.Second),
					NewNumericLabelFilter(LabelFilterNotEqual, "status", 200),
				),
				NewStringLabelFilter(labels.MustNewMatcher(labels.MatchNotEqual, "method", "POST")),
			),
			Labels{
				"duration": "2s",
				"status":   "200",
				"method":   "POST",
			},
			false,
			Labels{
				"duration": "2s",
				"status":   "200",
				"method":   "POST",
			},
		},
		{
			NewAndLabelFilter(
				NewOrLabelFilter(
					NewDurationLabelFilter(LabelFilterGreaterThan, "duration", 1*time.Second),
					NewNumericLabelFilter(LabelFilterNotEqual, "status", 200),
				),
				NewStringLabelFilter(labels.MustNewMatcher(labels.MatchNotEqual, "method", "POST")),
			),
			Labels{
				"duration": "2s",
				"status":   "500",
				"method":   "POST",
			},
			false,
			Labels{
				"duration": "2s",
				"status":   "500",
				"method":   "POST",
			},
		},
		{
			NewAndLabelFilter(
				NewOrLabelFilter(
					NewDurationLabelFilter(LabelFilterGreaterThan, "duration", 3*time.Second),
					NewNumericLabelFilter(LabelFilterNotEqual, "status", 200),
				),
				NewStringLabelFilter(labels.MustNewMatcher(labels.MatchNotEqual, "method", "POST")),
			),
			Labels{
				"duration": "2s",
				"status":   "200",
				"method":   "POST",
			},
			false,
			Labels{
				"duration": "2s",
				"status":   "200",
				"method":   "POST",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.f.String(), func(t *testing.T) {
			_, got := tt.f.Process(nil, tt.lbs)
			require.Equal(t, tt.want, got)
			require.Equal(t, tt.wantLbs, tt.lbs)
		})
	}
}
