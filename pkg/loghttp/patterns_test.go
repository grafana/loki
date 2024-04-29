package loghttp

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
)

func TestParsePatternsQuery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		path    string
		want    *logproto.QueryPatternsRequest
		wantErr bool
	}{
		{
			name: "should correctly parse valid params",
			path: "/loki/api/v1/patterns?query={}&start=100000000000&end=3600000000000&step=5s",
			want: &logproto.QueryPatternsRequest{
				Query: "{}",
				Start: time.Unix(100, 0),
				End:   time.Unix(3600, 0),
				Step:  (5 * time.Second).Milliseconds(),
			},
		},
		{
			name: "should default empty step param to sensible step for the range",
			path: "/loki/api/v1/patterns?query={}&start=100000000000&end=3600000000000",
			want: &logproto.QueryPatternsRequest{
				Query: "{}",
				Start: time.Unix(100, 0),
				End:   time.Unix(3600, 0),
				Step:  (14 * time.Second).Milliseconds(),
			},
		},
		{
			name: "should default start to zero for empty start param",
			path: "/loki/api/v1/patterns?query={}&end=3600000000000",
			want: &logproto.QueryPatternsRequest{
				Query: "{}",
				Start: time.Unix(0, 0),
				End:   time.Unix(3600, 0),
				Step:  (14 * time.Second).Milliseconds(),
			},
		},
		{
			name: "should accept step with no units as seconds",
			path: "/loki/api/v1/patterns?query={}&start=100000000000&end=3600000000000&step=10",
			want: &logproto.QueryPatternsRequest{
				Query: "{}",
				Start: time.Unix(100, 0),
				End:   time.Unix(3600, 0),
				Step:  (10 * time.Second).Milliseconds(),
			},
		},
		{
			name: "should accept step as string duration in seconds",
			path: "/loki/api/v1/patterns?query={}&start=100000000000&end=3600000000000&step=15s",
			want: &logproto.QueryPatternsRequest{
				Query: "{}",
				Start: time.Unix(100, 0),
				End:   time.Unix(3600, 0),
				Step:  (15 * time.Second).Milliseconds(),
			},
		},
		{
			name: "should correctly parse long duration for step",
			path: "/loki/api/v1/patterns?query={}&start=100000000000&end=3600000000000&step=10h",
			want: &logproto.QueryPatternsRequest{
				Query: "{}",
				Start: time.Unix(100, 0),
				End:   time.Unix(3600, 0),
				Step:  (10 * time.Hour).Milliseconds(),
			},
		},
		{
			name:    "should reject negative step value",
			path:    "/loki/api/v1/patterns?query={}&start=100000000000&end=3600000000000&step=-5s",
			want:    nil,
			wantErr: true,
		},
		{
			name:    "should reject very small step for big range",
			path:    "/loki/api/v1/patterns?query={}&start=100000000000&end=3600000000000&step=50ms",
			want:    nil,
			wantErr: true,
		},
		{
			name: "should accept very small step for small range",
			path: "/loki/api/v1/patterns?query={}&start=100000000000&end=110000000000&step=50ms",
			want: &logproto.QueryPatternsRequest{
				Query: "{}",
				Start: time.Unix(100, 0),
				End:   time.Unix(110, 0),
				Step:  (50 * time.Millisecond).Milliseconds(),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, tt.path, nil)
			require.NoError(t, err)
			err = req.ParseForm()
			require.NoError(t, err)

			got, err := ParsePatternsQuery(req)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.Equalf(t, tt.want, got, "Incorrect response from input path: %s", tt.path)
		})
	}
}
