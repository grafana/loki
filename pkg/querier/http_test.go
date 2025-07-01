package querier

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/mock"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/grafana/loki/v3/pkg/validation"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
)

func TestInstantQueryHandler(t *testing.T) {
	defaultLimits := defaultLimitsTestConfig()
	limits, err := validation.NewOverrides(defaultLimits, nil)
	require.NoError(t, err)

	t.Run("log selector expression not allowed for instant queries", func(t *testing.T) {
		api := NewQuerierAPI(mockQuerierConfig(), nil, limits, nil, nil, log.NewNopLogger())

		ctx := user.InjectOrgID(context.Background(), "user")
		req, err := http.NewRequestWithContext(ctx, "GET", `/api/v1/query`, nil)
		require.NoError(t, err)

		q := req.URL.Query()
		q.Add("query", `{app="loki"}`)
		req.URL.RawQuery = q.Encode()
		err = req.ParseForm()
		require.NoError(t, err)

		rr := httptest.NewRecorder()

		handler := NewQuerierHandler(api)
		httpHandler := NewQuerierHTTPHandler(handler)

		httpHandler.ServeHTTP(rr, req)
		require.Equal(t, http.StatusBadRequest, rr.Code)
		require.Equal(t, logqlmodel.ErrUnsupportedSyntaxForInstantQuery.Error(), rr.Body.String())
	})
}

type slowConnectionSimulator struct {
	sleepFor   time.Duration
	deadline   time.Duration
	didTimeout bool
}

func (s *slowConnectionSimulator) ServeHTTP(_ http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := ctx.Err(); err != nil {
		panic(fmt.Sprintf("context already errored: %s", err))
	}
	time.Sleep(s.sleepFor)

	select {
	case <-ctx.Done():
		switch ctx.Err() {
		case context.DeadlineExceeded:
			s.didTimeout = true
		case context.Canceled:
			panic("context already canceled")
		}
	case <-time.After(s.deadline):
	}
}

func TestQueryWrapperMiddleware(t *testing.T) {
	shortestTimeout := time.Millisecond * 5

	t.Run("request timeout is the shortest one", func(t *testing.T) {
		defaultLimits := defaultLimitsTestConfig()
		defaultLimits.QueryTimeout = model.Duration(time.Millisecond * 10)

		limits, err := validation.NewOverrides(defaultLimits, nil)
		require.NoError(t, err)

		// request timeout is 5ms but it sleeps for 100ms, so timeout injected in the request is expected.
		connSimulator := &slowConnectionSimulator{
			sleepFor: time.Millisecond * 100,
			deadline: shortestTimeout,
		}

		midl := WrapQuerySpanAndTimeout("mycall", limits).Wrap(connSimulator)

		req, err := http.NewRequest("GET", "/loki/api/v1/label", nil)
		ctx, cancelFunc := context.WithTimeout(user.InjectOrgID(req.Context(), "fake"), shortestTimeout)
		defer cancelFunc()
		req = req.WithContext(ctx)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		srv := http.HandlerFunc(midl.ServeHTTP)

		srv.ServeHTTP(rr, req)
		require.Equal(t, http.StatusOK, rr.Code)

		select {
		case <-ctx.Done():
			break
		case <-time.After(shortestTimeout):
			require.FailNow(t, "should have timed out before %s", shortestTimeout)
		default:
			require.FailNow(t, "timeout expected")
		}

		require.True(t, connSimulator.didTimeout)
	})

	t.Run("apply limits query timeout", func(t *testing.T) {
		defaultLimits := defaultLimitsTestConfig()
		defaultLimits.QueryTimeout = model.Duration(shortestTimeout)

		limits, err := validation.NewOverrides(defaultLimits, nil)
		require.NoError(t, err)

		connSimulator := &slowConnectionSimulator{
			sleepFor: time.Millisecond * 100,
			deadline: shortestTimeout,
		}

		midl := WrapQuerySpanAndTimeout("mycall", limits).Wrap(connSimulator)

		req, err := http.NewRequest("GET", "/loki/api/v1/label", nil)
		ctx, cancelFunc := context.WithTimeout(user.InjectOrgID(req.Context(), "fake"), time.Millisecond*100)
		defer cancelFunc()
		req = req.WithContext(ctx)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		srv := http.HandlerFunc(midl.ServeHTTP)

		srv.ServeHTTP(rr, req)
		require.Equal(t, http.StatusOK, rr.Code)

		select {
		case <-ctx.Done():
			break
		case <-time.After(shortestTimeout):
			require.FailNow(t, "should have timed out before %s", shortestTimeout)
		}

		require.True(t, connSimulator.didTimeout)
	})
}

