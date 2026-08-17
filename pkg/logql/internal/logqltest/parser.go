package logqltest

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	promql_parser "github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

// epoch is the base time the script's relative timestamps are added to. Timestamps in a
// script (`@ 10s`, `eval instant at 60s`) are durations offset from this base.
//
// A realistic base, not Unix(0,0): the query-range HTTP codec sends times as integer nanoseconds,
// and loghttp.parseTimestamp reads any value with 10 or fewer digits as seconds. Times near
// Unix(0,0) have <= 10 digits and are misread as seconds; a 2026 base keeps them at 19 digits.
var epoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

var (
	// reInstant and reRange match the remainder of an `eval instant`/`eval range` line (after the
	// mode keyword), capturing the trailing query to end of line.
	reInstant = regexp.MustCompile(`^at\s+(\S+)\s+(.+)$`)
	reRange   = regexp.MustCompile(`^from\s+(\S+)\s+to\s+(\S+)\s+step\s+(\S+)\s+(.+)$`)

	// reAt, reRepeat and reMetadata are anchored to the head so each load-line directive matches
	// only its own leading segment and can never reach into a later [metadata …] value.
	reAt       = regexp.MustCompile(`^\s*@\s*(\S+)`)
	reRepeat   = regexp.MustCompile(`^\s*\[repeat every\s+(\S+)\s+for\s+(\d+)\]`)
	reMetadata = regexp.MustCompile(`^\s*\[metadata\s+(.*?)\]`)

	// reSkip matches a `skip <what> on "<stack>"` directive in an expectation block.
	reSkip = regexp.MustCompile(`^skip\s+(\S+)\s+on\s+"([^"]+)"$`)

	// reMetadataKeyValue scans the key="value" pairs inside an already-extracted metadata block; it
	// is intentionally not anchored.
	reMetadataKeyValue = regexp.MustCompile(`(?:"([^"]*)"|([^\s"=]+))="([^"]*)"`)
)

type streamsParser struct {
	streamsOrder []string
	streams      map[string]*logproto.Stream
}

func newStreamsParser() *streamsParser {
	return &streamsParser{streams: map[string]*logproto.Stream{}}
}

func (p *streamsParser) parse(line string) error {
	// Each step below consumes its segment from the head of `rest`; whatever is left at the end
	// must be empty, so a typo'd or unterminated directive can't be silently ignored (e.g. a broken
	// `[repeat ...]` would otherwise quietly load a single entry).

	// Consume the stream labels. Canonicalize them so equivalent labels – that differ only in
	// label order or spacing key – end up being assigned to the same log stream.
	streamLabels, rest, err := splitStreamLabels(line)
	if err != nil {
		return err
	}
	parsedStreamLabels, err := syntax.ParseLabels(streamLabels)
	if err != nil {
		return fmt.Errorf("invalid stream labels %q: %w", streamLabels, err)
	}
	streamLabels = parsedStreamLabels.String()

	// Consume the log message.
	message, rest, err := splitQuoted(rest)
	if err != nil {
		return err
	}

	// Consume the '@ <start>' timestamp (required).
	m := reAt.FindStringSubmatch(rest)
	if m == nil {
		return fmt.Errorf("missing '@ <start>' timestamp")
	}
	start, err := time.ParseDuration(m[1])
	if err != nil {
		return fmt.Errorf("invalid start %q: %w", m[1], err)
	}
	rest = rest[len(m[0]):]

	// Consume the optional '[repeat every <step> for <count>]' clause.
	step := time.Duration(0)
	count := 1
	if r := reRepeat.FindStringSubmatch(rest); r != nil {
		if step, err = time.ParseDuration(r[1]); err != nil {
			return fmt.Errorf("invalid repeat step %q: %w", r[1], err)
		}
		if count, err = strconv.Atoi(r[2]); err != nil {
			return fmt.Errorf("invalid repeat count %q: %w", r[2], err)
		}
		if count < 1 {
			return fmt.Errorf("repeat count must be at least 1, got %q", r[2])
		}
		rest = rest[len(r[0]):]
	}

	// Consume the optional '[metadata key="value" ...]' clause.
	metadata, rest, err := parseMetadata(rest)
	if err != nil {
		return err
	}

	// Ensure there's nothing left to parse.
	if leftover := strings.TrimSpace(rest); leftover != "" {
		return fmt.Errorf("unexpected content after log line: %q", leftover)
	}

	// Create the log stream.
	stream, ok := p.streams[streamLabels]
	if !ok {
		stream = &logproto.Stream{Labels: streamLabels}
		p.streams[streamLabels] = stream
		p.streamsOrder = append(p.streamsOrder, streamLabels)
	}
	for i := 0; i < count; i++ {
		stream.Entries = append(stream.Entries, push.Entry{
			Timestamp:          epoch.Add(start + time.Duration(i)*step),
			Line:               strings.ReplaceAll(message, "{{.i}}", strconv.Itoa(i)),
			StructuredMetadata: metadata,
		})
	}

	return nil
}

