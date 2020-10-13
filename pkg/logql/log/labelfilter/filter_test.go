package labelfilter

import (
	"testing"
	"time"

	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logql/log"
)

func TestBinary_Filter(t *testing.T) {

	tests := []struct {
		f       *Binary
		lbs     log.Labels
		want    bool
		wantLbs log.Labels
	}{
		{
			NewAnd(NewNumeric(FilterEqual, "foo", 5), NewDuration(FilterEqual, "bar", 1*time.Second)),
			log.Labels{"foo": "5", "bar": "1s"},
			true,
			log.Labels{"foo": "5", "bar": "1s"},
		},
		{
			NewAnd(NewNumeric(FilterEqual, "foo", 5), NewBytes(FilterEqual, "bar", 42)),
			log.Labels{"foo": "5", "bar": "42B"},
			true,
			log.Labels{"foo": "5", "bar": "42B"},
		},
		{
			NewAnd(
				NewNumeric(FilterEqual, "foo", 5),
				NewDuration(FilterEqual, "bar", 1*time.Second),
			),
			log.Labels{"foo": "6", "bar": "1s"},
			false,
			log.Labels{"foo": "6", "bar": "1s"},
		},
		{
			NewAnd(
				NewNumeric(FilterEqual, "foo", 5),
				NewDuration(FilterEqual, "bar", 1*time.Second),
			),
			log.Labels{"foo": "5", "bar": "2s"},
			false,
			log.Labels{"foo": "5", "bar": "2s"},
		},
		{
			NewAnd(
				NewString(labels.MustNewMatcher(labels.MatchEqual, "foo", "5")),
				NewDuration(FilterEqual, "bar", 1*time.Second),
			),
			log.Labels{"foo": "5", "bar": "1s"},
			true,
			log.Labels{"foo": "5", "bar": "1s"},
		},
		{
			NewAnd(
				NewString(labels.MustNewMatcher(labels.MatchEqual, "foo", "5")),
				NewDuration(FilterEqual, "bar", 1*time.Second),
			),
			log.Labels{"foo": "6", "bar": "1s"},
			false,
			log.Labels{"foo": "6", "bar": "1s"},
		},
		{
			NewAnd(
				NewOr(
					NewDuration(FilterGreaterThan, "duration", 1*time.Second),
					NewNumeric(FilterNotEqual, "status", 200),
				),
				NewString(labels.MustNewMatcher(labels.MatchNotEqual, "method", "POST")),
			),
			log.Labels{
				"duration": "2s",
				"status":   "200",
				"method":   "GET",
			},
			true,
			log.Labels{
				"duration": "2s",
				"status":   "200",
				"method":   "GET",
			},
		},
		{
			NewAnd(
				NewOr(
					NewDuration(FilterGreaterThan, "duration", 1*time.Second),
					NewNumeric(FilterNotEqual, "status", 200),
				),
				NewString(labels.MustNewMatcher(labels.MatchNotEqual, "method", "POST")),
			),
			log.Labels{
				"duration": "2s",
				"status":   "200",
				"method":   "POST",
			},
			false,
			log.Labels{
				"duration": "2s",
				"status":   "200",
				"method":   "POST",
			},
		},
		{
			NewAnd(
				NewOr(
					NewDuration(FilterGreaterThan, "duration", 1*time.Second),
					NewNumeric(FilterNotEqual, "status", 200),
				),
				NewString(labels.MustNewMatcher(labels.MatchNotEqual, "method", "POST")),
			),
			log.Labels{
				"duration": "2s",
				"status":   "500",
				"method":   "POST",
			},
			false,
			log.Labels{
				"duration": "2s",
				"status":   "500",
				"method":   "POST",
			},
		},
		{
			NewAnd(
				NewOr(
					NewDuration(FilterGreaterThan, "duration", 3*time.Second),
					NewNumeric(FilterNotEqual, "status", 200),
				),
				NewString(labels.MustNewMatcher(labels.MatchNotEqual, "method", "POST")),
			),
			log.Labels{
				"duration": "2s",
				"status":   "200",
				"method":   "POST",
			},
			false,
			log.Labels{
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
