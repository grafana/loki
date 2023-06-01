package log

import (
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logqlmodel"
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
		{
			NewDurationLabelFilter(LabelFilterGreaterThan, "duration", 3*time.Second),
			labels.Labels{
				{Name: "duration", Value: "2weeeeee"},
			},
			true,
			labels.Labels{
				{Name: "duration", Value: "2weeeeee"},
				{Name: "__error__", Value: "LabelFilterErr"},
				{Name: "__error_details__", Value: "time: unknown unit \"weeeeee\" in duration \"2weeeeee\""},
			},
		},
		{
			NewBytesLabelFilter(LabelFilterGreaterThan, "bytes", 100),
			labels.Labels{
				{Name: "bytes", Value: "2qb"},
			},
			true,
			labels.Labels{
				{Name: "bytes", Value: "2qb"},
				{Name: "__error__", Value: "LabelFilterErr"},
				{Name: "__error_details__", Value: "unhandled size name: qb"},
			},
		},
		{
			NewNumericLabelFilter(LabelFilterGreaterThan, "number", 100),
			labels.Labels{
				{Name: "number", Value: "not_a_number"},
			},
			true,
			labels.Labels{
				{Name: "number", Value: "not_a_number"},
				{Name: "__error__", Value: "LabelFilterErr"},
				{Name: "__error_details__", Value: "strconv.ParseFloat: parsing \"not_a_number\": invalid syntax"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.f.String(), func(t *testing.T) {
			sort.Sort(tt.lbs)
			b := NewBaseLabelsBuilder().ForLabels(tt.lbs, tt.lbs.Hash())
			b.Reset()
			_, got := tt.f.Process(0, nil, b)
			require.Equal(t, tt.want, got)
			sort.Sort(tt.wantLbs)
			require.Equal(t, tt.wantLbs, b.LabelsResult().Labels())
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
			_, got := f.Process(0, nil, b)
			require.Equal(t, tt.want, got)
			wantLbs := labels.Labels{{Name: "bar", Value: tt.wantLabel}}
			require.Equal(t, wantLbs, b.LabelsResult().Labels())
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
			NewStringLabelFilter(labels.MustNewMatcher(labels.MatchNotEqual, logqlmodel.ErrorLabel, errJSON)),
			labels.Labels{
				{Name: "status", Value: "200"},
				{Name: "method", Value: "POST"},
			},
			errJSON,
			false,
			labels.Labels{
				{Name: logqlmodel.ErrorLabel, Value: errJSON},
				{Name: "status", Value: "200"},
				{Name: "method", Value: "POST"},
			},
		},
		{
			NewStringLabelFilter(labels.MustNewMatcher(labels.MatchNotRegexp, logqlmodel.ErrorLabel, ".+")),
			labels.Labels{
				{Name: "status", Value: "200"},
				{Name: "method", Value: "POST"},
			},
			"foo",
			false,
			labels.Labels{
				{Name: logqlmodel.ErrorLabel, Value: "foo"},
				{Name: "status", Value: "200"},
				{Name: "method", Value: "POST"},
			},
		},
		{
			NewStringLabelFilter(labels.MustNewMatcher(labels.MatchNotRegexp, logqlmodel.ErrorLabel, ".+")),
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
			NewStringLabelFilter(labels.MustNewMatcher(labels.MatchNotEqual, logqlmodel.ErrorLabel, errJSON)),
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
			_, got := tt.f.Process(0, nil, b)
			require.Equal(t, tt.want, got)
			sort.Sort(tt.wantLbs)
			require.Equal(t, tt.wantLbs, b.LabelsResult().Labels())
		})
	}
}

func TestReduceAndLabelFilter(t *testing.T) {
	tests := []struct {
		name    string
		filters []LabelFilterer
		want    LabelFilterer
	}{
		{"empty", nil, &NoopLabelFilter{}},
		{"1", []LabelFilterer{NewBytesLabelFilter(LabelFilterEqual, "foo", 5)}, NewBytesLabelFilter(LabelFilterEqual, "foo", 5)},
		{
			"2",
			[]LabelFilterer{
				NewBytesLabelFilter(LabelFilterEqual, "foo", 5),
				NewBytesLabelFilter(LabelFilterGreaterThanOrEqual, "bar", 6),
			},
			NewAndLabelFilter(NewBytesLabelFilter(LabelFilterEqual, "foo", 5), NewBytesLabelFilter(LabelFilterGreaterThanOrEqual, "bar", 6)),
		},
		{
			"3",
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

func TestStringLabelFilter(t *testing.T) {
	// NOTE: https://github.com/grafana/loki/issues/6713

	tests := []struct {
		name        string
		filter      LabelFilterer
		labels      labels.Labels
		shouldMatch bool
	}{
		{
			name:   `logfmt|subqueries!="0" (without label)`,
			filter: NewStringLabelFilter(labels.MustNewMatcher(labels.MatchNotEqual, "subqueries", "0")),
			labels: labels.Labels{{Name: "msg", Value: "hello"}}, // no label `subqueries`
			// without `subqueries` label, the value is assumed to be empty `subqueries=""` is matches the label filter `subqueries!="0"`.
			shouldMatch: true,
		},
		{
			name:        `logfmt|subqueries!="0" (with label)`,
			filter:      NewStringLabelFilter(labels.MustNewMatcher(labels.MatchNotEqual, "subqueries", "0")),
			labels:      labels.Labels{{Name: "msg", Value: "hello"}, {Name: "subqueries", Value: "2"}}, // label `subqueries` exist
			shouldMatch: true,
		},
		{
			name:   `logfmt|subqueries!~"0" (without label)`,
			filter: NewStringLabelFilter(labels.MustNewMatcher(labels.MatchNotRegexp, "subqueries", "0")),
			labels: labels.Labels{{Name: "msg", Value: "hello"}}, // no label `subqueries`
			// without `subqueries` label, the value is assumed to be empty `subqueries=""` is matches the label filter `subqueries!="0"`.
			shouldMatch: true,
		},
		{
			name:        `logfmt|subqueries!~"0" (with label)`,
			filter:      NewStringLabelFilter(labels.MustNewMatcher(labels.MatchNotRegexp, "subqueries", "0")),
			labels:      labels.Labels{{Name: "msg", Value: "hello"}, {Name: "subqueries", Value: "2"}}, // label `subqueries` exist
			shouldMatch: true,
		},
		{
			name:        `logfmt|subqueries="0" (without label)`,
			filter:      NewStringLabelFilter(labels.MustNewMatcher(labels.MatchEqual, "subqueries", "")),
			labels:      labels.Labels{{Name: "msg", Value: "hello"}}, // no label `subqueries`
			shouldMatch: true,
		},
		{
			name:        `logfmt|subqueries="0" (with label)`,
			filter:      NewStringLabelFilter(labels.MustNewMatcher(labels.MatchEqual, "subqueries", "")),
			labels:      labels.Labels{{Name: "msg", Value: "hello"}, {Name: "subqueries", Value: ""}}, // label `subqueries` exist
			shouldMatch: true,
		},
		{
			name:        `logfmt|subqueries=~"0" (without label)`,
			filter:      NewStringLabelFilter(labels.MustNewMatcher(labels.MatchRegexp, "subqueries", "")),
			labels:      labels.Labels{{Name: "msg", Value: "hello"}}, // no label `subqueries`
			shouldMatch: true,
		},
		{
			name:        `logfmt|subqueries=~"0" (with label)`,
			filter:      NewStringLabelFilter(labels.MustNewMatcher(labels.MatchRegexp, "subqueries", "")),
			labels:      labels.Labels{{Name: "msg", Value: "hello"}, {Name: "subqueries", Value: ""}}, // label `subqueries` exist
			shouldMatch: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, ok := tc.filter.Process(0, []byte("sample log line"), NewBaseLabelsBuilder().ForLabels(tc.labels, tc.labels.Hash()))
			assert.Equal(t, tc.shouldMatch, ok)
		})
	}
}

var result bool

func BenchmarkLineLabelFilters(b *testing.B) {
	line := []byte("line")
	fixture := strings.Join([]string{
		"foo", "foobar", "bar", "foobuzz", "buzz", "f", "  ", "fba", "foofoofoo", "b", "foob", "bfoo", "FoO",
		"foo, 世界", allunicode(), "fooÏbar",
	}, ",")
	lbl := NewBaseLabelsBuilder().ForLabels(labels.Labels{
		{Name: "foo", Value: fixture},
	}, 0)

	for _, test := range []struct {
		re string
	}{
		// regex we intend to support.
		{"foo"},
		{"(foo)"},
		{"(foo|ba)"},
		{"(foo|ba|ar)"},
		{"(foo|(ba|ar))"},
		{"foo.*"},
		{".*foo.*"},
		{"(.*)(foo).*"},
		{"(foo.*|.*ba)"},
		{"(foo.*|.*bar.*)"},
		{".*foo.*|bar"},
		{".*foo|bar"},
		{"(?:.*foo.*|bar)"},
		{"(?P<foo>.*foo.*|bar)"},
		{".*foo.*|bar|buzz"},
		{".*foo.*|bar|uzz"},
		{"foo|bar|b|buzz|zz"},
		{"f|foo|foobar"},
		{"f.*|foobar.*|.*buzz"},
		{"((f.*)|foobar.*)|.*buzz"},
		{".*"},
		{".*|.*"},
		{".*||||"},
		{""},
		{"(?i)foo"},
		{"(?i)界"},
		{"(?i)ïB"},
		{"(?:)foo|fatal|exception"},
		{"(?i)foo|fatal|exception"},
		{"(?i)f|foo|foobar"},
		{"(?i)f|fatal|e.*"},
		{"(?i).*foo.*"},
	} {
		b.Run(test.re, func(b *testing.B) {
			matcher := labels.MustNewMatcher(labels.MatchRegexp, "foo", test.re)
			f := NewStringLabelFilter(matcher)
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				_, result = f.Process(0, line, lbl)
			}
		})
	}
}