// get returns the parsed log streams in the same order they appear in the script.
func (p *streamsParser) get() []logproto.Stream {
	out := make([]logproto.Stream, 0, len(p.streamsOrder))
	for _, k := range p.streamsOrder {
		out = append(out, *p.streams[k])
	}
	return out
}

// splitStreamLabels returns the leading `{...}` log stream labels and the remainder of the line.
func splitStreamLabels(line string) (streamLabels, rest string, err error) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "{") {
		return "", "", fmt.Errorf("expected stream labels starting with '{'")
	}
	end := strings.IndexByte(line, '}')
	if end < 0 {
		return "", "", fmt.Errorf("unterminated stream labels")
	}
	return line[:end+1], line[end+1:], nil
}

// splitQuoted returns the first quoted log line in s and the remainder after it. The line may be
// delimited by double quotes ("...") or by backticks (`...`). Backticks are raw and let the line
// hold double quotes, e.g. a JSON object. Neither form processes escape sequences.
func splitQuoted(s string) (text, rest string, err error) {
	start := strings.IndexAny(s, "\"`")
	if start < 0 {
		return "", "", fmt.Errorf("missing quoted log line")
	}
	quote := s[start]
	end := strings.IndexByte(s[start+1:], quote)
	if end < 0 {
		return "", "", fmt.Errorf("unterminated quoted log line")
	}
	end += start + 1
	return s[start+1 : end], s[end+1:], nil
}

// parseMetadata extracts the optional `[metadata key="value" ...]` block, returning the parsed
// metadata and `rest` with that block removed. A block that is present but malformed (e.g. an
// unquoted value) is an error rather than silently dropped.
func parseMetadata(rest string) ([]logproto.LabelAdapter, string, error) {
	m := reMetadata.FindStringSubmatch(rest)
	if m == nil {
		return nil, rest, nil
	}
	inner := strings.TrimSpace(m[1])
	var out []logproto.LabelAdapter
	for _, kv := range reMetadataKeyValue.FindAllStringSubmatch(inner, -1) {
		key := kv[1]
		if key == "" {
			key = kv[2]
		}
		if key == "" {
			return nil, rest, fmt.Errorf("empty metadata key in %q", inner)
		}
		out = append(out, logproto.LabelAdapter{Name: key, Value: kv[3]})
	}
	// Every token inside the block must be a key="value" pair; anything left over is malformed.
	if leftover := strings.TrimSpace(reMetadataKeyValue.ReplaceAllString(inner, "")); leftover != "" {
		return nil, rest, fmt.Errorf("invalid metadata %q: expected key=\"value\" pairs", inner)
	}
	if len(out) == 0 {
		return nil, rest, fmt.Errorf("empty [metadata ...] block")
	}
	return out, rest[len(m[0]):], nil
}

type evalCmd struct {
	instant          bool
	ts               time.Duration // instant queries
	start, end, step time.Duration // range queries
	query            string
}

