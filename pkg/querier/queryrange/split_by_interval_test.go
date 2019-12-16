package queryrange

import (
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/querier/queryrange"
	"github.com/stretchr/testify/require"
)

func Test_splitQuery(t *testing.T) {

	tests := []struct {
		name     string
		req      *LokiRequest
		interval time.Duration
		want     []queryrange.Request
	}{
		{
			"smaller request than interval",
			&LokiRequest{
				StartTs: time.Date(2019, 12, 9, 12, 0, 0, 1, time.UTC),
				EndTs:   time.Date(2019, 12, 9, 12, 30, 0, 0, time.UTC),
			},
			time.Hour,
			[]queryrange.Request{
				&LokiRequest{
					StartTs: time.Date(2019, 12, 9, 12, 0, 0, 1, time.UTC),
					EndTs:   time.Date(2019, 12, 9, 12, 30, 0, 0, time.UTC),
				},
			},
		},
		{
			"exactly 1 interval",
			&LokiRequest{
				StartTs: time.Date(2019, 12, 9, 12, 1, 0, 0, time.UTC),
				EndTs:   time.Date(2019, 12, 9, 13, 1, 0, 0, time.UTC),
			},
			time.Hour,
			[]queryrange.Request{
				&LokiRequest{
					StartTs: time.Date(2019, 12, 9, 12, 1, 0, 0, time.UTC),
					EndTs:   time.Date(2019, 12, 9, 13, 1, 0, 0, time.UTC),
				},
			},
		},
		{
			"2 intervals",
			&LokiRequest{
				StartTs: time.Date(2019, 12, 9, 12, 0, 0, 1, time.UTC),
				EndTs:   time.Date(2019, 12, 9, 13, 0, 0, 2, time.UTC),
			},
			time.Hour,
			[]queryrange.Request{
				&LokiRequest{
					StartTs: time.Date(2019, 12, 9, 12, 0, 0, 1, time.UTC),
					EndTs:   time.Date(2019, 12, 9, 13, 0, 0, 1, time.UTC),
				},
				&LokiRequest{
					StartTs: time.Date(2019, 12, 9, 13, 0, 0, 1, time.UTC),
					EndTs:   time.Date(2019, 12, 9, 13, 0, 0, 2, time.UTC),
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, splitByTime(tt.req, tt.interval))
		})
	}
}
