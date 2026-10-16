package commands

import (
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/lokitool/query"
)

// streamsResponse has two streams with different labels, so the labels are not common labels.
const streamsResponse = `{
  "status": "success",
  "data": {
    "resultType": "streams",
    "result": [
      {"stream": {"app": "foo"}, "values": [["1700000001123456789", "line 2"]]},
      {"stream": {"app": "bar"}, "values": [["1700000000000000000", "line 1"]]}
    ],
    "stats": {}
  }
}`

const emptyResponse = `{"status": "success", "data": {"resultType": "streams", "result": [], "stats": {}}}`

// queryServer starts a server that responds to query requests with a stream of two entries on the first request,
// and with an empty result on all following requests.
func queryServer(t *testing.T) *server {
	t.Helper()

	first := true
	return newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		if first {
			first = false
			_, _ = w.Write([]byte(streamsResponse))
			return
		}
		_, _ = w.Write([]byte(emptyResponse))
	})
}

func TestQueryRange(t *testing.T) {
	t.Run("output formats", func(t *testing.T) {
		for _, tc := range []struct {
			name  string
			args  []string
			check func(t *testing.T, out string)
		}{
			{
				name: "default",
				args: []string{"--timezone=UTC"},
				check: func(t *testing.T, out string) {
					require.Contains(t, out, "2023-11-14T22:13:21Z")
					require.Contains(t, out, `{app="foo"} line 2`)
					require.Contains(t, out, `{app="bar"} line 1`)
				},
			},
			{
				name: "timestamp format",
				args: []string{"--timezone=UTC", "--output-timestamp-format=rfc3339nano"},
				check: func(t *testing.T, out string) {
					require.Contains(t, out, "2023-11-14T22:13:21.123456789Z")
				},
			},
			{
				name: "raw with -o",
				args: []string{"-o", "raw"},
				check: func(t *testing.T, out string) {
					require.Equal(t, "line 2\nline 1\n", out)
				},
			},
			{
				name: "raw with --format",
				args: []string{"--format=raw"},
				check: func(t *testing.T, out string) {
					require.Equal(t, "line 2\nline 1\n", out)
				},
			},
			{
				name: "raw with --output",
				args: []string{"--output=raw"},
				check: func(t *testing.T, out string) {
					require.Equal(t, "line 2\nline 1\n", out)
				},
			},
			{
				name: "jsonl",
				args: []string{"--format=jsonl"},
				check: func(t *testing.T, out string) {
					require.Contains(t, out, `"line":"line 2"`)
					require.Contains(t, out, `"labels":{"app":"foo"}`)
					require.Contains(t, out, `"labels":{"app":"bar"}`)
				},
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				srv := queryServer(t)

				args := append([]string{"--address=" + srv.URL, "query"}, tc.args...)
				args = append(args, "range", "--limit=2", `{app="foo"}`)
				out, err := runApplication(t, args...)
				require.NoError(t, err)
				tc.check(t, out)
			})
		}
	})

	t.Run("request parameters", func(t *testing.T) {
		var query string
		srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
			query = r.URL.RawQuery
			_, _ = w.Write([]byte(streamsResponse))
		})

		_, err := runApplication(t, "--address="+srv.URL, "--id=tenant", "query", "range",
			"--from=2023-11-14T22:00:00Z", "--to=2023-11-14T23:00:00Z",
			"--limit=2", "--forward", "--step=30s", "--interval=1m", `{app="foo"} |= "line"`)
		require.NoError(t, err)
		require.Equal(t, "tenant", srv.Requests()[0].Header.Get("X-Scope-OrgID"))

		params, err := url.ParseQuery(query)
		require.NoError(t, err)
		require.Equal(t, `{app="foo"} |= "line"`, params.Get("query"))
		require.Equal(t, "2", params.Get("limit"))
		require.Equal(t, "1699999200000000000", params.Get("start"))
		require.Equal(t, "1700002800000000000", params.Get("end"))
		require.Equal(t, "FORWARD", params.Get("direction"))
		require.Equal(t, "30", params.Get("step"))
		require.Equal(t, "60", params.Get("interval"))
	})

	t.Run("parallel", func(t *testing.T) {
		srv := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(emptyResponse))
		})

		_, err := runApplication(t, "--address="+srv.URL, "query", "range",
			"--from=2023-11-14T22:00:00Z", "--to=2023-11-14T23:00:00Z",
			"--parallel-duration=30m", "--parallel-max-workers=2", `{app="foo"}`)
		require.NoError(t, err)
		require.Len(t, srv.Requests(), 2)
	})

	t.Run("statistics are printed to stderr", func(t *testing.T) {
		srv := queryServer(t)

		var stdout string
		stderr := captureStderr(t, func() {
			var err error
			stdout, err = runApplication(t, "--address="+srv.URL, "query", "--stats", "--format=raw", "range", "--limit=2", `{app="foo"}`)
			require.NoError(t, err)
		})

		require.Equal(t, "line 2\nline 1\n", stdout)
		require.Contains(t, stderr, "Summary.")
	})

	t.Run("statistics are not printed by default", func(t *testing.T) {
		srv := queryServer(t)

		stderr := captureStderr(t, func() {
			_, err := runApplication(t, "--address="+srv.URL, "query", "--format=raw", "range", "--limit=2", `{app="foo"}`)
			require.NoError(t, err)
		})

		require.Empty(t, stderr)
	})

	t.Run("invalid time", func(t *testing.T) {
		_, err := runApplication(t, "query", "range", "--from=yesterday", `{app="foo"}`)
		require.ErrorContains(t, err, "unable to parse time")
	})

	t.Run("server error", func(t *testing.T) {
		srv := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "parse error", http.StatusBadRequest)
		})

		_, err := runApplication(t, "--address="+srv.URL, "query", "range", `{app="foo"}`)
		require.ErrorContains(t, err, "400 Bad Request: parse error")
	})

	t.Run("missing query", func(t *testing.T) {
		_, err := runApplication(t, "query", "range")
		require.ErrorContains(t, err, "required argument 'query' not provided")
	})
}