// getTimeRange returns the query's [start, end] range and step.
func (c evalCmd) getTimeRange() (start, end, step time.Duration) {
	if c.instant {
		return c.ts, c.ts, 0
	}
	return c.start, c.end, c.step
}

func parseEval(line string) (evalCmd, error) {
	rest := strings.TrimSpace(strings.TrimPrefix(line, "eval"))
	switch {
	case strings.HasPrefix(rest, "instant"):
		m := reInstant.FindStringSubmatch(strings.TrimSpace(strings.TrimPrefix(rest, "instant")))
		if m == nil {
			return evalCmd{}, fmt.Errorf("malformed 'eval instant': %q", line)
		}
		ts, err := time.ParseDuration(m[1])
		if err != nil {
			return evalCmd{}, fmt.Errorf("invalid instant time %q: %w", m[1], err)
		}
		return evalCmd{instant: true, ts: ts, query: strings.TrimSpace(m[2])}, nil
	case strings.HasPrefix(rest, "range"):
		m := reRange.FindStringSubmatch(strings.TrimSpace(strings.TrimPrefix(rest, "range")))
		if m == nil {
			return evalCmd{}, fmt.Errorf("malformed 'eval range': %q", line)
		}
		start, err := time.ParseDuration(m[1])
		if err != nil {
			return evalCmd{}, fmt.Errorf("invalid range start %q: %w", m[1], err)
		}
		end, err := time.ParseDuration(m[2])
		if err != nil {
			return evalCmd{}, fmt.Errorf("invalid range end %q: %w", m[2], err)
		}
		step, err := time.ParseDuration(m[3])
		if err != nil {
			return evalCmd{}, fmt.Errorf("invalid range step %q: %w", m[3], err)
		}
		if step <= 0 {
			return evalCmd{}, fmt.Errorf("range step must be positive, got %q", m[3])
		}
		if end < start {
			return evalCmd{}, fmt.Errorf("range end %q is before start %q", m[2], m[1])
		}
		return evalCmd{start: start, end: end, step: step, query: strings.TrimSpace(m[4])}, nil
	default:
		return evalCmd{}, fmt.Errorf("expected 'instant' or 'range' after eval: %q", line)
	}
}

type sample struct {
	present bool
	value   float64
}

// failMatch selects how an `expect fail` assertion checks the error.
type failMatch uint8

const (
	failAny   failMatch = iota // any error satisfies the assertion
	failMsg                    // the error must contain failText as a substring
	failRegex                  // the error must match failText as a regex
)

// expectations is the parsed expected result of an `eval` command: either a failure
// assertion, a scalar value, an empty-result assertion, or a set of series (for
// vector/matrix results).
type expectations struct {
	fail     bool
	failKind failMatch
	failText string
	empty    bool // when set, the result must contain no series (`expect empty`)
	ordered  bool // when set, series are compared positionally (for sort/sort_desc); instant only
	scalar   *float64
	series   []expectedSeries

	// isValueComparisonSkipped holds the execution stacks (by name) whose result values are not
	// compared, set by a `skip values-comparison on "<stack>"` directive. The stack still runs and
	// must not error; only the value/series comparison is skipped.
	isValueComparisonSkipped map[string]bool
}

// validate ensures an eval asserts exactly one result kind: series, a scalar, `expect empty`,
// or `expect fail`. This catches a forgotten expectation block (which would otherwise pass
// vacuously on an empty result) and contradictory combinations that would be silently ignored.
func (e expectations) validate() error {
	kinds := 0
	for _, set := range []bool{e.fail, e.empty, e.scalar != nil, len(e.series) > 0} {
		if set {
			kinds++
		}
	}
	switch {
	case e.failKind != failAny && !e.fail:
		return fmt.Errorf("failure qualifier set without `expect fail`")
	case kinds == 0:
		return fmt.Errorf("eval has no expectation: provide series, a scalar, `expect empty`, or `expect fail`")
	case kinds > 1:
		return fmt.Errorf("conflicting expectations: use exactly one of series, a scalar, `expect empty`, or `expect fail`")
	case e.ordered && len(e.series) == 0:
		return fmt.Errorf("`expect ordered` requires series")
	}
	return nil
}