func injectOrgID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, ctx, _ := user.ExtractOrgIDFromHTTPRequest(r)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func buildHandler(api *QuerierAPI) http.Handler {
	return injectOrgID(NewQuerierHTTPHandler(NewQuerierHandler(api)))
}

func TestSeriesHandler(t *testing.T) {
	t.Run("instant queries set a step of 0", func(t *testing.T) {
		ret := func() *logproto.SeriesResponse {
			return &logproto.SeriesResponse{
				Series: []logproto.SeriesIdentifier{
					{
						Labels: []logproto.SeriesIdentifier_LabelsEntry{
							{Key: "a", Value: "1"},
							{Key: "b", Value: "2"},
						},
					},
					{
						Labels: []logproto.SeriesIdentifier_LabelsEntry{
							{Key: "c", Value: "3"},
							{Key: "d", Value: "4"},
						},
					},
				},
			}
		}
		expected := `{"status":"success","data":[{"a":"1","b":"2"},{"c":"3","d":"4"}]}`

		q := newQuerierMock()
		q.On("Series", mock.Anything, mock.Anything).Return(ret, nil)
		api := setupAPI(t, q, false)
		handler := buildHandler(api)

		req := httptest.NewRequest(http.MethodGet, "/loki/api/v1/series"+
			"?start=0"+
			"&end=1"+
			"&step=42"+
			"&query=%7Bfoo%3D%22bar%22%7D", nil)
		req.Header.Set("X-Scope-OrgID", "test-org")
		res := makeRequest(t, handler, req)

		require.Equalf(t, 200, res.Code, "response was not HTTP OK: %s", res.Body.String())
		require.JSONEq(t, expected, res.Body.String())
	})

	t.Run("ignores __aggregated_metric__ and __patern__ series, when possible, unless explicitly requested", func(t *testing.T) {
		ret := func() *logproto.SeriesResponse {
			return &logproto.SeriesResponse{
				Series: []logproto.SeriesIdentifier{},
			}
		}

		q := newQuerierMock()
		q.On("Series", mock.Anything, mock.Anything).Return(ret, nil)
		api := setupAPI(t, q, true)
		handler := buildHandler(api)

		for _, tt := range []struct {
			match          string
			expectedGroups []string
		}{
			{
				// we can't add the negated __aggregated_metric__ matcher to an empty matcher set,
				// as that will produce an invalid query
				match:          "{}",
				expectedGroups: []string{},
			},
			{
				match:          `{foo="bar"}`,
				expectedGroups: []string{fmt.Sprintf(`{foo="bar", %s="", %s=""}`, constants.AggregatedMetricLabel, constants.PatternLabel)},
			},
			{
				match:          fmt.Sprintf(`{%s="foo-service"}`, constants.AggregatedMetricLabel),
				expectedGroups: []string{fmt.Sprintf(`{%s="foo-service", %s=""}`, constants.AggregatedMetricLabel, constants.PatternLabel)},
			},
			{
				match:          fmt.Sprintf(`{%s="foo-service"}`, constants.PatternLabel),
				expectedGroups: []string{fmt.Sprintf(`{%s="foo-service", %s=""}`, constants.PatternLabel, constants.AggregatedMetricLabel)},
			},
			{
				match:          fmt.Sprintf(`{%s="foo-service", %s="foo-service"}`, constants.AggregatedMetricLabel, constants.PatternLabel),
				expectedGroups: []string{fmt.Sprintf(`{%s="foo-service", %s="foo-service"}`, constants.AggregatedMetricLabel, constants.PatternLabel)},
			},
		} {
			req := httptest.NewRequest(http.MethodGet, "/loki/api/v1/series"+
				"?start=0"+
				"&end=1"+
				fmt.Sprintf("&match=%s", url.QueryEscape(tt.match)), nil)
			req.Header.Set("X-Scope-OrgID", "test-org")
			_ = makeRequest(t, handler, req)
			q.AssertCalled(t, "Series", mock.Anything, &logproto.SeriesRequest{
				Start:  time.Unix(0, 0).UTC(),
				End:    time.Unix(1, 0).UTC(),
				Groups: tt.expectedGroups,
			})
		}
	})
}

