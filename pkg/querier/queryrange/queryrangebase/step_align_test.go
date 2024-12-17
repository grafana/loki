package queryrangebase

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestStepAlign(t *testing.T) {
	for i, tc := range []struct {
		input, expected *PrometheusRequest
	}{
		{
			input: &PrometheusRequest{
				Start: time.UnixMilli(0),
				End:   time.UnixMilli(100),
				Step:  10,
			},
			expected: &PrometheusRequest{
				Start: time.UnixMilli(0),
				End:   time.UnixMilli(100),
				Step:  10,
			},
		},

		{
			input: &PrometheusRequest{
				Start: time.UnixMilli(2),
				End:   time.UnixMilli(102),
				Step:  10,
			},
			expected: &PrometheusRequest{
				Start: time.UnixMilli(0),
				End:   time.UnixMilli(100),
				Step:  10,
			},
		},
	} {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			var result *PrometheusRequest
			s := stepAlign{
				next: HandlerFunc(func(_ context.Context, req Request) (Response, error) {
					result = req.(*PrometheusRequest)
					return nil, nil
				}),
			}
			_, err := s.Do(context.Background(), tc.input)
			require.NoError(t, err)
			require.Equal(t, tc.expected, result)
		})
	}
}