// expectedSeries is one expected output series: its label set (in `{a="b"}` string form) and
// the expected sample at each step.
type expectedSeries struct {
	labels  string
	samples []sample
}

type expectationsParser struct {
	exp expectations
}

func newExpectationsParser() *expectationsParser {
	return &expectationsParser{}
}

// parse consumes one expectation line: an `expect` annotation (`fail [msg:|regex:]` / `empty` /
// `ordered`), a `{labels} p0 p1 ...` series line, or a bare scalar value.
func (p *expectationsParser) parse(line string) error {
	switch {
	case strings.HasPrefix(line, "expect fail"):
		p.exp.fail = true
		body := strings.TrimSpace(strings.TrimPrefix(line, "expect fail"))
		switch {
		case body == "":
			// Bare `expect fail`: any error satisfies it.
		case strings.HasPrefix(body, "msg:"):
			p.exp.failKind = failMsg
			p.exp.failText = strings.TrimSpace(strings.TrimPrefix(body, "msg:"))
			if p.exp.failText == "" {
				return fmt.Errorf("`expect fail msg:` requires a message substring")
			}
		case strings.HasPrefix(body, "regex:"):
			p.exp.failKind = failRegex
			p.exp.failText = strings.TrimSpace(strings.TrimPrefix(body, "regex:"))
			if p.exp.failText == "" {
				return fmt.Errorf("`expect fail regex:` requires a regex pattern")
			}
		default:
			// A typo'd qualifier (e.g. `mesg:`) must not silently degrade to "any error".
			return fmt.Errorf("unsupported `expect fail` qualifier %q (use `msg:` or `regex:`)", body)
		}
	case line == "expect empty":
		// The result must contain no series.
		p.exp.empty = true
	case line == "expect ordered":
		// Compare the following series positionally rather than as a set (for sort/sort_desc).
		p.exp.ordered = true
	case strings.HasPrefix(line, "expect "):
		// Reject unrecognized `expect` annotations rather than silently skipping them,
		// which would let a script assert something the harness never actually checks.
		return fmt.Errorf("unsupported expect annotation %q", line)
	case strings.HasPrefix(line, "skip "):
		m := reSkip.FindStringSubmatch(line)
		if m == nil {
			return fmt.Errorf(`invalid skip directive %q (use: skip <what> on "<stack>")`, line)
		}
		what, stack := m[1], m[2]
		if what != "values-comparison" {
			return fmt.Errorf("unsupported skip target %q (only %q)", what, "values-comparison")
		}
		if !isKnownStackName(stack) {
			return fmt.Errorf("unknown stack %q in skip directive (known: %s)", stack, strings.Join(stackNames, ", "))
		}
		if p.exp.isValueComparisonSkipped == nil {
			p.exp.isValueComparisonSkipped = map[string]bool{}
		}
		p.exp.isValueComparisonSkipped[stack] = true
	case strings.HasPrefix(line, "{"):
		lbls, samples, err := parseSeriesLine(line)
		if err != nil {
			return err
		}
		p.exp.series = append(p.exp.series, expectedSeries{labels: lbls.String(), samples: samples})
	default:
		v, err := parseFloat(line)
		if err != nil {
			return fmt.Errorf("invalid scalar expectation %q: %w", line, err)
		}
		p.exp.scalar = &v
	}
	return nil
}

// get returns the accumulated expectations.
func (p *expectationsParser) get() expectations {
	return p.exp
}