func TestQueryRangeStdin(t *testing.T) {
	stdin := os.Stdin
	t.Cleanup(func() { os.Stdin = stdin })

	for _, tc := range []struct {
		name  string
		query string
	}{
		{"with stream selector", `{source="logcli"} |= "error"`},
		{"without stream selector", `|= "error"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The input is read once for each query.
			r, w, err := os.Pipe()
			require.NoError(t, err)
			_, err = w.WriteString("level=info msg=started\nlevel=error msg=failed\nlevel=info msg=done\n")
			require.NoError(t, err)
			require.NoError(t, w.Close())
			os.Stdin = r

			out, err := runApplication(t, "query", "--stdin", "--format=raw", "range", tc.query)
			require.NoError(t, err)
			require.Equal(t, "level=error msg=failed\n", out)
		})
	}
}

func TestQueryRangeStdinDoesNotLogQueryStatistics(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	_, err = w.WriteString("level=info msg=started\nlevel=error msg=failed\n")
	require.NoError(t, err)
	require.NoError(t, w.Close())

	stdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = stdin })

	stderr := captureStderr(t, func() {
		out, err := runApplication(t, "query", "--stdin", "--format=raw", "range", `|= "error"`)
		require.NoError(t, err)
		require.Equal(t, "level=error msg=failed\n", out)
	})
	require.Empty(t, stderr)
}

func TestPrepareStdinQuery(t *testing.T) {
	start := time.Date(2023, 11, 14, 22, 0, 0, 0, time.UTC)

	for _, tc := range []struct {
		name      string
		query     query.Query
		wantQuery string
		wantStep  time.Duration
	}{
		{
			name:      "adds default step and keeps query with stream selector",
			query:     query.Query{QueryString: `{foo="bar"} |= "error"`, Start: start, End: start.Add(time.Hour), Limit: 30},
			wantQuery: `{foo="bar"} |= "error"`,
			wantStep:  14 * time.Second,
		},
		{
			name:      "short range has a step of one second",
			query:     query.Query{QueryString: `{foo="bar"}`, Start: start, End: start.Add(time.Minute)},
			wantQuery: `{foo="bar"}`,
			wantStep:  time.Second,
		},
		{
			name:      "keeps the step",
			query:     query.Query{QueryString: `{foo="bar"}`, Start: start, End: start.Add(time.Hour), Step: time.Minute},
			wantQuery: `{foo="bar"}`,
			wantStep:  time.Minute,
		},
		{
			name:      "adds stream selector to line filter",
			query:     query.Query{QueryString: `|= "error"`, Start: start, End: start.Add(time.Hour)},
			wantQuery: `{source="logcli"}|= "error"`,
			wantStep:  14 * time.Second,
		},
		{
			name:      "adds stream selector to negated line filter",
			query:     query.Query{QueryString: ` != "debug"`, Start: start, End: start.Add(time.Hour)},
			wantQuery: `{source="logcli"} != "debug"`,
			wantStep:  14 * time.Second,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prepareStdinQuery(&tc.query)
			require.Equal(t, tc.wantQuery, tc.query.QueryString)
			require.Equal(t, tc.wantStep, tc.query.Step)
			require.Zero(t, tc.query.Limit)
		})
	}
}
