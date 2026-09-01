package logqltest

import (
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
)

func TestStreamsParser(t *testing.T) {
	t.Run("parse a log stream with repeats and structured metadata", func(t *testing.T) {
		s := newStreamsParser()
		require.NoError(t, s.parse(`{app="foo"} "value={{.i}}" @ 0s [repeat every 10s for 3] [metadata lvl="info"]`))

		streams := s.get()
		require.Len(t, streams, 1)
		require.Equal(t, `{app="foo"}`, streams[0].Labels)

		entries := streams[0].Entries
		require.Len(t, entries, 3)
		for i, e := range entries {
			require.Equal(t, epoch.Add(time.Duration(i)*10*time.Second).UnixNano(), e.Timestamp.UnixNano())
			require.Equal(t, "value="+strconv.Itoa(i), e.Line)
			require.Equal(t, `{lvl="info"}`, logproto.FromLabelAdaptersToLabels(e.StructuredMetadata).String())
		}

		// Entries for the same stream labels accumulate; a missing timestamp is an error.
		require.NoError(t, s.parse(`{app="foo"} "extra" @ 100s`))
		require.Len(t, s.get()[0].Entries, 4)
		require.Error(t, s.parse(`{app="foo"} "no timestamp"`))
	})

	t.Run("selectors that differ only in label order load into one canonical stream", func(t *testing.T) {
		s := newStreamsParser()
		require.NoError(t, s.parse(`{app="foo", env="prod"} "x" @ 0s`))
		require.NoError(t, s.parse(`{env="prod", app="foo"} "y" @ 10s`))

		streams := s.get()
		require.Len(t, streams, 1)
		require.Equal(t, `{app="foo", env="prod"}`, streams[0].Labels)
		require.Len(t, streams[0].Entries, 2)
	})

	t.Run("a backtick raw log line keeps its double quotes", func(t *testing.T) {
		bt := "`"
		s := newStreamsParser()
		require.NoError(t, s.parse(`{app="foo"} `+bt+`{"level":"info","n":1}`+bt+` @ 0s`))

		streams := s.get()
		require.Len(t, streams, 1)
		require.Len(t, streams[0].Entries, 1)
		require.Equal(t, `{"level":"info","n":1}`, streams[0].Entries[0].Line)
	})
}

func TestSplitStreamLabels(t *testing.T) {
	sel, rest, err := splitStreamLabels(`{app="foo", env="prod"} "line" @ 0s`)
	require.NoError(t, err)
	require.Equal(t, `{app="foo", env="prod"}`, sel)
	require.Equal(t, ` "line" @ 0s`, rest)

	_, _, err = splitStreamLabels(`no brace "x"`)
	require.Error(t, err)

	_, _, err = splitStreamLabels(`{unterminated "x"`)
	require.Error(t, err)
}

func TestSplitQuoted(t *testing.T) {
	bt := "`" // a backtick, awkward to embed in Go string literals
	for name, tc := range map[string]struct {
		in       string
		wantText string
		wantRest string
		wantErr  bool
	}{
		"double quoted":                     {in: ` "hello world" @ 0s`, wantText: "hello world", wantRest: " @ 0s"},
		"backtick raw":                      {in: " " + bt + "raw line" + bt + " @ 0s", wantText: "raw line", wantRest: " @ 0s"},
		"backtick keeps double quotes":      {in: " " + bt + `{"a":"b"}` + bt + " @ 0s", wantText: `{"a":"b"}`, wantRest: " @ 0s"},
		"double quotes stop at inner quote": {in: ` "a"b`, wantText: "a", wantRest: "b"},
		"double quotes keep a backtick":     {in: ` "a` + bt + `b" @ 0s`, wantText: "a" + bt + "b", wantRest: " @ 0s"},
		"first delimiter wins":              {in: ` "x" ` + bt + "y" + bt, wantText: "x", wantRest: " " + bt + "y" + bt},
		"missing quote":                     {in: ` no quote here`, wantErr: true},
		"unterminated double quote":         {in: ` "unterminated`, wantErr: true},
		"unterminated backtick":             {in: " " + bt + "unterminated", wantErr: true},
	} {
		t.Run(name, func(t *testing.T) {
			text, rest, err := splitQuoted(tc.in)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantText, text)
			require.Equal(t, tc.wantRest, rest)
		})
	}
}

