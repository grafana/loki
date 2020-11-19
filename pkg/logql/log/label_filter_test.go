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
			NewAndLabelFilter(NewNumericLabelFilter(LabelFilterEqual, "foo", 5), NewBytesLabelFilter(LabelFilterEqual, "bar", 42000)),
			labels.Labels{{Name: "foo", Value: "5"}, {Name: "bar", Value: "42kB"}},
			true,
			labels.Labels{{Name: "foo", Value: "5"}, {Name: "bar", Value: "42kB"}},
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
			b := NewBaseLabelsBuilder().ForLabels(tt.lbs, tt.lbs.Hash())
			b.Reset()
			_, got := tt.f.Process(nil, b)
			require.Equal(t, tt.want, got)
			sort.Sort(tt.wantLbs)
			require.Equal(t, tt.wantLbs, b.Labels())
		})
	}
}

func TestBytes_Filter(t *testing.T) {
	tests := []struct {
		expectedBytes uint64
		label         string

		want      bool
		wantLabel string
	}{
		{42, "42B", true, "42B"},
		{42 * 1000, "42kB", true, "42kB"},
		{42 * 1000 * 1000, "42MB", true, "42MB"},
		{42 * 1000 * 1000 * 1000, "42GB", true, "42GB"},
		{42 * 1000 * 1000 * 1000 * 1000, "42TB", true, "42TB"},
		{42 * 1000 * 1000 * 1000 * 1000 * 1000, "42PB", true, "42PB"},
		{42 * 1024, "42KiB", true, "42KiB"},
		{42 * 1024 * 1024, "42MiB", true, "42MiB"},
		{42 * 1024 * 1024 * 1024, "42GiB", true, "42GiB"},
		{42 * 1024 * 1024 * 1024 * 1024, "42TiB", true, "42TiB"},
		{42 * 1024 * 1024 * 1024 * 1024 * 1024, "42PiB", true, "42PiB"},
	}
	for _, tt := range tests {
		f := NewBytesLabelFilter(LabelFilterEqual, "bar", tt.expectedBytes)
		lbs := labels.Labels{{Name: "bar", Value: tt.label}}
		t.Run(f.String(), func(t *testing.T) {
			b := NewBaseLabelsBuilder().ForLabels(lbs, lbs.Hash())
			b.Reset()
			_, got := f.Process(nil, b)
			require.Equal(t, tt.want, got)
			wantLbs := labels.Labels{{Name: "bar", Value: tt.wantLabel}}
			require.Equal(t, wantLbs, b.Labels())
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
			b := NewBaseLabelsBuilder().ForLabels(tt.lbs, tt.lbs.Hash())
			b.Reset()
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
