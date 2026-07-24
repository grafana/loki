package logqltest

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

// epoch is the base time the script's relative timestamps are added to. Timestamps in a
// script (`@ 10s`, `eval instant at 60s`) are durations offset from this base.
var epoch = time.Unix(0, 0).UTC()

var (
	reInstant          = regexp.MustCompile(`^at\s+(\S+)\s+(.+)$`)
	reRange            = regexp.MustCompile(`^from\s+(\S+)\s+to\s+(\S+)\s+step\s+(\S+)\s+(.+)$`)
	reAt               = regexp.MustCompile(`@\s*(\S+)`)
	reRepeat           = regexp.MustCompile(`\[repeat every\s+(\S+)\s+for\s+(\d+)\]`)
	reMetadata         = regexp.MustCompile(`\[metadata\s+(.*?)\]`)
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
	// Parse the log stream labels.
	streamLabels, rest, err := splitStreamLabels(line)
	if err != nil {
		return err
	}

	// Parse the log message.
	message, rest, err := splitQuoted(rest)
	if err != nil {
		return err
	}

	// Parse the timestamp.
	m := reAt.FindStringSubmatch(rest)
	if m == nil {
		return fmt.Errorf("missing '@ <start>' timestamp")
	}
	start, err := time.ParseDuration(m[1])
	if err != nil {
		return fmt.Errorf("invalid start %q: %w", m[1], err)
	}

	// Parse the repeat config (if any).
	step := time.Duration(0)
	count := 1
	if r := reRepeat.FindStringSubmatch(rest); r != nil {
		if step, err = time.ParseDuration(r[1]); err != nil {
			return fmt.Errorf("invalid repeat step %q: %w", r[1], err)
		}
		if count, err = strconv.Atoi(r[2]); err != nil {
			return fmt.Errorf("invalid repeat count %q: %w", r[2], err)
		}
	}

	// Parse the metadata (if any).
	metadata := parseMetadata(rest)

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

// splitQuoted returns the first double-quoted string in s and the remainder after it.
func splitQuoted(s string) (text, rest string, err error) {
	start := strings.IndexByte(s, '"')
	if start < 0 {
		return "", "", fmt.Errorf("missing quoted log line")
	}
	end := strings.IndexByte(s[start+1:], '"')
	if end < 0 {
		return "", "", fmt.Errorf("unterminated quoted log line")
	}
	end += start + 1
	return s[start+1 : end], s[end+1:], nil
}

func parseMetadata(rest string) []logproto.LabelAdapter {
	m := reMetadata.FindStringSubmatch(rest)
	if m == nil {
		return nil
	}
	var out []logproto.LabelAdapter
	for _, kv := range reMetadataKeyValue.FindAllStringSubmatch(m[1], -1) {
		key := kv[1]
		if key == "" {
			key = kv[2]
		}
		out = append(out, logproto.LabelAdapter{Name: key, Value: kv[3]})
	}
	return out
}

type evalCmd struct {
	instant          bool
	ts               time.Duration // instant queries
	start, end, step time.Duration // range queries
	query            string
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
		return evalCmd{start: start, end: end, step: step, query: strings.TrimSpace(m[4])}, nil
	default:
		return evalCmd{}, fmt.Errorf("expected 'instant' or 'range' after eval: %q", line)
	}
}

type sample struct {
	present bool
	value   float64
}

// expectations is the parsed expected result of an `eval` command: either a failure
// assertion, a scalar value, or a set of series (for vector/matrix results).
type expectations struct {
	fail     bool
	failKind string // "", "msg", or "regex"
	failText string
	scalar   *float64
	series   []expectedSeries
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

// parse consumes one expectation line: an `expect fail [msg:|regex:]` assertion, a
// `{labels} p0 p1 ...` series line, or a bare scalar value.
func (p *expectationsParser) parse(line string) error {
	switch {
	case strings.HasPrefix(line, "expect fail"):
		p.exp.fail = true
		body := strings.TrimSpace(strings.TrimPrefix(line, "expect fail"))
		switch {
		case strings.HasPrefix(body, "msg:"):
			p.exp.failKind = "msg"
			p.exp.failText = strings.TrimSpace(strings.TrimPrefix(body, "msg:"))
		case strings.HasPrefix(body, "regex:"):
			p.exp.failKind = "regex"
			p.exp.failText = strings.TrimSpace(strings.TrimPrefix(body, "regex:"))
		}
	case strings.HasPrefix(line, "expect "):
		// Reject unrecognized `expect` annotations rather than silently skipping them,
		// which would let a script assert something the harness never actually checks.
		return fmt.Errorf("unsupported expect annotation %q", line)
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
	return lbls, samples, nil
}

func parseSeriesLabels(s string) (labels.Labels, error) {
	if strings.TrimSpace(s) == "{}" {
		return labels.EmptyLabels(), nil
	}
	return syntax.ParseLabels(s)
}

// parseSamples expands a series of tokens (`5`, `_`, `NaN`, `2+3x4`, `2x4`) into samples.
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

// expandSamples handles the `<base>[+<step>]x<count>` repetition, producing count+1 samples.
func expandSamples(tok string, xIdx int) ([]sample, error) {
	head, tail := tok[:xIdx], tok[xIdx+1:]
	count, err := strconv.Atoi(tail)
	if err != nil {
		return nil, fmt.Errorf("invalid repeat count in %q: %w", tok, err)
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