func TestParseMetadata(t *testing.T) {
	// No metadata at the head: rest is returned unchanged.
	md, rest, err := parseMetadata(`[repeat every 10s for 3]`)
	require.NoError(t, err)
	require.Nil(t, md)
	require.Equal(t, `[repeat every 10s for 3]`, rest)

	// A metadata block at the head is parsed and consumed from rest.
	md, rest, err = parseMetadata(`[metadata detected_level="error" "svc name"="api"] rest`)
	require.NoError(t, err)
	require.Equal(t, []logproto.LabelAdapter{
		{Name: "detected_level", Value: "error"},
		{Name: "svc name", Value: "api"},
	}, md)
	require.Equal(t, ` rest`, rest)

	// Malformed or empty metadata is rejected, not silently dropped.
	_, _, err = parseMetadata(`[metadata trace_id=abc]`) // unquoted value
	require.Error(t, err)
	_, _, err = parseMetadata(`[metadata k="v" junk]`) // stray token
	require.Error(t, err)
	_, _, err = parseMetadata(`[metadata ]`) // empty block
	require.Error(t, err)
	_, _, err = parseMetadata(`[metadata ""="v"]`) // empty key
	require.Error(t, err)
}

func TestStreamsParser_RejectsMalformedDirectives(t *testing.T) {
	for name, line := range map[string]string{
		"unterminated repeat":                `{app="foo"} "x" @ 0s [repeat every 10s for 19`,
		"typo'd repeat keyword":              `{app="foo"} "x" @ 0s [repaet every 10s for 19]`,
		"non-integer repeat count":           `{app="foo"} "x" @ 0s [repeat every 10s for 5.5]`,
		"unquoted metadata value":            `{app="foo"} "x" @ 0s [metadata trace_id=abc]`,
		"stray trailing tokens":              `{app="foo"} "x" @ 0s trailing junk`,
		"trailing junk after a valid clause": `{app="foo"} "x" @ 0s [repeat every 10s for 3] junk`,
		"invalid repeat step":                `{app="foo"} "x" @ 0s [repeat every 1x for 3]`,
		"repeat count overflow":              `{app="foo"} "x" @ 0s [repeat every 10s for 99999999999999999999]`,
		"zero repeat count":                  `{app="foo"} "x" @ 0s [repeat every 10s for 0]`,
	} {
		t.Run(name, func(t *testing.T) {
			require.Error(t, newStreamsParser().parse(line))
		})
	}

	// A well-formed directive still loads the full set of entries.
	s := newStreamsParser()
	require.NoError(t, s.parse(`{app="foo"} "x" @ 0s [repeat every 10s for 19]`))
	require.Len(t, s.get()[0].Entries, 19)
}

func TestParseEval(t *testing.T) {
	cmd, err := parseEval(`eval instant at 60s sum(rate({app="foo"}[1m]))`)
	require.NoError(t, err)
	require.Equal(t, evalInstant, cmd.mode)
	require.Equal(t, time.Minute, cmd.ts)
	require.Equal(t, `sum(rate({app="foo"}[1m]))`, cmd.query)

	cmd, err = parseEval(`eval range from 0 to 10m step 1m count_over_time({app="foo"}[1m])`)
	require.NoError(t, err)
	require.Equal(t, evalRange, cmd.mode)
	require.Equal(t, time.Duration(0), cmd.start)
	require.Equal(t, 10*time.Minute, cmd.end)
	require.Equal(t, time.Minute, cmd.step)
	require.Equal(t, `count_over_time({app="foo"}[1m])`, cmd.query)

	cmd, err = parseEval(`eval select from 0 to 10m forward {app="foo"}`)
	require.NoError(t, err)
	require.Equal(t, evalSelect, cmd.mode)
	require.Equal(t, time.Duration(0), cmd.start)
	require.Equal(t, 10*time.Minute, cmd.end)
	require.Equal(t, 10*time.Minute, cmd.step)
	require.Equal(t, logproto.FORWARD, cmd.direction)
	require.Equal(t, `{app="foo"}`, cmd.query)

	_, err = parseEval(`eval sideways at 0s foo`)
	require.Error(t, err)

	_, err = parseEval(`eval instant garbage`)
	require.Error(t, err)

	// A non-positive range step would silently collapse the window to a single point.
	_, err = parseEval(`eval range from 0 to 10m step 0s count_over_time({app="foo"}[1m])`)
	require.Error(t, err)

	// A backwards range (end before start) would produce an empty step window.
	_, err = parseEval(`eval range from 10s to 0s step 1s count_over_time({app="foo"}[1m])`)
	require.Error(t, err)

	// An empty or backwards select window has no valid step to derive.
	_, err = parseEval(`eval select from 10s to 10s forward {app="foo"}`)
	require.Error(t, err)
	_, err = parseEval(`eval select from 10s to 0s forward {app="foo"}`)
	require.Error(t, err)
}

