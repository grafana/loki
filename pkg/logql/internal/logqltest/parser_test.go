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
	require.Nil(t, parseMetadata(`@ 0s [repeat every 10s for 3]`))

	got := parseMetadata(`@ 0s [metadata detected_level="error" "svc name"="api"]`)
	require.Equal(t, []logproto.LabelAdapter{
		{Name: "detected_level", Value: "error"},
		{Name: "svc name", Value: "api"},
	}, got)
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

	// Invalid scalar line.
	require.Error(t, newExpectationsParser().parse("not-a-number"))

	// Unrecognized expect annotation.
	require.Error(t, newExpectationsParser().parse("expect ordered"))
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
	for _, tc := range []struct {
		in   []string
		want []sample
	}{
		{[]string{"5"}, []sample{{present: true, value: 5}}},
		{[]string{"_"}, []sample{{present: false}}},
		{[]string{"1", "_", "3"}, []sample{{present: true, value: 1}, {present: false}, {present: true, value: 3}}},
		{[]string{"2+3x2"}, []sample{{present: true, value: 2}, {present: true, value: 5}, {present: true, value: 8}}},
		{[]string{"4x3"}, []sample{{present: true, value: 4}, {present: true, value: 4}, {present: true, value: 4}, {present: true, value: 4}}},
		{[]string{"1-1x2"}, []sample{{present: true, value: 1}, {present: true, value: 0}, {present: true, value: -1}}},
	} {
		got, err := parseSamples(tc.in)
		require.NoError(t, err)
		require.Equal(t, tc.want, got, tc.in)
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
	for _, tc := range []struct {
		input, expected string
	}{
		{`no comment here`, `no comment here`},
		{`value # comment`, `value`},
		{`  # whole line comment`, ``},
		{`{app="foo"} "a # b" @ 0s # real`, `{app="foo"} "a # b" @ 0s`},
		{"count_over_time({app=`x#y`}[1m]) # c", "count_over_time({app=`x#y`}[1m])"},
		{`trailing spaces   # c`, `trailing spaces`},
	} {
		require.Equal(t, tc.expected, stripComment(tc.input), tc.input)
	}
}