func TestVolumeHandler(t *testing.T) {
	ret := &logproto.VolumeResponse{
		Volumes: []logproto.Volume{
			{Name: `{foo="bar"}`, Volume: 38},
		},
	}

	t.Run("shared beavhior between range and instant queries", func(t *testing.T) {
		for _, tc := range []struct {
			mode string
			req  *logproto.VolumeRequest
		}{
			{mode: "instant", req: loghttp.NewVolumeInstantQueryWithDefaults(`{foo="bar"}`)},
			{mode: "range", req: loghttp.NewVolumeRangeQueryWithDefaults(`{foo="bar"}`)},
		} {
			t.Run(fmt.Sprintf("%s queries return label volumes from the querier", tc.mode), func(t *testing.T) {
				querier := newQuerierMock()
				querier.On("Volume", mock.Anything, mock.Anything).Return(ret, nil)
				api := setupAPI(t, querier, false)

				res, err := api.VolumeHandler(context.Background(), tc.req)
				require.NoError(t, err)

				calls := querier.GetMockedCallsByMethod("Volume")
				require.Len(t, calls, 1)

				request := calls[0].Arguments[1].(*logproto.VolumeRequest)
				require.Equal(t, `{foo="bar"}`, request.Matchers)
				require.Equal(t, "series", request.AggregateBy)

				require.Equal(t, ret, res)
			})

			t.Run(fmt.Sprintf("%s queries return nothing when a store doesn't support label volumes", tc.mode), func(t *testing.T) {
				querier := newQuerierMock()
				querier.On("Volume", mock.Anything, mock.Anything).Return(nil, nil)
				api := setupAPI(t, querier, false)

				res, err := api.VolumeHandler(context.Background(), tc.req)
				require.NoError(t, err)

				calls := querier.GetMockedCallsByMethod("Volume")
				require.Len(t, calls, 1)

				require.Empty(t, res.Volumes)
			})

			t.Run(fmt.Sprintf("%s queries return error when there's an error in the querier", tc.mode), func(t *testing.T) {
				err := errors.New("something bad")
				querier := newQuerierMock()
				querier.On("Volume", mock.Anything, mock.Anything).Return(nil, err)

				api := setupAPI(t, querier, false)

				_, err = api.VolumeHandler(context.Background(), tc.req)
				require.ErrorContains(t, err, "something bad")

				calls := querier.GetMockedCallsByMethod("Volume")
				require.Len(t, calls, 1)
			})
		}
	})
}