func TestParseEval_SelectDirection(t *testing.T) {
	for name, tc := range map[string]struct {
		line      string
		direction logproto.Direction
		query     string
	}{
		"forward":          {`eval select from 0 to 10m forward {app="foo"}`, logproto.FORWARD, `{app="foo"}`},
		"backward":         {`eval select from 0 to 10m backward {app="foo"}`, logproto.BACKWARD, `{app="foo"}`},
		"case insensitive": {`eval select from 0 to 10m BackWard {app="foo"}`, logproto.BACKWARD, `{app="foo"}`},
		"pipeline follows": {`eval select from 0 to 10m backward {app="foo"} |= "x" | logfmt`, logproto.BACKWARD, `{app="foo"} |= "x" | logfmt`},
		"extra whitespace": {`eval select from 0 to 10m   backward   {app="foo"}`, logproto.BACKWARD, `{app="foo"}`},
		// The direction is the token before the query, so the same word inside the query is
		// query text.
		"direction word inside the query": {`eval select from 0 to 10m forward {app="foo"} |= "backward"`, logproto.FORWARD, `{app="foo"} |= "backward"`},
	} {
		t.Run(name, func(t *testing.T) {
			cmd, err := parseEval(tc.line)
			require.NoError(t, err)
			require.Equal(t, evalSelect, cmd.mode)
			require.Equal(t, tc.direction, cmd.direction)
			require.Equal(t, tc.query, cmd.query)
		})
	}

	// The direction is required: a select line without one, or with a misspelled one, must be
	// rejected rather than have the stray text folded into the query.
	for name, line := range map[string]string{
		"missing":             `eval select from 0 to 10m {app="foo"}`,
		"misspelled":          `eval select from 0 to 10m backwards {app="foo"}`,
		"unknown word":        `eval select from 0 to 10m sideways {app="foo"}`,
		"direction, no query": `eval select from 0 to 10m backward`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseEval(line)
			require.ErrorContains(t, err, "malformed 'eval select'")
		})
	}

	// A metric query has no line order, so instant and range take no direction: one written there
	// stays part of the query text, where LogQL rejects it.
	cmd, err := parseEval(`eval instant at 60s backward count_over_time({app="foo"}[1m])`)
	require.NoError(t, err)
	require.Equal(t, `backward count_over_time({app="foo"}[1m])`, cmd.query)
	require.Equal(t, logproto.FORWARD, cmd.direction)
}

