package queryrangebase

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStepAlign(t *testing.T) {
	for i, tc := range []struct {
		input, expected *PrometheusRequest
	}{
		{
			input: &PrometheusRequest{
				Start: 0,
				End:   100,
				Step:  10,
			},
			expected: &PrometheusRequest{
				Start: 0,
				End:   100,
				Step:  10,
			},
		},

		{
			input: &PrometheusRequest{
				Start: 2,
				End:   102,
				Step:  10,
			},
			expected: &PrometheusRequest{
				Start: 0,
				End:   100,
				Step:  10,
			},
		},
	} {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			var result *PrometheusRequest
			s := stepAlign{
				next: HandlerFunc(func(_ context.Context, probabilistic bool, req Request) (Response, error) {
					result = req.(*PrometheusRequest)
					return nil, nil
				}),
			}
			_, err := s.Do(context.Background(), false, tc.input)
			require.NoError(t, err)
			require.Equal(t, tc.expected, result)
		})
	}
}