func TestLabelsHandler(t *testing.T) {
	t.Run("remove __aggregated_metric__ label from response when present", func(t *testing.T) {
		ret := &logproto.LabelResponse{
			Values: []string{
				constants.AggregatedMetricLabel,
				"foo",
				"bar",
			},
		}
		expected := `{"status":"success","data":["foo","bar"]}`

		q := newQuerierMock()
		q.On("Label", mock.Anything, mock.Anything).Return(ret, nil)
		api := setupAPI(t, q, true)
		handler := buildHandler(api)

		req := httptest.NewRequest(http.MethodGet, "/loki/api/v1/labels"+
			"?start=0"+
			"&end=1", nil)
		req.Header.Set("X-Scope-OrgID", "test-org")
		res := makeRequest(t, handler, req)

		require.Equalf(t, 200, res.Code, "response was not HTTP OK: %s", res.Body.String())
		require.JSONEq(t, expected, res.Body.String())
	})

	t.Run("remove __pattern__ label from response when present", func(t *testing.T) {
		ret := &logproto.LabelResponse{
			Values: []string{
				constants.PatternLabel,
				"foo",
				"bar",
			},
		}
		expected := `{"status":"success","data":["foo","bar"]}`

		q := newQuerierMock()
		q.On("Label", mock.Anything, mock.Anything).Return(ret, nil)
		api := setupAPI(t, q, true)
		handler := buildHandler(api)

		req := httptest.NewRequest(http.MethodGet, "/loki/api/v1/labels"+
			"?start=0"+
			"&end=1", nil)
		req.Header.Set("X-Scope-OrgID", "test-org")
		res := makeRequest(t, handler, req)

		require.Equalf(t, 200, res.Code, "response was not HTTP OK: %s", res.Body.String())
		require.JSONEq(t, expected, res.Body.String())
	})

	t.Run("remove both __aggregated_metric__ and __pattern__ labels from response when present", func(t *testing.T) {
		ret := &logproto.LabelResponse{
			Values: []string{
				constants.AggregatedMetricLabel,
				constants.PatternLabel,
				"foo",
				"bar",
			},
		}
		expected := `{"status":"success","data":["foo","bar"]}`

		q := newQuerierMock()
		q.On("Label", mock.Anything, mock.Anything).Return(ret, nil)
		api := setupAPI(t, q, true)
		handler := buildHandler(api)

		req := httptest.NewRequest(http.MethodGet, "/loki/api/v1/labels"+
			"?start=0"+
			"&end=1", nil)
		req.Header.Set("X-Scope-OrgID", "test-org")
		res := makeRequest(t, handler, req)

		require.Equalf(t, 200, res.Code, "response was not HTTP OK: %s", res.Body.String())
		require.JSONEq(t, expected, res.Body.String())
	})
}