// parseSeriesLine parses a `{labels} p0 p1 ...` result line into labels and expanded samples.
func parseSeriesLine(line string) (labels.Labels, []sample, error) {
	line = strings.TrimSpace(line)

	// Parse series labels.
	if !strings.HasPrefix(line, "{") {
		return labels.EmptyLabels(), nil, fmt.Errorf("expected series line to start with '{'")
	}
	end := strings.IndexByte(line, '}')
	if end < 0 {
		return labels.EmptyLabels(), nil, fmt.Errorf("unterminated label set")
	}
	lbls, err := parseSeriesLabels(line[:end+1])
	if err != nil {
		return labels.EmptyLabels(), nil, err
	}

	// Parse samples.
	samples, err := parseSamples(strings.Fields(line[end+1:]))
	if err != nil {
		return labels.EmptyLabels(), nil, err
	}
	if len(samples) == 0 {
		return labels.EmptyLabels(), nil, fmt.Errorf("series line %q has no sample values", line)
	}
	return lbls, samples, nil
}

func parseSeriesLabels(s string) (labels.Labels, error) {
	if strings.TrimSpace(s) == "{}" {
		return labels.EmptyLabels(), nil
	}
	// Unlike syntax.ParseLabels, keep empty-value labels instead of dropping them with WithoutEmpty().
	// That normalization exists for write-path hash stability, but a query result can carry a label
	// with an empty value (e.g. a json expression whose path is missing sets `age=""`), and a test
	// must assert `{age=""}` as distinct from an absent `age`.
	return promql_parser.NewParser(promql_parser.Options{}).ParseMetric(s)
}

// parseSamples expands a series of tokens (`5`, `_`, `NaN`, `2+3x4`, `2-1x4`, `2x4`) into samples.
func parseSamples(tokens []string) ([]sample, error) {
	var out []sample
	for _, tok := range tokens {
		if tok == "_" {
			out = append(out, sample{present: false})
			continue
		}
		if idx := strings.IndexByte(tok, 'x'); idx >= 0 {
			expanded, err := expandSamples(tok, idx)
			if err != nil {
				return nil, err
			}
			out = append(out, expanded...)
			continue
		}
		v, err := parseFloat(tok)
		if err != nil {
			return nil, err
		}
		out = append(out, sample{present: true, value: v})
	}
	return out, nil
}

// expandSamples handles the `<base>[±<step>]x<count>` repetition, producing count+1 samples.
func expandSamples(tok string, xIdx int) ([]sample, error) {
	head, tail := tok[:xIdx], tok[xIdx+1:]
	count, err := strconv.Atoi(tail)
	if err != nil {
		return nil, fmt.Errorf("invalid repeat count in %q: %w", tok, err)
	}
	if count < 0 {
		return nil, fmt.Errorf("negative repeat count in %q", tok)
	}
	base, step := head, "0"
	if plus := strings.LastIndexByte(head, '+'); plus > 0 {
		base, step = head[:plus], head[plus+1:]
	} else if minus := strings.LastIndexByte(head, '-'); minus > 0 {
		base, step = head[:minus], head[minus:] // keep the sign
	}
	b, err := parseFloat(base)
	if err != nil {
		return nil, fmt.Errorf("invalid base in %q: %w", tok, err)
	}
	s, err := parseFloat(step)
	if err != nil {
		return nil, fmt.Errorf("invalid step in %q: %w", tok, err)
	}
	out := make([]sample, 0, count+1)
	for i := 0; i <= count; i++ {
		out = append(out, sample{present: true, value: b + float64(i)*s})
	}
	return out, nil
}

func parseFloat(value string) (float64, error) {
	switch value {
	case "NaN":
		return math.NaN(), nil
	case "+Inf", "Inf":
		return math.Inf(1), nil
	case "-Inf":
		return math.Inf(-1), nil
	}
	return strconv.ParseFloat(value, 64)
}

// stripComment removes a trailing `# ...` comment, ignoring `#` inside "..." or `...` quotes.
func stripComment(line string) string {
	var quote byte
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '`':
			quote = c
		case c == '#':
			return strings.TrimRight(line[:i], " \t")
		}
	}
	return line
}
