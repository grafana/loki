package queryrange

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/querier/queryrange"
	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/user"
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

func Test_splitByInterval_Do(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), "1")
	split := splitByInterval{
		next: queryrange.HandlerFunc(func(_ context.Context, r queryrange.Request) (queryrange.Response, error) {
			return &LokiResponse{
				Status:    loghttp.QueryStatusSuccess,
				Direction: r.(*LokiRequest).Direction,
				Limit:     r.(*LokiRequest).Limit,
				Version:   uint32(loghttp.VersionV1),
				Data: LokiData{
					ResultType: loghttp.ResultTypeStream,
					Result: []logproto.Stream{
						{
							Labels: `{foo="bar", level="debug"}`,
							Entries: []logproto.Entry{

								{Timestamp: time.Unix(0, r.(*LokiRequest).StartTs.UnixNano()), Line: fmt.Sprintf("%d", r.(*LokiRequest).StartTs.UnixNano())},
							},
						},
					},
				},
			}, nil
		}),
		limits:   fakeLimits{},
		merger:   lokiCodec,
		interval: time.Hour,
	}

	tests := []struct {
		name string
		req  *LokiRequest
		want *LokiResponse
	}{
		{
			"backward",
			&LokiRequest{
				StartTs:   time.Unix(0, 0),
				EndTs:     time.Unix(0, (4 * time.Hour).Nanoseconds()),
				Query:     "",
				Limit:     1000,
				Step:      1,
				Direction: logproto.BACKWARD,
				Path:      "/api/prom/query_range",
			},
			&LokiResponse{
				Status:    loghttp.QueryStatusSuccess,
				Direction: logproto.BACKWARD,
				Limit:     1000,
				Version:   1,
				Data: LokiData{
					ResultType: loghttp.ResultTypeStream,
					Result: []logproto.Stream{
						{
							Labels: `{foo="bar", level="debug"}`,
							Entries: []logproto.Entry{
								{Timestamp: time.Unix(0, 3*time.Hour.Nanoseconds()), Line: fmt.Sprintf("%d", 3*time.Hour.Nanoseconds())},
								{Timestamp: time.Unix(0, 2*time.Hour.Nanoseconds()), Line: fmt.Sprintf("%d", 2*time.Hour.Nanoseconds())},
								{Timestamp: time.Unix(0, time.Hour.Nanoseconds()), Line: fmt.Sprintf("%d", time.Hour.Nanoseconds())},
								{Timestamp: time.Unix(0, 0), Line: fmt.Sprintf("%d", 0)},
							},
						},
					},
				},
			},
		},
		{
			"forward",
			&LokiRequest{
				StartTs:   time.Unix(0, 0),
				EndTs:     time.Unix(0, (4 * time.Hour).Nanoseconds()),
				Query:     "",
				Limit:     1000,
				Step:      1,
				Direction: logproto.FORWARD,
				Path:      "/api/prom/query_range",
			},
			&LokiResponse{
				Status:    loghttp.QueryStatusSuccess,
				Direction: logproto.FORWARD,
				Limit:     1000,
				Version:   1,
				Data: LokiData{
					ResultType: loghttp.ResultTypeStream,
					Result: []logproto.Stream{
						{
							Labels: `{foo="bar", level="debug"}`,
							Entries: []logproto.Entry{
								{Timestamp: time.Unix(0, 0), Line: fmt.Sprintf("%d", 0)},
								{Timestamp: time.Unix(0, time.Hour.Nanoseconds()), Line: fmt.Sprintf("%d", time.Hour.Nanoseconds())},
								{Timestamp: time.Unix(0, 2*time.Hour.Nanoseconds()), Line: fmt.Sprintf("%d", 2*time.Hour.Nanoseconds())},
								{Timestamp: time.Unix(0, 3*time.Hour.Nanoseconds()), Line: fmt.Sprintf("%d", 3*time.Hour.Nanoseconds())},
							},
						},
					},
				},
			},
		},
		{
			"forward limited",
			&LokiRequest{
				StartTs:   time.Unix(0, 0),
				EndTs:     time.Unix(0, (4 * time.Hour).Nanoseconds()),
				Query:     "",
				Limit:     2,
				Step:      1,
				Direction: logproto.FORWARD,
				Path:      "/api/prom/query_range",
			},
			&LokiResponse{
				Status:    loghttp.QueryStatusSuccess,
				Direction: logproto.FORWARD,
				Limit:     2,
				Version:   1,
				Data: LokiData{
					ResultType: loghttp.ResultTypeStream,
					Result: []logproto.Stream{
						{
							Labels: `{foo="bar", level="debug"}`,
							Entries: []logproto.Entry{
								{Timestamp: time.Unix(0, 0), Line: fmt.Sprintf("%d", 0)},
								{Timestamp: time.Unix(0, time.Hour.Nanoseconds()), Line: fmt.Sprintf("%d", time.Hour.Nanoseconds())},
							},
						},
					},
				},
			},
		},
		{
			"backward limited",
			&LokiRequest{
				StartTs:   time.Unix(0, 0),
				EndTs:     time.Unix(0, (4 * time.Hour).Nanoseconds()),
				Query:     "",
				Limit:     2,
				Step:      1,
				Direction: logproto.BACKWARD,
				Path:      "/api/prom/query_range",
			},
			&LokiResponse{
				Status:    loghttp.QueryStatusSuccess,
				Direction: logproto.BACKWARD,
				Limit:     2,
				Version:   1,
				Data: LokiData{
					ResultType: loghttp.ResultTypeStream,
					Result: []logproto.Stream{
						{
							Labels: `{foo="bar", level="debug"}`,
							Entries: []logproto.Entry{
								{Timestamp: time.Unix(0, 3*time.Hour.Nanoseconds()), Line: fmt.Sprintf("%d", 3*time.Hour.Nanoseconds())},
								{Timestamp: time.Unix(0, 2*time.Hour.Nanoseconds()), Line: fmt.Sprintf("%d", 2*time.Hour.Nanoseconds())},
							},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := split.Do(ctx, tt.req)
			require.NoError(t, err)
			require.Equal(t, tt.want, res)
		})
	}

}

func Test_ExitEarly(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), "1")

	var callCt int
	var mtx sync.Mutex

	split := splitByInterval{
		next: queryrange.HandlerFunc(func(_ context.Context, r queryrange.Request) (queryrange.Response, error) {
			time.Sleep(time.Millisecond) // artificial delay to minimize race condition exposure in test

			mtx.Lock()
			defer mtx.Unlock()
			callCt++

			return &LokiResponse{
				Status:    loghttp.QueryStatusSuccess,
				Direction: r.(*LokiRequest).Direction,
				Limit:     r.(*LokiRequest).Limit,
				Version:   uint32(loghttp.VersionV1),
				Data: LokiData{
					ResultType: loghttp.ResultTypeStream,
					Result: []logproto.Stream{
						{
							Labels: `{foo="bar", level="debug"}`,
							Entries: []logproto.Entry{

								{
									Timestamp: time.Unix(0, r.(*LokiRequest).StartTs.UnixNano()),
									Line:      fmt.Sprintf("%d", r.(*LokiRequest).StartTs.UnixNano()),
								},
							},
						},
					},
				},
			}, nil
		}),
		limits:   fakeLimits{},
		merger:   lokiCodec,
		interval: time.Hour,
	}

	req := &LokiRequest{
		StartTs:   time.Unix(0, 0),
		EndTs:     time.Unix(0, (4 * time.Hour).Nanoseconds()),
		Query:     "",
		Limit:     2,
		Step:      1,
		Direction: logproto.FORWARD,
		Path:      "/api/prom/query_range",
	}

	expected := &LokiResponse{
		Status:    loghttp.QueryStatusSuccess,
		Direction: logproto.FORWARD,
		Limit:     2,
		Version:   1,
		Data: LokiData{
			ResultType: loghttp.ResultTypeStream,
			Result: []logproto.Stream{
				{
					Labels: `{foo="bar", level="debug"}`,
					Entries: []logproto.Entry{
						{
							Timestamp: time.Unix(0, 0),
							Line:      fmt.Sprintf("%d", 0),
						},
						{
							Timestamp: time.Unix(0, time.Hour.Nanoseconds()),
							Line:      fmt.Sprintf("%d", time.Hour.Nanoseconds()),
						},
					},
				},
			},
		},
	}

	res, err := split.Do(ctx, req)

	require.Equal(t, int(req.Limit), callCt)
	require.NoError(t, err)
	require.Equal(t, expected, res)
}