func TestExpectationsParser(t *testing.T) {
	t.Run("fail with msg qualifier", func(t *testing.T) {
		p := newExpectationsParser()
		require.NoError(t, p.parse("expect fail msg: boom happened"))
		exp := p.get()
		require.True(t, exp.fail)
		require.Equal(t, failMsg, exp.failKind)
		require.Equal(t, "boom happened", exp.failText)
	})

	t.Run("scalar", func(t *testing.T) {
		p := newExpectationsParser()
		require.NoError(t, p.parse("3.5"))
		exp := p.get()
		require.NotNil(t, exp.scalar)
		require.Equal(t, 3.5, *exp.scalar)
	})

	t.Run("series with a gap", func(t *testing.T) {
		p := newExpectationsParser()
		require.NoError(t, p.parse(`{app="foo"} 1 2 3`))
		require.NoError(t, p.parse(`{app="bar"} _ 5`))
		exp := p.get()
		require.Len(t, exp.series, 2)
		require.Equal(t, `{app="foo"}`, exp.series[0].labels)
		require.Equal(t, []sample{{present: true, value: 1}, {present: true, value: 2}, {present: true, value: 3}}, exp.series[0].samples)
		require.Equal(t, `{app="bar"}`, exp.series[1].labels)
		require.Equal(t, []sample{{present: false}, {present: true, value: 5}}, exp.series[1].samples)
	})

	t.Run("expect ordered", func(t *testing.T) {
		p := newExpectationsParser()
		require.NoError(t, p.parse("expect ordered"))
		require.NoError(t, p.parse(`{app="a"} 1`))
		exp := p.get()
		require.True(t, exp.ordered)
		require.Len(t, exp.series, 1)
	})

	t.Run("expect empty", func(t *testing.T) {
		p := newExpectationsParser()
		require.NoError(t, p.parse("expect empty"))
		require.True(t, p.get().empty)
	})

	t.Run("fail with regex qualifier", func(t *testing.T) {
		p := newExpectationsParser()
		require.NoError(t, p.parse("expect fail regex: many-to-one.*explicit"))
		exp := p.get()
		require.True(t, exp.fail)
		require.Equal(t, failRegex, exp.failKind)
		require.Equal(t, "many-to-one.*explicit", exp.failText)
	})

	t.Run("bare fail is allowed, a typo'd qualifier is not", func(t *testing.T) {
		require.NoError(t, newExpectationsParser().parse("expect fail"))
		require.Error(t, newExpectationsParser().parse("expect fail mesg: typo"))
	})

	t.Run("fail qualifier without text is rejected", func(t *testing.T) {
		// An empty qualifier would silently match any error.
		require.Error(t, newExpectationsParser().parse("expect fail msg:"))
		require.Error(t, newExpectationsParser().parse("expect fail regex:"))
	})

	t.Run("invalid scalar line is rejected", func(t *testing.T) {
		require.Error(t, newExpectationsParser().parse("not-a-number"))
	})

	t.Run("unrecognized expect annotation is rejected", func(t *testing.T) {
		require.Error(t, newExpectationsParser().parse("expect sorted"))
	})

	t.Run("skip marks one stack's values as not compared", func(t *testing.T) {
		p := newExpectationsParser()
		require.NoError(t, p.parse(`skip values-comparison on "`+queryFrontendShardStackName+`"`))
		require.NoError(t, p.parse(`{app="a"} 1`))
		exp := p.get()
		require.True(t, exp.isValueComparisonSkipped[queryFrontendShardStackName])
		require.False(t, exp.isValueComparisonSkipped[directStackName])
	})

	t.Run("skip directives accumulate across stacks", func(t *testing.T) {
		p := newExpectationsParser()
		require.NoError(t, p.parse(`skip values-comparison on "`+directStackName+`"`))
		require.NoError(t, p.parse(`skip values-comparison on "`+queryFrontendShardStackName+`"`))
		exp := p.get()
		require.True(t, exp.isValueComparisonSkipped[directStackName])
		require.True(t, exp.isValueComparisonSkipped[queryFrontendShardStackName])
	})

	t.Run("skip with unknown target is rejected", func(t *testing.T) {
		require.ErrorContains(t, newExpectationsParser().parse(`skip series on "`+directStackName+`"`), "unsupported skip target")
	})

	t.Run("skip with unknown stack is rejected", func(t *testing.T) {
		// A typo would otherwise silently skip nothing.
		require.ErrorContains(t, newExpectationsParser().parse(`skip values-comparison on "nope"`), "unknown stack")
	})

	t.Run("malformed skip directive is rejected", func(t *testing.T) {
		// Missing `on`, or an unquoted stack name.
		require.ErrorContains(t, newExpectationsParser().parse(`skip values-comparison "`+directStackName+`"`), "invalid skip directive")
		require.ErrorContains(t, newExpectationsParser().parse(`skip values-comparison on `+directStackName), "invalid skip directive")
	})

	t.Run("values-toleration sets one stack's tolerance", func(t *testing.T) {
		p := newExpectationsParser()
		require.NoError(t, p.parse(`expect values-toleration 0.02 on "`+queryFrontendShardStackName+`"`))
		require.NoError(t, p.parse(`{app="a"} 1`))
		exp := p.get()
		require.Equal(t, 0.02, exp.valuesToleration[queryFrontendShardStackName])
		require.NotContains(t, exp.valuesToleration, directStackName)
	})

	t.Run("values-toleration directives accumulate across stacks", func(t *testing.T) {
		p := newExpectationsParser()
		require.NoError(t, p.parse(`expect values-toleration 0.01 on "`+directStackName+`"`))
		require.NoError(t, p.parse(`expect values-toleration 0.02 on "`+queryFrontendShardStackName+`"`))
		exp := p.get()
		require.Equal(t, 0.01, exp.valuesToleration[directStackName])
		require.Equal(t, 0.02, exp.valuesToleration[queryFrontendShardStackName])
	})

	t.Run("values-toleration with unknown stack is rejected", func(t *testing.T) {
		require.ErrorContains(t, newExpectationsParser().parse(`expect values-toleration 0.02 on "nope"`), "unknown stack")
	})

	t.Run("values-toleration with a non-positive, non-finite, or non-numeric value is rejected", func(t *testing.T) {
		require.ErrorContains(t, newExpectationsParser().parse(`expect values-toleration 0 on "`+directStackName+`"`), "must be a positive, finite number")
		require.ErrorContains(t, newExpectationsParser().parse(`expect values-toleration -0.01 on "`+directStackName+`"`), "must be a positive, finite number")
		require.ErrorContains(t, newExpectationsParser().parse(`expect values-toleration abc on "`+directStackName+`"`), "must be a positive, finite number")
		require.ErrorContains(t, newExpectationsParser().parse(`expect values-toleration NaN on "`+directStackName+`"`), "must be a positive, finite number")
		require.ErrorContains(t, newExpectationsParser().parse(`expect values-toleration Inf on "`+directStackName+`"`), "must be a positive, finite number")
	})

	t.Run("malformed values-toleration directive is rejected", func(t *testing.T) {
		// Missing `on`, or an unquoted stack name.
		require.ErrorContains(t, newExpectationsParser().parse(`expect values-toleration 0.02 "`+directStackName+`"`), "invalid values-toleration directive")
		require.ErrorContains(t, newExpectationsParser().parse(`expect values-toleration 0.02 on `+directStackName), "invalid values-toleration directive")
	})

	t.Run("a stack cannot both skip values-comparison and have a toleration", func(t *testing.T) {
		// skip, then toleration, on the same stack.
		p := newExpectationsParser()
		require.NoError(t, p.parse(`skip values-comparison on "`+directStackName+`"`))
		require.ErrorContains(t, p.parse(`expect values-toleration 0.02 on "`+directStackName+`"`), "cannot also set a toleration")

		// toleration, then skip, on the same stack.
		p = newExpectationsParser()
		require.NoError(t, p.parse(`expect values-toleration 0.02 on "`+directStackName+`"`))
		require.ErrorContains(t, p.parse(`skip values-comparison on "`+directStackName+`"`), "cannot also skip values-comparison")
	})

	t.Run("duplicate values-toleration directive for the same stack is rejected", func(t *testing.T) {
		p := newExpectationsParser()
		require.NoError(t, p.parse(`expect values-toleration 0.02 on "`+directStackName+`"`))
		require.ErrorContains(t, p.parse(`expect values-toleration 0.03 on "`+directStackName+`"`), "duplicate values-toleration directive")
	})

	t.Run("skip and values-toleration on different stacks both apply", func(t *testing.T) {
		p := newExpectationsParser()
		require.NoError(t, p.parse(`skip values-comparison on "`+directStackName+`"`))
		require.NoError(t, p.parse(`expect values-toleration 0.02 on "`+queryFrontendShardStackName+`"`))
		exp := p.get()
		require.True(t, exp.isValueComparisonSkipped[directStackName])
		require.Equal(t, 0.02, exp.valuesToleration[queryFrontendShardStackName])
	})
}

