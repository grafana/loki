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
	text, rest, err := splitQuoted(` "hello world" @ 0s`)
	require.NoError(t, err)
	require.Equal(t, `hello world`, text)
	require.Equal(t, ` @ 0s`, rest)

	_, _, err = splitQuoted(` no quote here`)
	require.Error(t, err)

	_, _, err = splitQuoted(` "unterminated`)
	require.Error(t, err)
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
	require.True(t, cmd.instant)
	require.Equal(t, time.Minute, cmd.ts)
	require.Equal(t, `sum(rate({app="foo"}[1m]))`, cmd.query)

	cmd, err = parseEval(`eval range from 0 to 10m step 1m count_over_time({app="foo"}[1m])`)
	require.NoError(t, err)
	require.False(t, cmd.instant)
	require.Equal(t, time.Duration(0), cmd.start)
	require.Equal(t, 10*time.Minute, cmd.end)
	require.Equal(t, time.Minute, cmd.step)
	require.Equal(t, `count_over_time({app="foo"}[1m])`, cmd.query)

	_, err = parseEval(`eval sideways at 0s foo`)
	require.Error(t, err)

	_, err = parseEval(`eval instant garbage`)
	require.Error(t, err)
}

func TestExpectationsParser(t *testing.T) {
	// Failure assertion.
	p := newExpectationsParser()
	require.NoError(t, p.parse("expect fail msg: boom happened"))
	exp := p.get()
	require.True(t, exp.fail)
	require.Equal(t, "msg", exp.failKind)
	require.Equal(t, "boom happened", exp.failText)

	// Scalar result.
	p = newExpectationsParser()
	require.NoError(t, p.parse("3.5"))
	exp = p.get()
	require.NotNil(t, exp.scalar)
	require.Equal(t, 3.5, *exp.scalar)

	// Series results (vector/matrix), including a gap.
	p = newExpectationsParser()
	require.NoError(t, p.parse(`{app="foo"} 1 2 3`))
	require.NoError(t, p.parse(`{app="bar"} _ 5`))
	exp = p.get()
	require.Len(t, exp.series, 2)
	require.Equal(t, `{app="foo"}`, exp.series[0].labels)
	require.Equal(t, []sample{{present: true, value: 1}, {present: true, value: 2}, {present: true, value: 3}}, exp.series[0].samples)
	require.Equal(t, `{app="bar"}`, exp.series[1].labels)
	require.Equal(t, []sample{{present: false}, {present: true, value: 5}}, exp.series[1].samples)

	// Ordered annotation switches series comparison to positional.
	p = newExpectationsParser()
	require.NoError(t, p.parse("expect ordered"))
	require.NoError(t, p.parse(`{app="a"} 1`))
	exp = p.get()
	require.True(t, exp.ordered)
	require.Len(t, exp.series, 1)

	// expect empty.
	p = newExpectationsParser()
	require.NoError(t, p.parse("expect empty"))
	require.True(t, p.get().empty)

	// expect fail with a regex qualifier.
	p = newExpectationsParser()
	require.NoError(t, p.parse("expect fail regex: many-to-one.*explicit"))
	exp = p.get()
	require.True(t, exp.fail)
	require.Equal(t, "regex", exp.failKind)
	require.Equal(t, "many-to-one.*explicit", exp.failText)

	// Bare `expect fail` is allowed; a typo'd qualifier is rejected rather than silently ignored.
	require.NoError(t, newExpectationsParser().parse("expect fail"))
	require.Error(t, newExpectationsParser().parse("expect fail mesg: typo"))

	// Invalid scalar line.
	require.Error(t, newExpectationsParser().parse("not-a-number"))

	// Unrecognized expect annotation.
	require.Error(t, newExpectationsParser().parse("expect sorted"))
}

func TestExpectationsValidate(t *testing.T) {
	scalar := 1.0
	series := []expectedSeries{{labels: `{a="b"}`, samples: []sample{{present: true, value: 1}}}}

	// Valid: exactly one result kind (plus ordered alongside series).
	require.NoError(t, expectations{scalar: &scalar}.validate())
	require.NoError(t, expectations{series: series}.validate())
	require.NoError(t, expectations{empty: true}.validate())
	require.NoError(t, expectations{fail: true}.validate())
	require.NoError(t, expectations{ordered: true, series: series}.validate())

	// Invalid: no expectation, conflicting kinds, `expect ordered` without series, or a failure
	// qualifier without `fail`.
	require.Error(t, expectations{}.validate())
	require.Error(t, expectations{fail: true, series: series}.validate())
	require.Error(t, expectations{scalar: &scalar, series: series}.validate())
	require.Error(t, expectations{empty: true, series: series}.validate())
	require.Error(t, expectations{ordered: true}.validate())
	require.Error(t, expectations{ordered: true, scalar: &scalar}.validate()) // ordered needs series
	require.Error(t, expectations{failKind: "msg"}.validate())                // qualifier without fail
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
