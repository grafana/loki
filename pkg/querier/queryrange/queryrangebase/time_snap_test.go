package queryrangebase

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTimeSnap(t *testing.T) {
	for i, tc := range []struct {
		input, expected *PrometheusRequest
	}{
		{
			input: &PrometheusRequest{
				Start: 0,
				End:   100,
			},
			expected: &PrometheusRequest{
				Start: 0,
				End:   100,
			},
		},

		{
			input: &PrometheusRequest{
				Start: 2,
				End:   102,
			},
			expected: &PrometheusRequest{
				Start: 0,
				End:   100,
			},
		},
	} {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			var result *PrometheusRequest
			s := timeSnap{
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
