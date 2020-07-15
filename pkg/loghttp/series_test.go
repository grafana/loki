package loghttp

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logproto"
)

func TestParseSeriesQuery(t *testing.T) {
	for _, tc := range []struct {
		desc      string
		input     *http.Request
		shouldErr bool
		expected  *logproto.SeriesRequest
	}{
		{
			"no match",
			withForm(url.Values{
				"start": []string{"1000"},
				"end":   []string{"2000"},
			}),
			false,
			mkSeriesRequest(t, "1000", "2000", []string{}),
		},
		{
			"empty matcher",
			withForm(url.Values{
				"start": []string{"1000"},
				"end":   []string{"2000"},
				"match": []string{"{}"},
			}),
			false,
			mkSeriesRequest(t, "1000", "2000", []string{}),
		},
		{
			"empty matcher with whitespace",
			withForm(url.Values{
				"start": []string{"1000"},
				"end":   []string{"2000"},
				"match": []string{" { }"},
			}),
			false,
			mkSeriesRequest(t, "1000", "2000", []string{}),
		},
		{
			"malformed",
			withForm(url.Values{
				"match": []string{`{a="}`},
			}),
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
		{
			"mixes match encodings",
			withForm(url.Values{
				"start":   []string{"1000"},
				"end":     []string{"2000"},
				"match":   []string{`{a="1"}`},
				"match[]": []string{`{b="2"}`},
			}),
			false,
			mkSeriesRequest(t, "1000", "2000", []string{`{a="1"}`, `{b="2"}`}),
		},
		{
			"dedupes match encodings",
			withForm(url.Values{
				"start":   []string{"1000"},
				"end":     []string{"2000"},
				"match":   []string{`{a="1"}`, `{b="2"}`},
				"match[]": []string{`{b="2"}`, `{c="3"}`},
			}),
			false,
			mkSeriesRequest(t, "1000", "2000", []string{`{a="1"}`, `{b="2"}`, `{c="3"}`}),
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			out, err := ParseSeriesQuery(tc.input)
			if tc.shouldErr {
				require.Error(t, err)
			} else {
				require.Nil(t, err)
				require.Equal(t, tc.expected, out)
			}
		})
	}
}

func withForm(form url.Values) *http.Request {
	return &http.Request{Form: form}
}

// nolint
func mkSeriesRequest(t *testing.T, from, to string, matches []string) *logproto.SeriesRequest {
	start, end, err := bounds(withForm(url.Values{
		"start": []string{from},
		"end":   []string{to},
	}))
	require.Nil(t, err)

	require.Nil(t, err)
	return &logproto.SeriesRequest{
		Start:  start,
		End:    end,
		Groups: matches,
	}
}