func makeRequest(t *testing.T, handler http.Handler, req *http.Request) *httptest.ResponseRecorder {
	err := req.ParseForm()
	require.NoError(t, err)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

func setupAPI(t *testing.T, querier *querierMock, enableMetricAggregation bool) *QuerierAPI {
	defaultLimits := defaultLimitsTestConfig()
	defaultLimits.MetricAggregationEnabled = enableMetricAggregation
	limits, err := validation.NewOverrides(defaultLimits, nil)
	require.NoError(t, err)

	api := NewQuerierAPI(Config{}, querier, limits, nil, nil, log.NewNopLogger())
	return api
}

// Mock pattern responses
var mockPatternResponse = func(now time.Time, patterns []string) *logproto.QueryPatternsResponse {
	resp := &logproto.QueryPatternsResponse{
		Series: make([]*logproto.PatternSeries, 0),
	}
	for i, pattern := range patterns {
		resp.Series = append(resp.Series, &logproto.PatternSeries{
			Pattern: pattern,
			Level:   constants.LogLevelInfo,
			Samples: []*logproto.PatternSample{
				{
					Timestamp: model.Time(now.Unix() * 1000),
					Value:     int64((i + 1) * 100),
				},
			},
		})
	}
	return resp
}

func TestPatternsHandler(t *testing.T) {
	// Enable pattern persistence for most tests
	limitsConfig := defaultLimitsTestConfig()
	limitsConfig.PatternPersistenceEnabled = true
	limits, err := validation.NewOverrides(limitsConfig, nil)
	require.NoError(t, err)

	ctx := user.InjectOrgID(context.Background(), "test")
	now := time.Now()

	tests := []struct {
		name                          string
		start, end                    time.Time
		queryIngestersWithin          time.Duration
		queryPatternIngestersWithin   time.Duration
		ingesterQueryStoreMaxLookback time.Duration
		setupQuerier                  func() Querier
		patternsFromStore             []string
		expectedPatterns              []string
		expectedError                 error
		queryStoreOnly                bool
		queryIngesterOnly             bool
	}{
		{
			name:                 "query both ingester and store when time range overlaps and ingesterQueryStoreMaxLookback is not set",
			start:                now.Add(-2 * time.Hour),
			end:                  now,
			queryIngestersWithin: 3 * time.Hour,
			setupQuerier: func() Querier {
				q := &querierMock{}
				q.On("Patterns", mock.Anything, mock.MatchedBy(func(req *logproto.QueryPatternsRequest) bool {
					// Should query recent data only (within queryIngestersWithin)
					return req.Start.After(now.Add(-3*time.Hour)) && req.End.Equal(now)
				})).Return(mockPatternResponse(now, []string{"pattern1", "pattern2"}), nil)
				return q
			},
			patternsFromStore: []string{"pattern3", "pattern4"},
			expectedPatterns:  []string{"pattern1", "pattern2", "pattern3", "pattern4"},
		},
		{
			name:                 "query only store when time range is older than ingester lookback",
			start:                now.Add(-10 * time.Hour),
			end:                  now.Add(-4 * time.Hour),
			queryIngestersWithin: 3 * time.Hour,
			setupQuerier: func() Querier {
				// Should not be called
				q := &querierMock{}
				return q
			},
			patternsFromStore: []string{"old_pattern1", "old_pattern2"},
			expectedPatterns:  []string{"old_pattern1", "old_pattern2"},
		},
		{
			name:                          "query only ingester when time range is recent and ingesterQueryStoreMaxLookback is set",
			start:                         now.Add(-30 * time.Minute),
			end:                           now,
			queryIngestersWithin:          3 * time.Hour,
			ingesterQueryStoreMaxLookback: 1 * time.Hour,
			setupQuerier: func() Querier {
				q := &querierMock{}
				q.On("Patterns", mock.Anything, mock.MatchedBy(func(req *logproto.QueryPatternsRequest) bool {
					return req.Start.Equal(now.Add(-30*time.Minute)) && req.End.Equal(now)
				})).Return(mockPatternResponse(now, []string{"recent_pattern1", "recent_pattern2"}), nil)
				return q
			},
			patternsFromStore: []string{"old_pattern1", "old_pattern2"},
			expectedPatterns:  []string{"recent_pattern1", "recent_pattern2"},
		},
		{
			name:           "query store only when configured",
			start:          now.Add(-2 * time.Hour),
			end:            now,
			queryStoreOnly: true,
			setupQuerier: func() Querier {
				// Should not be called
				return newQuerierMock()
			},
			patternsFromStore: []string{"store_only_pattern"},
			expectedPatterns:  []string{"store_only_pattern"},
		},
		{
			name:                 "query ingester only when configured",
			start:                now.Add(-2 * time.Hour),
			end:                  now,
			queryIngesterOnly:    true,
			queryIngestersWithin: 3 * time.Hour,
			setupQuerier: func() Querier {
				q := &querierMock{}
				q.On("Patterns", mock.Anything, mock.Anything).Return(mockPatternResponse(now, []string{"ingester_only_pattern"}), nil)
				return q
			},
			patternsFromStore: []string{"store_only_pattern"},
			expectedPatterns:  []string{"ingester_only_pattern"},
		},
		{
			name:                 "returns empty response when no patterns found",
			start:                now.Add(-2 * time.Hour),
			end:                  now,
			queryIngestersWithin: 3 * time.Hour,
			setupQuerier: func() Querier {
				q := &querierMock{}
				q.On("Patterns", mock.Anything, mock.Anything).Return(&logproto.QueryPatternsResponse{Series: []*logproto.PatternSeries{}}, nil)
				return q
			},
		},
		{
			name:                        "query uses pattern-specific lookback when configured",
			start:                       now.Add(-2 * time.Hour),
			end:                         now.Add(-90 * time.Minute), // Query ends before pattern ingester lookback
			queryIngestersWithin:        3 * time.Hour,
			queryPatternIngestersWithin: 1 * time.Hour, // Pattern ingester only looks back 1 hour
			setupQuerier: func() Querier {
				// Should not be called since query is entirely outside pattern ingester lookback
				return newQuerierMock()
			},
			patternsFromStore: []string{"pattern_from_store"},
			expectedPatterns:  []string{"pattern_from_store"},
		},
		{
			name:                 "merges patterns from both sources correctly",
			start:                now.Add(-2 * time.Hour),
			end:                  now,
			queryIngestersWithin: 3 * time.Hour,
			setupQuerier: func() Querier {
				q := &querierMock{}
				q.On("Patterns", mock.Anything, mock.Anything).Return(&logproto.QueryPatternsResponse{
					Series: []*logproto.PatternSeries{
						{
							Pattern: "common_pattern",
							Samples: []*logproto.PatternSample{
								{Timestamp: model.Time(now.Add(-30*time.Minute).Unix() * 1000), Value: 100},
							},
						},
						{
							Pattern: "ingester_pattern",
							Samples: []*logproto.PatternSample{
								{Timestamp: model.Time(now.Add(-15*time.Minute).Unix() * 1000), Value: 200},
							},
						},
					},
				}, nil)
				return q
			},
			patternsFromStore: []string{"common_pattern", "store_pattern"},
			expectedPatterns:  []string{"common_pattern", "ingester_pattern", "store_pattern"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			conf := mockQuerierConfig()
			conf.QueryIngestersWithin = tc.queryIngestersWithin
			if tc.queryPatternIngestersWithin != 0 {
				conf.QueryPatternIngestersWithin = tc.queryPatternIngestersWithin
			} else {
				conf.QueryPatternIngestersWithin = tc.queryIngestersWithin
			}
			conf.IngesterQueryStoreMaxLookback = tc.ingesterQueryStoreMaxLookback
			conf.QueryStoreOnly = tc.queryStoreOnly
			conf.QueryIngesterOnly = tc.queryIngesterOnly

			querier := tc.setupQuerier()

			// Create a mock engine that returns the expected patterns from store
			engineMock := newMockEngineWithPatterns(tc.patternsFromStore)

			api := &QuerierAPI{
				cfg:      conf,
				querier:  querier,
				limits:   limits,
				engineV1: engineMock,
				logger:   log.NewNopLogger(),
			}

			req := &logproto.QueryPatternsRequest{
				Query: `{service_name="test-service"}`,
				Start: tc.start,
				End:   tc.end,
				Step:  time.Minute.Milliseconds(),
			}

			resp, err := api.PatternsHandler(ctx, req)

			if tc.expectedError != nil {
				require.Error(t, err)
				require.Equal(t, tc.expectedError, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, resp)

			// Collect all patterns from response
			foundPatterns := make(map[string]bool)
			for _, series := range resp.Series {
				foundPatterns[series.Pattern] = true
			}

			// Check expected patterns from ingester
			for _, pattern := range tc.expectedPatterns {
				require.True(t, foundPatterns[pattern], "Expected pattern %s, not found", pattern)
			}

			require.Equal(t, len(tc.expectedPatterns), len(resp.Series), "Unexpected number of patterns in response")

			// Verify mocks were called as expected (if it's a mock)
			if q, ok := querier.(*querierMock); ok {
				q.AssertExpectations(t)
			}
		})
	}
}

func TestPatternsHandlerDisabled(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), "test")
	now := time.Now()
	// Add test case for when pattern persistence is disabled
	tests := []struct {
		name                          string
		start, end                    time.Time
		queryIngestersWithin          time.Duration
		queryPatternIngestersWithin   time.Duration
		ingesterQueryStoreMaxLookback time.Duration
		setupQuerier                  func() Querier
		patternsFromStore             []string
		expectedPatterns              []string
		expectedError                 error
		queryStoreOnly                bool
		queryIngesterOnly             bool
	}{
		{
			name:                          "skip store query when pattern persistence is disabled",
			start:                         now.Add(-5 * time.Hour),
			end:                           now,
			queryIngestersWithin:          3 * time.Hour,
			ingesterQueryStoreMaxLookback: 1 * time.Hour,
			setupQuerier: func() Querier {
				q := &querierMock{}
				q.On("Patterns", mock.Anything, mock.MatchedBy(func(req *logproto.QueryPatternsRequest) bool {
					// Should query recent data only (within queryIngestersWithin)
					return req.Start.After(now.Add(-3*time.Hour)) && req.End.Equal(now)
				})).Return(mockPatternResponse(now, []string{"ingester_pattern1", "ingester_pattern2"}), nil)
				return q
			},
			patternsFromStore: []string{"store_pattern1", "store_pattern2"},       // These should NOT be returned
			expectedPatterns:  []string{"ingester_pattern1", "ingester_pattern2"}, // Only ingester patterns
		},
	}

	// Test cases for when pattern persistence is disabled
	for _, tc := range tests {
		t.Run(tc.name+" (pattern persistence disabled)", func(t *testing.T) {
			// Use limits without pattern persistence enabled
			limitsConfigDisabled := defaultLimitsTestConfig()
			limitsConfigDisabled.PatternPersistenceEnabled = false
			limitsDisabled, err := validation.NewOverrides(limitsConfigDisabled, nil)
			require.NoError(t, err)

			conf := mockQuerierConfig()
			conf.QueryIngestersWithin = tc.queryIngestersWithin
			if tc.queryPatternIngestersWithin != 0 {
				conf.QueryPatternIngestersWithin = tc.queryPatternIngestersWithin
			} else {
				conf.QueryPatternIngestersWithin = tc.queryIngestersWithin
			}
			conf.IngesterQueryStoreMaxLookback = tc.ingesterQueryStoreMaxLookback
			conf.QueryStoreOnly = tc.queryStoreOnly
			conf.QueryIngesterOnly = tc.queryIngesterOnly

			querier := tc.setupQuerier()

			// Create a mock engine that returns the expected patterns from store
			// This should NOT be called when pattern persistence is disabled
			engineMock := newMockEngineWithPatterns(tc.patternsFromStore)

			api := &QuerierAPI{
				cfg:      conf,
				querier:  querier,
				limits:   limitsDisabled, // Use the disabled limits
				engineV1: engineMock,
				logger:   log.NewNopLogger(),
			}

			req := &logproto.QueryPatternsRequest{
				Query: `{service_name="test-service"}`,
				Start: tc.start,
				End:   tc.end,
				Step:  time.Minute.Milliseconds(),
			}

			resp, err := api.PatternsHandler(ctx, req)

			if tc.expectedError != nil {
				require.Error(t, err)
				require.Equal(t, tc.expectedError, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, resp)

			// Collect all patterns from response
			foundPatterns := make(map[string]bool)
			for _, series := range resp.Series {
				foundPatterns[series.Pattern] = true
			}

			// Check expected patterns from ingester only
			for _, pattern := range tc.expectedPatterns {
				require.True(t, foundPatterns[pattern], "Expected pattern %s, not found", pattern)
			}

			// Ensure store patterns are NOT included
			for _, pattern := range tc.patternsFromStore {
				require.False(t, foundPatterns[pattern], "Store pattern %s should not be included when pattern persistence is disabled", pattern)
			}

			require.Equal(t, len(tc.expectedPatterns), len(resp.Series), "Unexpected number of patterns in response")

			// Verify mocks were called as expected (if it's a mock)
			if q, ok := querier.(*querierMock); ok {
				q.AssertExpectations(t)
			}
		})
	}
}
