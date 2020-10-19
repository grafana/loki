package log

import (
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/require"
)

func TestBinary_Filter(t *testing.T) {

	tests := []struct {
		f   LabelFilterer
		lbs labels.Labels

		want    bool
		wantLbs labels.Labels
	}{
		{
			NewAndLabelFilter(NewNumericLabelFilter(LabelFilterEqual, "foo", 5), NewDurationLabelFilter(LabelFilterEqual, "bar", 1*time.Second)),
			labels.Labels{{Name: "foo", Value: "5"}, {Name: "bar", Value: "1s"}},
			true,
			labels.Labels{{Name: "foo", Value: "5"}, {Name: "bar", Value: "1s"}},
		},
		{
			NewAndLabelFilter(NewNumericLabelFilter(LabelFilterEqual, "foo", 5), NewBytesLabelFilter(LabelFilterEqual, "bar", 42)),
			labels.Labels{{Name: "foo", Value: "5"}, {Name: "bar", Value: "42B"}},
			true,
			labels.Labels{{Name: "foo", Value: "5"}, {Name: "bar", Value: "42B"}},
		},
		{
			NewAndLabelFilter(
				NewNumericLabelFilter(LabelFilterEqual, "foo", 5),
				NewDurationLabelFilter(LabelFilterEqual, "bar", 1*time.Second),
			),
			labels.Labels{{Name: "foo", Value: "6"}, {Name: "bar", Value: "1s"}},
			false,
			labels.Labels{{Name: "foo", Value: "6"}, {Name: "bar", Value: "1s"}},
		},
		{
			NewAndLabelFilter(
				NewNumericLabelFilter(LabelFilterEqual, "foo", 5),
				NewDurationLabelFilter(LabelFilterEqual, "bar", 1*time.Second),
			),
			labels.Labels{{Name: "foo", Value: "5"}, {Name: "bar", Value: "2s"}},
			false,
			labels.Labels{{Name: "foo", Value: "5"}, {Name: "bar", Value: "2s"}},
		},
		{
			NewAndLabelFilter(
				NewStringLabelFilter(labels.MustNewMatcher(labels.MatchEqual, "foo", "5")),
				NewDurationLabelFilter(LabelFilterEqual, "bar", 1*time.Second),
			),
			labels.Labels{{Name: "foo", Value: "5"}, {Name: "bar", Value: "1s"}},
			true,
			labels.Labels{{Name: "foo", Value: "5"}, {Name: "bar", Value: "1s"}},
		},
		{
			NewAndLabelFilter(
				NewStringLabelFilter(labels.MustNewMatcher(labels.MatchEqual, "foo", "5")),
				NewDurationLabelFilter(LabelFilterEqual, "bar", 1*time.Second),
			),
			labels.Labels{{Name: "foo", Value: "6"}, {Name: "bar", Value: "1s"}},
			false,
			labels.Labels{{Name: "foo", Value: "6"}, {Name: "bar", Value: "1s"}},
		},
		{
			NewAndLabelFilter(
				NewOrLabelFilter(
					NewDurationLabelFilter(LabelFilterGreaterThan, "duration", 1*time.Second),
					NewNumericLabelFilter(LabelFilterNotEqual, "status", 200),
				),
				NewStringLabelFilter(labels.MustNewMatcher(labels.MatchNotEqual, "method", "POST")),
			),
			labels.Labels{
				{Name: "duration", Value: "2s"},
				{Name: "status", Value: "200"},
				{Name: "method", Value: "GET"},
			},
			true,
			labels.Labels{
				{Name: "duration", Value: "2s"},
				{Name: "status", Value: "200"},
				{Name: "method", Value: "GET"},
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
			labels.Labels{
				{Name: "duration", Value: "2s"},
				{Name: "status", Value: "200"},
				{Name: "method", Value: "POST"},
			},
			false,
			labels.Labels{
				{Name: "duration", Value: "2s"},
				{Name: "status", Value: "200"},
				{Name: "method", Value: "POST"},
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
			labels.Labels{
				{Name: "duration", Value: "2s"},
				{Name: "status", Value: "500"},
				{Name: "method", Value: "POST"},
			},
			false,
			labels.Labels{
				{Name: "duration", Value: "2s"},
				{Name: "status", Value: "500"},
				{Name: "method", Value: "POST"},
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
			labels.Labels{
				{Name: "duration", Value: "2s"},
				{Name: "status", Value: "200"},
				{Name: "method", Value: "POST"},
			},
			false,
			labels.Labels{
				{Name: "duration", Value: "2s"},
				{Name: "status", Value: "200"},
				{Name: "method", Value: "POST"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.f.String(), func(t *testing.T) {
			sort.Sort(tt.lbs)
			b := NewLabelsBuilder()
			b.Reset(tt.lbs)
			_, got := tt.f.Process(nil, b)
			require.Equal(t, tt.want, got)
			sort.Sort(tt.wantLbs)
			require.Equal(t, tt.wantLbs, b.Labels())
		})
	}
}

func TestErrorFiltering(t *testing.T) {
	tests := []struct {
		f   LabelFilterer
		lbs labels.Labels
		err string

		want    bool
		wantLbs labels.Labels
	}{
		{
			NewStringLabelFilter(labels.MustNewMatcher(labels.MatchNotEqual, ErrorLabel, errJSON)),
			labels.Labels{
				{Name: "status", Value: "200"},
				{Name: "method", Value: "POST"},
			},
			errJSON,
			false,
			labels.Labels{
				{Name: ErrorLabel, Value: errJSON},
				{Name: "status", Value: "200"},
				{Name: "method", Value: "POST"},
			},
		},
		{

			NewStringLabelFilter(labels.MustNewMatcher(labels.MatchNotRegexp, ErrorLabel, ".+")),
			labels.Labels{
				{Name: "status", Value: "200"},
				{Name: "method", Value: "POST"},
			},
			"foo",
			false,
			labels.Labels{
				{Name: ErrorLabel, Value: "foo"},
				{Name: "status", Value: "200"},
				{Name: "method", Value: "POST"},
			},
		},
		{

			NewStringLabelFilter(labels.MustNewMatcher(labels.MatchNotRegexp, ErrorLabel, ".+")),
			labels.Labels{
				{Name: "status", Value: "200"},
				{Name: "method", Value: "POST"},
			},
			"",
			true,
			labels.Labels{
				{Name: "status", Value: "200"},
				{Name: "method", Value: "POST"},
			},
		},
		{

			NewStringLabelFilter(labels.MustNewMatcher(labels.MatchNotEqual, ErrorLabel, errJSON)),
			labels.Labels{
				{Name: "status", Value: "200"},
				{Name: "method", Value: "POST"},
			},
			"",
			true,
			labels.Labels{
				{Name: "status", Value: "200"},
				{Name: "method", Value: "POST"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.f.String(), func(t *testing.T) {
			sort.Sort(tt.lbs)
			b := NewLabelsBuilder()
			b.Reset(tt.lbs)
			b.SetErr(tt.err)
			_, got := tt.f.Process(nil, b)
			require.Equal(t, tt.want, got)
			sort.Sort(tt.wantLbs)
			require.Equal(t, tt.wantLbs, b.Labels())
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