func TestExpectationsValidate(t *testing.T) {
	scalar := 1.0
	series := []expectedSeries{{labels: `{a="b"}`, samples: []sample{{present: true, value: 1}}}}
	streams := []expectedStream{{labels: `{a="b"}`, entries: []expectedLogEntry{{ts: 0, line: "x"}}}}

	// Valid: exactly one result kind (plus ordered alongside series).
	require.NoError(t, expectations{scalar: &scalar}.validate())
	require.NoError(t, expectations{series: series}.validate())
	require.NoError(t, expectations{streams: streams}.validate())
	require.NoError(t, expectations{empty: true}.validate())
	require.NoError(t, expectations{fail: true}.validate())
	require.NoError(t, expectations{ordered: true, series: series}.validate())

	// Invalid: no expectation, conflicting kinds, `expect ordered` without series, or a failure
	// qualifier without `fail`.
	require.Error(t, expectations{}.validate())
	require.Error(t, expectations{fail: true, series: series}.validate())
	require.Error(t, expectations{scalar: &scalar, series: series}.validate())
	require.Error(t, expectations{empty: true, series: series}.validate())
	require.Error(t, expectations{series: series, streams: streams}.validate())
	require.Error(t, expectations{ordered: true}.validate())
	require.Error(t, expectations{ordered: true, scalar: &scalar}.validate()) // ordered needs series
	require.Error(t, expectations{failKind: failMsg}.validate())              // qualifier without fail
}

