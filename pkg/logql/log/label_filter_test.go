package log

import (
	"reflect"
	"testing"
	"time"

	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/require"
)

func TestBinary_Filter(t *testing.T) {

	tests := []struct {
		f       LabelFilterer
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
		{

			NewStringLabelFilter(labels.MustNewMatcher(labels.MatchNotEqual, ErrorLabel, errJSON)),
			Labels{
				ErrorLabel: errJSON,
				"status":   "200",
				"method":   "POST",
			},
			false,
			Labels{
				ErrorLabel: errJSON,
				"status":   "200",
				"method":   "POST",
			},
		},
		{

			NewStringLabelFilter(labels.MustNewMatcher(labels.MatchNotRegexp, ErrorLabel, ".+")),
			Labels{
				ErrorLabel: "foo",
				"status":   "200",
				"method":   "POST",
			},
			false,
			Labels{
				ErrorLabel: "foo",
				"status":   "200",
				"method":   "POST",
			},
		},
		{

			NewStringLabelFilter(labels.MustNewMatcher(labels.MatchNotRegexp, ErrorLabel, ".+")),
			Labels{
				"status": "200",
				"method": "POST",
			},
			true,
			Labels{
				"status": "200",
				"method": "POST",
			},
		},
		{

			NewStringLabelFilter(labels.MustNewMatcher(labels.MatchNotEqual, ErrorLabel, errJSON)),
			Labels{
				"status": "200",
				"method": "POST",
			},
			true,
			Labels{
				"status": "200",
				"method": "POST",
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

func TestReduceAndLabelFilter(t *testing.T) {
	tests := []struct {
		name    string
		filters []LabelFilterer
		want    LabelFilterer
	}{
		{"empty", nil, NoopLabelFilter},
		{"1", []LabelFilterer{NewBytesLabelFilter(LabelFilterEqual, "foo", 5)}, NewBytesLabelFilter(LabelFilterEqual, "foo", 5)},
		{"2",
			[]LabelFilterer{
				NewBytesLabelFilter(LabelFilterEqual, "foo", 5),
				NewBytesLabelFilter(LabelFilterGreaterThanOrEqual, "bar", 6),
			},
			NewAndLabelFilter(NewBytesLabelFilter(LabelFilterEqual, "foo", 5), NewBytesLabelFilter(LabelFilterGreaterThanOrEqual, "bar", 6)),
		},
		{"3",
			[]LabelFilterer{
				NewBytesLabelFilter(LabelFilterEqual, "foo", 5),
				NewBytesLabelFilter(LabelFilterGreaterThanOrEqual, "bar", 6),
				NewStringLabelFilter(labels.MustNewMatcher(labels.MatchEqual, "buzz", "bla")),
			},
			NewAndLabelFilter(
				NewAndLabelFilter(
					NewBytesLabelFilter(LabelFilterEqual, "foo", 5),
					NewBytesLabelFilter(LabelFilterGreaterThanOrEqual, "bar", 6),
				),
				NewStringLabelFilter(labels.MustNewMatcher(labels.MatchEqual, "buzz", "bla")),
			),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ReduceAndLabelFilter(tt.filters); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ReduceAndLabelFilter() = %v, want %v", got, tt.want)
			}
		})
	}
}
