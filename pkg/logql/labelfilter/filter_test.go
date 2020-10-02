package labelfilter

import (
	"testing"
	"time"

	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/require"
)

func TestBinary_Filter(t *testing.T) {

	tests := []struct {
		f       *Binary
		lbs     labels.Labels
		want    bool
		wantErr bool
	}{
		{
			NewAnd(NewNumeric(FilterEqual, "foo", 5), NewDuration(FilterEqual, "bar", 1*time.Second)),
			labels.Labels{labels.Label{Name: "foo", Value: "5"}, labels.Label{Name: "bar", Value: "1s"}},
			true,
			false,
		},
		{
			NewAnd(
				NewNumeric(FilterEqual, "foo", 5),
				NewDuration(FilterEqual, "bar", 1*time.Second),
			),
			labels.Labels{labels.Label{Name: "foo", Value: "6"}, labels.Label{Name: "bar", Value: "1s"}},
			false,
			false,
		},
		{
			NewAnd(
				NewNumeric(FilterEqual, "foo", 5),
				NewDuration(FilterEqual, "bar", 1*time.Second),
			),
			labels.Labels{labels.Label{Name: "foo", Value: "5"}, labels.Label{Name: "bar", Value: "2s"}},
			false,
			false,
		},
		{
			NewAnd(
				NewString(labels.MustNewMatcher(labels.MatchEqual, "foo", "5")),
				NewDuration(FilterEqual, "bar", 1*time.Second),
			),
			labels.Labels{labels.Label{Name: "foo", Value: "5"}, labels.Label{Name: "bar", Value: "1s"}},
			true,
			false,
		},
		{
			NewAnd(
				NewString(labels.MustNewMatcher(labels.MatchEqual, "foo", "5")),
				NewDuration(FilterEqual, "bar", 1*time.Second),
			),
			labels.Labels{labels.Label{Name: "foo", Value: "6"}, labels.Label{Name: "bar", Value: "1s"}},
			false,
			false,
		},
		{
			NewAnd(
				NewOr(
					NewDuration(FilterGreaterThan, "duration", 1*time.Second),
					NewNumeric(FilterNotEqual, "status", 200),
				),
				NewString(labels.MustNewMatcher(labels.MatchNotEqual, "method", "POST")),
			),
			labels.Labels{
				{Name: "duration", Value: "2s"},
				{Name: "status", Value: "200"},
				{Name: "method", Value: "GET"},
			},
			true,
			false,
		},
		{
			NewAnd(
				NewOr(
					NewDuration(FilterGreaterThan, "duration", 1*time.Second),
					NewNumeric(FilterNotEqual, "status", 200),
				),
				NewString(labels.MustNewMatcher(labels.MatchNotEqual, "method", "POST")),
			),
			labels.Labels{
				{Name: "duration", Value: "2s"},
				{Name: "status", Value: "200"},
				{Name: "method", Value: "POST"},
			},
			false,
			false,
		},
		{
			NewAnd(
				NewOr(
					NewDuration(FilterGreaterThan, "duration", 1*time.Second),
					NewNumeric(FilterNotEqual, "status", 200),
				),
				NewString(labels.MustNewMatcher(labels.MatchNotEqual, "method", "POST")),
			),
			labels.Labels{
				{Name: "duration", Value: "2s"},
				{Name: "status", Value: "500"},
				{Name: "method", Value: "POST"},
			},
			false,
			false,
		},
		{
			NewAnd(
				NewOr(
					NewDuration(FilterGreaterThan, "duration", 3*time.Second),
					NewNumeric(FilterNotEqual, "status", 200),
				),
				NewString(labels.MustNewMatcher(labels.MatchNotEqual, "method", "POST")),
			),
			labels.Labels{
				{Name: "duration", Value: "2s"},
				{Name: "status", Value: "200"},
				{Name: "method", Value: "POST"},
			},
			false,
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.f.String(), func(t *testing.T) {
			got, err := tt.f.Filter(tt.lbs)
			if (err != nil) != tt.wantErr {
				t.Errorf("Binary.Filter() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			require.Equal(t, got, tt.want, tt.lbs)
		})
	}
}