func TestIsLogLine(t *testing.T) {
	require.True(t, isLogLine(`{app="foo"} "line" @ 10s`))
	require.True(t, isLogLine("{app=\"foo\"} `raw line` @ 10s"))
	require.False(t, isLogLine(`{app="foo"} 1 2 3`))
	require.False(t, isLogLine(`{app="foo"} _ 5`))
	require.False(t, isLogLine(`no braces`))
}

func TestParseLogLine(t *testing.T) {
	lbls, ts, text, err := parseLogLine(`{app="foo", env="prod"} "hello world" @ 90s`)
	require.NoError(t, err)
	require.Equal(t, `{app="foo", env="prod"}`, lbls.String())
	require.Equal(t, 90*time.Second, ts)
	require.Equal(t, "hello world", text)

	// A backtick raw line keeps its double quotes.
	bt := "`"
	_, _, text, err = parseLogLine(`{app="foo"} ` + bt + `{"a":"b"}` + bt + ` @ 0s`)
	require.NoError(t, err)
	require.Equal(t, `{"a":"b"}`, text)

	_, _, _, err = parseLogLine(`{app="foo"} "line"`) // missing timestamp
	require.Error(t, err)

	_, _, _, err = parseLogLine(`{app="foo"} "line" @ 0s trailing junk`)
	require.Error(t, err)
}

func TestExpectationsParser_LogStreams(t *testing.T) {
	p := newExpectationsParser()
	require.NoError(t, p.parse(`{app="foo"} "1st" @ 0s`))
	require.NoError(t, p.parse(`{app="foo"} "2nd" @ 10s`))
	require.NoError(t, p.parse(`{app="bar"} "x" @ 5s`))
	exp := p.get()

	require.Len(t, exp.streams, 2)
	require.Equal(t, `{app="foo"}`, exp.streams[0].labels)
	require.Equal(t, []expectedLogEntry{{ts: 0, line: "1st"}, {ts: 10 * time.Second, line: "2nd"}}, exp.streams[0].entries)
	require.Equal(t, `{app="bar"}`, exp.streams[1].labels)
	require.Equal(t, []expectedLogEntry{{ts: 5 * time.Second, line: "x"}}, exp.streams[1].entries)

	// A malformed log line (missing timestamp) is surfaced, not silently dropped.
	require.Error(t, newExpectationsParser().parse(`{app="foo"} "no timestamp"`))
}

func TestParseSeriesLabels(t *testing.T) {
	// An empty-value label is preserved, so a test can assert a present-but-empty label distinctly
	// from an absent one (unlike stream labels, which syntax.ParseLabels normalizes with WithoutEmpty).
	present, err := parseSeriesLabels(`{app="a", age=""}`)
	require.NoError(t, err)
	require.Equal(t, `{age="", app="a"}`, present.String())

	absent, err := parseSeriesLabels(`{app="a"}`)
	require.NoError(t, err)
	require.Equal(t, `{app="a"}`, absent.String())
	require.NotEqual(t, present.String(), absent.String())

	empty, err := parseSeriesLabels(`{}`)
	require.NoError(t, err)
	require.Equal(t, `{}`, empty.String())
}

