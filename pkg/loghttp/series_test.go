package loghttp

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/require"
)

func Test_toLabelMatcher(t *testing.T) {
	for i, tc := range []struct {
		input    [][]*labels.Matcher
		expected []*logproto.LabelMatchers
	}{
		{
			[][]*labels.Matcher{
				{
					mustMatcher(labels.MatchEqual, "a", "1"),
				},
				{
					mustMatcher(labels.MatchEqual, "b", "2"),
					mustMatcher(labels.MatchRegexp, "c", "3"),
					mustMatcher(labels.MatchNotEqual, "d", "4"),
				},
			},
			[]*logproto.LabelMatchers{
				{
					Matchers: []*logproto.LabelMatcher{
						{
							Type:  logproto.EQUAL,
							Name:  "a",
							Value: "1",
						},
					},
				},
				{
					Matchers: []*logproto.LabelMatcher{
						{
							Type:  logproto.EQUAL,
							Name:  "b",
							Value: "2",
						},
						{
							Type:  logproto.REGEX_MATCH,
							Name:  "c",
							Value: "3",
						},
						{
							Type:  logproto.NOT_EQUAL,
							Name:  "d",
							Value: "4",
						},
					},
				},
			},
		},
	} {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			require.Equal(t, tc.expected, toLabelMatchers(tc.input))
		})
	}
}

func TestParseSeriesQuery(t *testing.T) {
	for _, tc := range []struct {
		desc      string
		input     *http.Request
		shouldErr bool
		expected  *logproto.SeriesRequest
	}{
		{
			"no match",
			withForm(url.Values{}),
			true,
			nil,
		},
		{
			"multiple matches",
			withForm(url.Values{
				"start": []string{"1000"},
				"end":   []string{"2000"},
				"match": []string{`{a="1"}`, `{b="2", c=~"3", d!="4"}`},
			}),
			false,
			mkSeriesRequest(t, "1000", "2000", []string{`{a="1"}`, `{b="2", c=~"3", d!="4"}`}),
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			out, err := ParseSeriesQuery(tc.input)
			if tc.shouldErr {
				require.Error(t, err)
			} else {
				require.Equal(t, tc.expected, out)
			}
		})
	}
}

func withForm(form url.Values) *http.Request {
	return &http.Request{Form: form}
}

func mkSeriesRequest(t *testing.T, from, to string, matches []string) *logproto.SeriesRequest {
	start, end, err := bounds(withForm(url.Values{
		"start": []string{from},
		"end":   []string{to},
	}))
	require.Nil(t, err)

	grps, err := match(matches)
	require.Nil(t, err)
	return &logproto.SeriesRequest{
		Start:  start,
		End:    end,
		Groups: toLabelMatchers(grps),
	}
}