func TestParseSeriesLine(t *testing.T) {
	lbls, samples, err := parseSeriesLine(`{app="foo", env="prod"} 1 2 3`)
	require.NoError(t, err)
	require.Equal(t, `{app="foo", env="prod"}`, lbls.String())
	require.Equal(t, []sample{{present: true, value: 1}, {present: true, value: 2}, {present: true, value: 3}}, samples)

	lbls, samples, err = parseSeriesLine(`{} 5`)
	require.NoError(t, err)
	require.Equal(t, `{}`, lbls.String())
	require.Equal(t, []sample{{present: true, value: 5}}, samples)

	_, _, err = parseSeriesLine(`missing braces 1 2`)
	require.Error(t, err)

	_, _, err = parseSeriesLine(`{app="foo"}`) // no sample values
	require.Error(t, err)
}

func TestParseSamples(t *testing.T) {
	for name, tc := range map[string]struct {
		in   []string
		want []sample
	}{
		"single value": {
			in:   []string{"5"},
			want: []sample{{present: true, value: 5}},
		},
		"gap": {
			in:   []string{"_"},
			want: []sample{{present: false}},
		},
		"values with a gap": {
			in:   []string{"1", "_", "3"},
			want: []sample{{present: true, value: 1}, {present: false}, {present: true, value: 3}},
		},
		"incrementing expansion": {
			in:   []string{"2+3x2"},
			want: []sample{{present: true, value: 2}, {present: true, value: 5}, {present: true, value: 8}},
		},
		"constant expansion": {
			in:   []string{"4x3"},
			want: []sample{{present: true, value: 4}, {present: true, value: 4}, {present: true, value: 4}, {present: true, value: 4}},
		},
		"decrementing expansion": {
			in:   []string{"1-1x2"},
			want: []sample{{present: true, value: 1}, {present: true, value: 0}, {present: true, value: -1}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := parseSamples(tc.in)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestParseSamples_SpecialValues(t *testing.T) {
	got, err := parseSamples([]string{"NaN", "+Inf", "-Inf"})
	require.NoError(t, err)
	require.Len(t, got, 3)
	require.True(t, math.IsNaN(got[0].value))
	require.True(t, math.IsInf(got[1].value, 1))
	require.True(t, math.IsInf(got[2].value, -1))
}

func TestParseSamples_RejectsMalformed(t *testing.T) {
	for name, tokens := range map[string][]string{
		"non-numeric value":        {"abc"},
		"non-integer repeat count": {"5x2.5"},
		"negative repeat count":    {"5x-1"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseSamples(tokens)
			require.Error(t, err)
		})
	}
}

func TestParseFloat(t *testing.T) {
	f, err := parseFloat("NaN")
	require.NoError(t, err)
	require.True(t, math.IsNaN(f))

	f, err = parseFloat("1.5")
	require.NoError(t, err)
	require.Equal(t, 1.5, f)

	_, err = parseFloat("not-a-number")
	require.Error(t, err)
}

func TestStripComment(t *testing.T) {
	for name, tc := range map[string]struct {
		input    string
		expected string
	}{
		"no comment": {
			input:    `no comment here`,
			expected: `no comment here`,
		},
		"trailing comment": {
			input:    `value # comment`,
			expected: `value`,
		},
		"whole line comment": {
			input:    `  # whole line comment`,
			expected: ``,
		},
		"hash inside double quotes": {
			input:    `{app="foo"} "a # b" @ 0s # real`,
			expected: `{app="foo"} "a # b" @ 0s`,
		},
		"hash inside backticks": {
			input:    "count_over_time({app=`x#y`}[1m]) # c",
			expected: "count_over_time({app=`x#y`}[1m])",
		},
		"trailing spaces before comment": {
			input:    `trailing spaces   # c`,
			expected: `trailing spaces`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.expected, stripComment(tc.input))
		})
	}
}
