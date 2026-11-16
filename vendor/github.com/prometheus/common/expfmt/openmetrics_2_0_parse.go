// Copyright The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package expfmt

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"math"
	"math/bits"
	"strconv"
	"strings"
	"sync"

	goabnf "github.com/pandatix/go-abnf"
	dto "github.com/prometheus/client_model/go"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/prometheus/common/model"
)

// openMetrics20ABNF is the OpenMetrics 2.0 ABNF grammar (RFC 5234 / RFC 7405).
//
// NOTE: Every OpenMetrics 2.0 metric-descriptor, sample, and eof construct is
// strictly single-line (normal-char excludes LF %x0A). Adding the `line` rule
// allows streaming line-by-line from an io.Reader via goabnf.ParseForest per
// line while keeping the full exposition grammar intact.
const openMetrics20ABNF = `exposition = metricset eof-line

metricset = *(metric-descriptor / sample)

line = metric-descriptor / sample / eof-line
eof-line = HASH SP %s"EOF" [ LF ]

metric-descriptor = HASH SP descriptor-type SP metric-descriptor-name SP metric-type LF
metric-descriptor =/ HASH SP descriptor-help SP metric-descriptor-name SP help-value LF
metric-descriptor =/ HASH SP descriptor-unit SP metric-descriptor-name SP unit-value LF

descriptor-type = %s"TYPE"
descriptor-help = %s"HELP"
descriptor-unit = %s"UNIT"

metric-descriptor-name = metricname / metricname-utf8
help-value = escaped-string
unit-value = *metricname-char

metric-type = %s"counter" / %s"gauge" / %s"histogram" / %s"gaugehistogram" / %s"stateset"
metric-type =/ %s"info" / %s"summary" / %s"unknown"

sample = metricname-and-labels SP value [SP sample-timestamp] [SP start-timestamp] *exemplar LF

value = sample-number / "{" composite-value "}"
sample-number = number

sample-timestamp = timestamp
timestamp = realnumber

; Lowercase st @ timestamp
start-timestamp = %s"st" "@" timestamp

exemplar = SP HASH SP labels-in-braces SP exemplar-value SP exemplar-timestamp
exemplar-value = number
exemplar-timestamp = timestamp

metricname-and-labels = metricname [labels-in-braces] / name-and-labels-in-braces
labels-in-braces = "{" [label *(COMMA label)] "}"
name-and-labels-in-braces = "{" metricname-utf8 *(COMMA label) "}"

label = label-key "=" DQUOTE label-value DQUOTE
label-key = label-name / label-name-utf8
label-name-utf8 = DQUOTE escaped-string-non-empty DQUOTE
label-value = escaped-string

; Number value
number = realnumber
; Case insensitive
number =/ [SIGN] (%i"inf" / %i"infinity")
number =/ %i"nan"

; Real floats
; Leading 0s explicitly okay
realnumber = [SIGN] 1*DIGIT ["." *DIGIT] [ "e" [SIGN] 1*DIGIT ]
realnumber =/ [SIGN] *DIGIT "." 1*DIGIT [ "e" [SIGN] 1*DIGIT ]

; Integers
; Leading 0s explicitly okay
integer = [SIGN] 1*DIGIT
non-negative-integer = ["+"] 1*DIGIT

BS = "\"
COMMA = ","
HASH = "#"
SIGN = "-" / "+"

metricname = metricname-initial-char 0*metricname-char

metricname-char = metricname-initial-char / DIGIT
metricname-initial-char = ALPHA / "_" / ":"
metricname-utf8 = DQUOTE escaped-string-non-empty DQUOTE

label-name = label-name-initial-char *label-name-char

label-name-char = label-name-initial-char / DIGIT
label-name-initial-char = ALPHA / "_"

escaped-string = *escaped-char
escaped-string-non-empty = 1*escaped-char

escaped-char = normal-char
escaped-char =/ BS (DQUOTE / BS / normal-char)

; Any unicode character, except newline, double quote, and backslash
normal-char = %x00-09 / %x0B-21 / %x23-5B / %x5D-D7FF / %xE000-10FFFF

; Composite values
composite-value = histogram-value / gauge-histogram-value / summary-value

; Histograms
histogram-value = h-count "," h-sum "," histogram-buckets
gauge-histogram-value = %s"g" h-count "," %s"g" h-sum "," histogram-buckets

; count:x
h-count = %s"count" ":" number
; sum:f allows real numbers and +-Inf and NaN
h-sum = %s"sum" ":" number

histogram-buckets = classic-buckets / native-buckets [ "," classic-buckets ]

; bucket:[...,+Inf:v] The +Inf bucket is required.
classic-buckets = %s"bucket" ":" "[" [ ch-le-counts "," ] ch-pos-inf-bucket "]"
ch-le-counts = (ch-neg-inf-bucket / ch-le-bucket) *("," ch-le-bucket)
ch-pos-inf-bucket = "+" %s"Inf" ":" number
ch-neg-inf-bucket = "-" %s"Inf" ":" number
ch-le-bucket = realnumber ":" number

; schema:3,zero_threshold:1e-128,zero_count:2,negative_spans:[1:1],negative_buckets:[2],positive_spans:[-3:1,2:2],positive_buckets:[3,1,0]
native-buckets = nh-schema "," nh-zero-threshold "," nh-zero-count [ "," nh-negative-spans "," nh-negative-buckets ] [ "," nh-positive-spans "," nh-positive-buckets ]

; schema:i
nh-schema = %s"schema" ":" integer
; zero_threshold:f
nh-zero-threshold = %s"zero_threshold" ":" realnumber
; zero_count:x
nh-zero-count = %s"zero_count" ":" number
; negative_spans:[1:2,3:4] and positive_spans:[-3:1,2:2]
nh-negative-spans = %s"negative_spans" ":" "[" [nh-spans] "]"
nh-positive-spans = %s"positive_spans" ":" "[" [nh-spans] "]"
; Spans hold offset and length. The offset can start from any index, even
; negative, however subsequent spans can only advance the index, not decrease it.
nh-spans = nh-start-span *("," nh-span)
nh-start-span = integer ":" non-negative-integer
nh-span = non-negative-integer ":" non-negative-integer

; negative_buckets:[1,2,3] and positive_buckets:[1,2,3]
nh-negative-buckets = %s"negative_buckets" ":" "[" [nh-buckets] "]"
nh-positive-buckets = %s"positive_buckets" ":" "[" [nh-buckets] "]"

nh-buckets = number *("," number)

; Summary

; count:12.0,sum:100.0,quantile:[0.9:2.0,0.95:3.0,0.99:20.0]
summary-value = cs-count "," cs-sum "," cs-quantile

; count:x where x is a number
cs-count = %s"count" ":" number
; sum:x where x is a real number or +-Inf or NaN
cs-sum = %s"sum" ":" number
; quantile:[...]
cs-quantile = %s"quantile" ":" "[" [ cs-q-counts ] "]"
cs-q-counts = cs-q-count *("," cs-q-count)
cs-q-count = realnumber ":" number
`

var (
	om20GrammarOnce sync.Once
	om20Grammar     *goabnf.Grammar
	om20GrammarErr  error
)

func getOpenMetrics20Grammar() (*goabnf.Grammar, error) {
	om20GrammarOnce.Do(func() {
		raw := strings.ReplaceAll(openMetrics20ABNF, "\r\n", "\n")
		raw = strings.ReplaceAll(raw, "\n", "\r\n")
		om20Grammar, om20GrammarErr = goabnf.ParseABNF([]byte(raw))
	})
	return om20Grammar, om20GrammarErr
}

// openMetrics20Parser streams OpenMetrics 2.0 text exposition format line-by-line
// using the OpenMetrics 2.0 ABNF grammar.
//
// NOTE: Like openMetricsParser, this parser invokes goabnf.ParseForest on each
// full line without a line-length cap, which has superlinear (roughly quadratic)
// parse time in line length. As documented on NewDecoder, it is intended for
// reference and conformance decoding rather than high-performance or untrusted inputs.
type openMetrics20Parser struct {
	br          *bufio.Reader
	g           *goabnf.Grammar
	initErr     error
	scheme      model.ValidationScheme
	families    map[string]*openMetricsFamilyState
	curFamily   *openMetricsFamilyState
	lineNum     int
	pendingLine []byte
	pendingNode *goabnf.ParseTree
	done        bool
}

func newOpenMetrics20Parser(r io.Reader, scheme model.ValidationScheme) *openMetrics20Parser {
	br, ok := r.(*bufio.Reader)
	if !ok {
		br = bufio.NewReader(r)
	}
	g, err := getOpenMetrics20Grammar()
	if err != nil {
		err = fmt.Errorf("failed to initialize OpenMetrics 2.0 ABNF grammar: %w", err)
	}
	return &openMetrics20Parser{
		br:       br,
		g:        g,
		initErr:  err,
		scheme:   scheme,
		families: make(map[string]*openMetricsFamilyState),
	}
}

func (p *openMetrics20Parser) validationScheme() model.ValidationScheme {
	if p.scheme == model.UnsetValidation {
		return model.UTF8Validation
	}
	return p.scheme
}

func emitOpenMetrics20Family(st *openMetricsFamilyState, v *dto.MetricFamily) {
	if st.mf.Type == nil {
		st.mf.Type = dto.MetricType_UNTYPED.Enum()
	}
	v.Name = st.mf.Name
	v.Help = st.mf.Help
	v.Type = st.mf.Type
	v.Metric = st.mf.Metric
	v.Unit = st.mf.Unit
	st.emitted = true
	st.mf.Metric = nil
}

// next reads from the input stream until the next MetricFamily is complete and
// populates v, or returns io.EOF when # EOF is reached.
func (p *openMetrics20Parser) next(v *dto.MetricFamily) error {
	if p.initErr != nil {
		return p.initErr
	}
	for {
		if p.done {
			return io.EOF
		}

		var (
			line []byte
			node *goabnf.ParseTree
		)
		if p.pendingNode != nil {
			line = p.pendingLine
			node = p.pendingNode
			p.pendingLine = nil
			p.pendingNode = nil
		} else {
			rawLine, readErr := p.br.ReadBytes('\n')
			if len(rawLine) == 0 {
				if errors.Is(readErr, io.EOF) {
					return ParseError{
						Line: max(1, p.lineNum+1),
						Msg:  "data does not end with # EOF",
					}
				}
				return readErr
			}
			if readErr != nil && !errors.Is(readErr, io.EOF) {
				return readErr
			}
			p.lineNum++
			line = rawLine

			forest, err := goabnf.ParseForest(line, p.g, "line")
			if err != nil {
				return err
			}
			if !forest.Valid() {
				return formatABNFLineParseError(p.lineNum, forest.ParseError())
			}
			tree := forest.Tree()
			if tree == nil || len(tree.Children) == 0 {
				return ParseError{Line: p.lineNum, Msg: "failed to extract parse tree"}
			}
			node = tree.Children[0]
		}

		switch node.Rule {
		case "eof-line":
			if _, peekErr := p.br.Peek(1); !errors.Is(peekErr, io.EOF) {
				if peekErr != nil {
					return peekErr
				}
				return ParseError{
					Line: p.lineNum + 1,
					Msg:  "unexpected data after # EOF",
				}
			}
			p.done = true
			if p.curFamily != nil && p.curFamily.hasSamples && !p.curFamily.emitted {
				st := p.curFamily
				p.curFamily = nil
				emitOpenMetrics20Family(st, v)
				return nil
			}
			return io.EOF

		case "metric-descriptor":
			if p.curFamily != nil && !p.curFamily.emitted {
				mfName := extractDescriptorMetricName(line, node)
				if mfName != p.curFamily.mf.GetName() {
					if p.curFamily.hasSamples {
						p.pendingLine = line
						p.pendingNode = node
						st := p.curFamily
						p.curFamily = nil
						emitOpenMetrics20Family(st, v)
						return nil
					}
					p.curFamily.emitted = true
					p.curFamily = nil
				}
			}
			if err := p.handleDescriptor(line, node); err != nil {
				return err
			}

		case "sample":
			if p.curFamily != nil && !p.curFamily.emitted {
				sampleName := extractSampleMetricName(line, node)
				if sampleName != p.curFamily.mf.GetName() {
					if p.curFamily.hasSamples {
						p.pendingLine = line
						p.pendingNode = node
						st := p.curFamily
						p.curFamily = nil
						emitOpenMetrics20Family(st, v)
						return nil
					}
					p.curFamily.emitted = true
					p.curFamily = nil
				}
			}
			if err := p.handleSample(line, node); err != nil {
				return err
			}
		}
	}
}

func extractDescriptorMetricName(input []byte, node *goabnf.ParseTree) string {
	for _, c := range node.Children {
		if c.Rule == "metric-descriptor-name" {
			return extractMetricNameNode(input, c)
		}
	}
	return ""
}

func (p *openMetrics20Parser) getOrCreateDescriptorFamily(name string) (*openMetricsFamilyState, error) {
	if !p.validationScheme().IsValidMetricName(name) {
		return nil, ParseError{Line: p.lineNum, Msg: fmt.Sprintf("invalid metric name %q", name)}
	}
	if p.curFamily != nil && !p.curFamily.emitted && p.curFamily.mf.GetName() == name {
		if p.curFamily.hasSamples {
			return nil, ParseError{Line: p.lineNum, Msg: fmt.Sprintf("metadata for metric %q after samples", name)}
		}
		return p.curFamily, nil
	}
	if prev, exists := p.families[name]; exists {
		if prev.hasSamples {
			return nil, ParseError{Line: p.lineNum, Msg: fmt.Sprintf("metadata for metric %q after samples", name)}
		}
		if prev.emitted {
			return nil, ParseError{Line: p.lineNum, Msg: fmt.Sprintf("interleaved metric family %q", name)}
		}
		return prev, nil
	}
	st := &openMetricsFamilyState{
		mf: &dto.MetricFamily{
			Name: proto.String(name),
		},
	}
	p.families[name] = st
	if p.curFamily == nil || !p.curFamily.hasSamples {
		p.curFamily = st
	}
	return st, nil
}

func (p *openMetrics20Parser) handleDescriptor(input []byte, node *goabnf.ParseTree) error {
	var (
		descKind string
		mfName   string
		valNode  *goabnf.ParseTree
	)
	for _, c := range node.Children {
		switch c.Rule {
		case "descriptor-type", "descriptor-help", "descriptor-unit":
			descKind = c.Rule
		case "metric-descriptor-name":
			mfName = extractMetricNameNode(input, c)
		case "metric-type", "help-value", "unit-value":
			valNode = c
		}
	}

	st, err := p.getOrCreateDescriptorFamily(mfName)
	if err != nil {
		return err
	}

	switch descKind {
	case "descriptor-type":
		if st.hasType {
			return ParseError{Line: p.lineNum, Msg: fmt.Sprintf("second TYPE line for metric name %q", mfName)}
		}
		st.hasType = true
		rawType := string(input[valNode.Start:valNode.End])
		switch rawType {
		case "counter":
			st.mf.Type = dto.MetricType_COUNTER.Enum()
		case "gauge":
			st.mf.Type = dto.MetricType_GAUGE.Enum()
		case "histogram":
			st.mf.Type = dto.MetricType_HISTOGRAM.Enum()
		case "gaugehistogram":
			st.mf.Type = dto.MetricType_GAUGE_HISTOGRAM.Enum()
		case "summary":
			st.mf.Type = dto.MetricType_SUMMARY.Enum()
		case "unknown":
			st.mf.Type = dto.MetricType_UNTYPED.Enum()
		case "info", "stateset":
			// TODO(bwplotka): Expand client_model (dto.MetricType) to support INFO and STATESET metric types.
			return ParseError{Line: p.lineNum, Msg: fmt.Sprintf("unsupported metric type %q", rawType)}
		default:
			return ParseError{Line: p.lineNum, Msg: fmt.Sprintf("unknown metric type %q", rawType)}
		}
	case "descriptor-help":
		if st.hasHelp {
			return ParseError{Line: p.lineNum, Msg: fmt.Sprintf("second HELP line for metric name %q", mfName)}
		}
		st.hasHelp = true
		rawHelp := string(input[valNode.Start:valNode.End])
		if strings.HasSuffix(rawHelp, "\r") {
			return ParseError{Line: p.lineNum, Msg: fmt.Sprintf("unexpected carriage return in HELP for metric %q", mfName)}
		}
		st.mf.Help = proto.String(unescapeOpenMetricsString(rawHelp))
	case "descriptor-unit":
		if st.hasUnit {
			return ParseError{Line: p.lineNum, Msg: fmt.Sprintf("second UNIT line for metric name %q", mfName)}
		}
		st.hasUnit = true
		unit := string(input[valNode.Start:valNode.End])
		if unit != "" {
			st.mf.Unit = proto.String(unit)
		}
	}
	return nil
}

func (p *openMetrics20Parser) resolveSampleFamily(sampleName string) (*openMetricsFamilyState, error) {
	if !p.validationScheme().IsValidMetricName(sampleName) {
		return nil, ParseError{Line: p.lineNum, Msg: fmt.Sprintf("invalid metric name %q", sampleName)}
	}

	if p.curFamily != nil && !p.curFamily.emitted && p.curFamily.mf.GetName() == sampleName {
		if p.curFamily.mf.Type == nil {
			p.curFamily.mf.Type = dto.MetricType_UNTYPED.Enum()
		}
		p.curFamily.hasSamples = true
		return p.curFamily, nil
	}

	if st, exists := p.families[sampleName]; exists {
		if st.emitted {
			return nil, ParseError{Line: p.lineNum, Msg: fmt.Sprintf("interleaved metric family %q", st.mf.GetName())}
		}
		if st.mf.Type == nil {
			st.mf.Type = dto.MetricType_UNTYPED.Enum()
		}
		st.hasSamples = true
		p.curFamily = st
		return st, nil
	}

	st := &openMetricsFamilyState{
		mf: &dto.MetricFamily{
			Name: proto.String(sampleName),
			Type: dto.MetricType_UNTYPED.Enum(),
		},
		hasSamples: true,
	}
	p.families[sampleName] = st
	p.curFamily = st
	return st, nil
}

func (p *openMetrics20Parser) handleSample(input []byte, node *goabnf.ParseTree) error {
	var (
		nameAndLabelsNode *goabnf.ParseTree
		valueNode         *goabnf.ParseTree
		hasTS             bool
		tsStr             string
		hasST             bool
		stStr             string
		exemplarNodes     []*goabnf.ParseTree
	)
	for _, c := range node.Children {
		switch c.Rule {
		case "metricname-and-labels":
			nameAndLabelsNode = c
		case "value":
			valueNode = c
		case "sample-timestamp":
			hasTS = true
			tsStr = string(input[c.Start:c.End])
		case "start-timestamp":
			for _, sc := range c.Children {
				if sc.Rule == "timestamp" {
					hasST = true
					stStr = string(input[sc.Start:sc.End])
				}
			}
		case "exemplar":
			exemplarNodes = append(exemplarNodes, c)
		}
	}

	sampleName, rawLabels := extractNameAndLabels(input, nameAndLabelsNode)
	st, err := p.resolveSampleFamily(sampleName)
	if err != nil {
		return err
	}

	labelPairs, err := p.validateAndBuildLabels(st.mf.GetName(), rawLabels)
	if err != nil {
		return err
	}

	var tsMs *int64
	if hasTS {
		parsedTS, err := parseSampleTimestampMs(tsStr)
		if err != nil {
			return ParseError{Line: p.lineNum, Msg: fmt.Sprintf("invalid timestamp %q: %v", tsStr, err)}
		}
		tsMs = proto.Int64(parsedTS)
	}

	var startTS *timestamppb.Timestamp
	if hasST {
		startTS, err = parseTimestampToProto(stStr)
		if err != nil {
			return ParseError{Line: p.lineNum, Msg: fmt.Sprintf("invalid start timestamp %q: %v", stStr, err)}
		}
	}

	var exemplars []*dto.Exemplar
	if len(exemplarNodes) > 0 {
		exemplars = make([]*dto.Exemplar, 0, len(exemplarNodes))
		for _, en := range exemplarNodes {
			ex, err := p.parseExemplar(input, en)
			if err != nil {
				return err
			}
			exemplars = append(exemplars, ex)
		}
	}

	var (
		scalarNode    *goabnf.ParseTree
		compositeNode *goabnf.ParseTree
	)
	for _, vc := range valueNode.Children {
		switch vc.Rule {
		case "sample-number":
			scalarNode = vc
		case "composite-value":
			compositeNode = vc
		}
	}

	mtype := st.mf.GetType()
	switch mtype {
	case dto.MetricType_COUNTER:
		if scalarNode == nil {
			return ParseError{Line: p.lineNum, Msg: fmt.Sprintf("composite value not supported for counter %q", sampleName)}
		}
		valStr := string(input[scalarNode.Start:scalarNode.End])
		val, err := parseOpenMetricsFloat(valStr)
		if err != nil {
			return ParseError{Line: p.lineNum, Msg: fmt.Sprintf("invalid counter value %q: %v", valStr, err)}
		}
		if math.IsNaN(val) {
			return ParseError{Line: p.lineNum, Msg: fmt.Sprintf("counter sample value must not be NaN for %q", sampleName)}
		}
		if val < 0 {
			return ParseError{Line: p.lineNum, Msg: fmt.Sprintf("counter sample value must not be negative for %q", sampleName)}
		}
		// NOTE: dto.Counter only supports a single Exemplar field in client_model;
		// if multiple exemplars are present on the wire, keep the first one.
		var ex *dto.Exemplar
		if len(exemplars) > 0 {
			ex = exemplars[0]
		}
		st.mf.Metric = append(st.mf.Metric, &dto.Metric{
			Label: labelPairs,
			Counter: &dto.Counter{
				Value:            proto.Float64(val),
				CreatedTimestamp: startTS,
				Exemplar:         ex,
			},
			TimestampMs: tsMs,
		})

	case dto.MetricType_GAUGE:
		if scalarNode == nil {
			return ParseError{Line: p.lineNum, Msg: fmt.Sprintf("composite value not supported for gauge %q", sampleName)}
		}
		if startTS != nil {
			return ParseError{Line: p.lineNum, Msg: fmt.Sprintf("start timestamp not allowed on gauge metric %q", sampleName)}
		}
		if len(exemplars) > 0 {
			return ParseError{Line: p.lineNum, Msg: fmt.Sprintf("exemplar not allowed on gauge metric %q", sampleName)}
		}
		valStr := string(input[scalarNode.Start:scalarNode.End])
		val, err := parseOpenMetricsFloat(valStr)
		if err != nil {
			return ParseError{Line: p.lineNum, Msg: fmt.Sprintf("invalid gauge value %q: %v", valStr, err)}
		}
		st.mf.Metric = append(st.mf.Metric, &dto.Metric{
			Label:       labelPairs,
			Gauge:       &dto.Gauge{Value: proto.Float64(val)},
			TimestampMs: tsMs,
		})

	case dto.MetricType_UNTYPED:
		if scalarNode == nil {
			return ParseError{Line: p.lineNum, Msg: fmt.Sprintf("composite value not supported for untyped %q", sampleName)}
		}
		if startTS != nil {
			return ParseError{Line: p.lineNum, Msg: fmt.Sprintf("start timestamp not allowed on untyped metric %q", sampleName)}
		}
		if len(exemplars) > 0 {
			return ParseError{Line: p.lineNum, Msg: fmt.Sprintf("exemplar not allowed on untyped metric %q", sampleName)}
		}
		valStr := string(input[scalarNode.Start:scalarNode.End])
		val, err := parseOpenMetricsFloat(valStr)
		if err != nil {
			return ParseError{Line: p.lineNum, Msg: fmt.Sprintf("invalid untyped value %q: %v", valStr, err)}
		}
		st.mf.Metric = append(st.mf.Metric, &dto.Metric{
			Label:       labelPairs,
			Untyped:     &dto.Untyped{Value: proto.Float64(val)},
			TimestampMs: tsMs,
		})

	case dto.MetricType_SUMMARY:
		if compositeNode == nil {
			return ParseError{Line: p.lineNum, Msg: fmt.Sprintf("composite value required for metric type summary %q", sampleName)}
		}
		if len(exemplars) > 0 {
			return ParseError{Line: p.lineNum, Msg: fmt.Sprintf("exemplar not allowed on summary metric %q", sampleName)}
		}
		summaryProto, err := parseCompositeSummary(p.lineNum, input, compositeNode, sampleName, labelPairs, startTS)
		if err != nil {
			return err
		}
		st.mf.Metric = append(st.mf.Metric, &dto.Metric{
			Label:       labelPairs,
			Summary:     summaryProto,
			TimestampMs: tsMs,
		})

	case dto.MetricType_HISTOGRAM, dto.MetricType_GAUGE_HISTOGRAM:
		if compositeNode == nil {
			return ParseError{Line: p.lineNum, Msg: fmt.Sprintf("composite value required for metric type %s %q", mtype, sampleName)}
		}
		isGauge := mtype == dto.MetricType_GAUGE_HISTOGRAM
		if isGauge && startTS != nil {
			return ParseError{Line: p.lineNum, Msg: fmt.Sprintf("start timestamp not allowed on gaugehistogram metric %q", sampleName)}
		}
		histProto, err := parseCompositeHistogram(p.lineNum, input, compositeNode, sampleName, labelPairs, isGauge, startTS, exemplars)
		if err != nil {
			return err
		}
		st.mf.Metric = append(st.mf.Metric, &dto.Metric{
			Label:       labelPairs,
			Histogram:   histProto,
			TimestampMs: tsMs,
		})
	}

	return nil
}

func (p *openMetrics20Parser) validateAndBuildLabels(mfName string, rawLabels []parsedLabel) ([]*dto.LabelPair, error) {
	if len(rawLabels) == 0 {
		return nil, nil
	}
	pairs := make([]*dto.LabelPair, 0, len(rawLabels))
	seen := make(map[string]struct{}, len(rawLabels))
	for _, rl := range rawLabels {
		if rl.name == "" {
			return nil, ParseError{Line: p.lineNum, Msg: "label name must not be empty"}
		}
		if rl.name == string(model.MetricNameLabel) {
			return nil, ParseError{Line: p.lineNum, Msg: fmt.Sprintf("label name %q is reserved", model.MetricNameLabel)}
		}
		if !p.validationScheme().IsValidLabelName(rl.name) {
			return nil, ParseError{Line: p.lineNum, Msg: fmt.Sprintf("invalid label name %q", rl.name)}
		}
		if !model.LabelValue(rl.value).IsValid() {
			return nil, ParseError{Line: p.lineNum, Msg: fmt.Sprintf("invalid label value %q", rl.value)}
		}
		if _, dup := seen[rl.name]; dup {
			return nil, ParseError{Line: p.lineNum, Msg: fmt.Sprintf("duplicate label %q for metric %q", rl.name, mfName)}
		}
		seen[rl.name] = struct{}{}
		pairs = append(pairs, &dto.LabelPair{
			Name:  proto.String(rl.name),
			Value: proto.String(rl.value),
		})
	}
	return pairs, nil
}

func (p *openMetrics20Parser) parseExemplar(input []byte, node *goabnf.ParseTree) (*dto.Exemplar, error) {
	om1 := openMetricsParser{scheme: p.validationScheme(), lineNum: p.lineNum}
	return om1.parseExemplar(input, node, false)
}

func parseCompositeSummary(
	lineNum int,
	input []byte,
	compNode *goabnf.ParseTree,
	mfName string,
	labels []*dto.LabelPair,
	startTS *timestamppb.Timestamp,
) (*dto.Summary, error) {
	for _, lp := range labels {
		if lp.GetName() == model.QuantileLabel {
			return nil, ParseError{Line: lineNum, Msg: fmt.Sprintf("summary %q label set must not contain %q label", mfName, model.QuantileLabel)}
		}
	}

	var sumNode *goabnf.ParseTree
	for _, c := range compNode.Children {
		if c.Rule == "summary-value" {
			sumNode = c
			break
		}
	}
	if sumNode == nil {
		return nil, ParseError{Line: lineNum, Msg: fmt.Sprintf("expected summary composite value for %q", mfName)}
	}

	var (
		countStr  string
		sumStr    string
		quantiles []*dto.Quantile
	)
	for _, c := range sumNode.Children {
		switch c.Rule {
		case "cs-count":
			countStr = extractFirstRuleText(input, c, "number")
		case "cs-sum":
			sumStr = extractFirstRuleText(input, c, "number")
		case "cs-quantile":
			var prevQ float64
			for _, qc := range c.Children {
				if qc.Rule != "cs-q-counts" {
					continue
				}
				for i, qpair := range qc.Children {
					if qpair.Rule != "cs-q-count" {
						continue
					}
					qStr := extractFirstRuleText(input, qpair, "realnumber")
					vStr := extractFirstRuleText(input, qpair, "number")
					q, err := parseOpenMetricsFloat(qStr)
					if err != nil || math.IsNaN(q) {
						return nil, ParseError{Line: lineNum, Msg: fmt.Sprintf("invalid quantile %q", qStr)}
					}
					if q < 0 || q > 1 {
						return nil, ParseError{Line: lineNum, Msg: fmt.Sprintf("quantile must be between 0.0 and 1.0, got %g", q)}
					}
					if i > 0 && q <= prevQ {
						return nil, ParseError{Line: lineNum, Msg: fmt.Sprintf("quantiles must be sorted in increasing order: %g <= %g", q, prevQ)}
					}
					prevQ = q

					v, err := parseOpenMetricsFloat(vStr)
					if err != nil {
						return nil, ParseError{Line: lineNum, Msg: fmt.Sprintf("invalid quantile value %q: %v", vStr, err)}
					}
					quantiles = append(quantiles, &dto.Quantile{
						Quantile: proto.Float64(q),
						Value:    proto.Float64(v),
					})
				}
			}
		}
	}

	countUint, ok := parsePlainUint64(countStr)
	if !ok {
		f, err := parseOpenMetricsFloat(countStr)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || f < 0 || f >= math.Exp2(64) || float64(uint64(f)) != f {
			return nil, ParseError{Line: lineNum, Msg: fmt.Sprintf("invalid summary count %q", countStr)}
		}
		countUint = uint64(f)
	}

	// Per the OpenMetrics 2.0 spec, summary sum must not be NaN (unlike
	// histogram sum, which can be NaN).
	sumVal, err := parseOpenMetricsFloat(sumStr)
	if err != nil || math.IsNaN(sumVal) {
		return nil, ParseError{Line: lineNum, Msg: fmt.Sprintf("invalid summary sum %q", sumStr)}
	}

	return &dto.Summary{
		SampleCount:      proto.Uint64(countUint),
		SampleSum:        proto.Float64(sumVal),
		Quantile:         quantiles,
		CreatedTimestamp: startTS,
	}, nil
}

func parseCompositeHistogram(
	lineNum int,
	input []byte,
	compNode *goabnf.ParseTree,
	mfName string,
	labels []*dto.LabelPair,
	isGauge bool,
	startTS *timestamppb.Timestamp,
	exemplars []*dto.Exemplar,
) (*dto.Histogram, error) {
	expectedRule := "histogram-value"
	if isGauge {
		expectedRule = "gauge-histogram-value"
	}

	var hNode *goabnf.ParseTree
	for _, c := range compNode.Children {
		if c.Rule == expectedRule {
			hNode = c
			break
		}
	}
	if hNode == nil {
		return nil, ParseError{Line: lineNum, Msg: fmt.Sprintf("expected %s for %q", expectedRule, mfName)}
	}

	var (
		countStr    string
		sumStr      string
		bucketsNode *goabnf.ParseTree
	)
	for _, c := range hNode.Children {
		switch c.Rule {
		case "h-count":
			countStr = extractFirstRuleText(input, c, "number")
		case "h-sum":
			sumStr = extractFirstRuleText(input, c, "number")
		case "histogram-buckets":
			bucketsNode = c
		}
	}

	countFloat, err := parseOpenMetricsFloat(countStr)
	if err != nil || math.IsNaN(countFloat) {
		return nil, ParseError{Line: lineNum, Msg: fmt.Sprintf("invalid histogram count %q", countStr)}
	}
	if countFloat < 0 {
		if isGauge {
			return nil, ParseError{Line: lineNum, Msg: "gcount must not be negative"}
		}
		return nil, ParseError{Line: lineNum, Msg: "count must not be negative"}
	}
	countUint, countIsInt := parsePlainUint64(countStr)

	sumFloat, err := parseOpenMetricsFloat(sumStr)
	if err != nil {
		return nil, ParseError{Line: lineNum, Msg: fmt.Sprintf("invalid histogram sum %q: %v", sumStr, err)}
	}

	var (
		classicNode *goabnf.ParseTree
		nativeNode  *goabnf.ParseTree
	)
	for _, c := range bucketsNode.Children {
		switch c.Rule {
		case "classic-buckets":
			classicNode = c
		case "native-buckets":
			nativeNode = c
		}
	}

	// NOTE: OpenMetrics 2.0 has no distinct wire tag for integer vs float histograms,
	// and writeFloat encodes whole-number floats without a trailing ".0". We classify
	// a histogram as a FloatHistogram iff any count, zero_count, or bucket count token
	// is not a plain integer (e.g. contains a decimal point, exponent, or "+Inf").
	// Consequently, a FloatHistogram whose counts and buckets are all whole integers
	// on the wire decodes as an integer Histogram.
	isFloatHistogram := !countIsInt
	h := &dto.Histogram{
		SampleSum:        proto.Float64(sumFloat),
		CreatedTimestamp: startTS,
		Exemplars:        exemplars,
	}

	type parsedClassicBucket struct {
		ub    float64
		fVal  float64
		uVal  uint64
		isInt bool
	}
	var classicBuckets []parsedClassicBucket

	if classicNode != nil {
		for _, lp := range labels {
			if lp.GetName() == model.BucketLabel {
				return nil, ParseError{Line: lineNum, Msg: fmt.Sprintf("histogram %q with classic buckets must not contain %q label", mfName, model.BucketLabel)}
			}
		}

		var extractBucket func(n *goabnf.ParseTree) error
		extractBucket = func(n *goabnf.ParseTree) error {
			for _, c := range n.Children {
				switch c.Rule {
				case "ch-le-counts":
					if err := extractBucket(c); err != nil {
						return err
					}
				case "ch-neg-inf-bucket":
					vStr := extractFirstRuleText(input, c, "number")
					fv, err := parseOpenMetricsFloat(vStr)
					if err != nil || math.IsNaN(fv) {
						return ParseError{Line: lineNum, Msg: fmt.Sprintf("invalid bucket value %q", vStr)}
					}
					if fv < 0 {
						return ParseError{Line: lineNum, Msg: "classic bucket value must not be negative"}
					}
					uv, isInt := parsePlainUint64(vStr)
					if !isInt {
						isFloatHistogram = true
					}
					classicBuckets = append(classicBuckets, parsedClassicBucket{
						ub: math.Inf(-1), fVal: fv, uVal: uv, isInt: isInt,
					})
				case "ch-le-bucket":
					ubStr := extractFirstRuleText(input, c, "realnumber")
					vStr := extractFirstRuleText(input, c, "number")
					ub, err := parseOpenMetricsFloat(ubStr)
					if err != nil || math.IsNaN(ub) {
						return ParseError{Line: lineNum, Msg: fmt.Sprintf("invalid bucket threshold %q", ubStr)}
					}
					fv, err := parseOpenMetricsFloat(vStr)
					if err != nil || math.IsNaN(fv) {
						return ParseError{Line: lineNum, Msg: fmt.Sprintf("invalid bucket value %q", vStr)}
					}
					if fv < 0 {
						return ParseError{Line: lineNum, Msg: "classic bucket value must not be negative"}
					}
					uv, isInt := parsePlainUint64(vStr)
					if !isInt {
						isFloatHistogram = true
					}
					classicBuckets = append(classicBuckets, parsedClassicBucket{
						ub: ub, fVal: fv, uVal: uv, isInt: isInt,
					})
				case "ch-pos-inf-bucket":
					vStr := extractFirstRuleText(input, c, "number")
					fv, err := parseOpenMetricsFloat(vStr)
					if err != nil || math.IsNaN(fv) {
						return ParseError{Line: lineNum, Msg: fmt.Sprintf("invalid bucket value %q", vStr)}
					}
					if fv < 0 {
						return ParseError{Line: lineNum, Msg: "classic bucket value must not be negative"}
					}
					uv, isInt := parsePlainUint64(vStr)
					if !isInt {
						isFloatHistogram = true
					}
					classicBuckets = append(classicBuckets, parsedClassicBucket{
						ub: math.Inf(1), fVal: fv, uVal: uv, isInt: isInt,
					})
				}
			}
			return nil
		}
		if err := extractBucket(classicNode); err != nil {
			return nil, err
		}
	}

	var (
		zeroCountFloat float64
		zeroCountUint  uint64
		negBucketStrs  []string
		posBucketStrs  []string
	)

	if nativeNode != nil {
		for _, c := range nativeNode.Children {
			switch c.Rule {
			case "nh-schema":
				sStr := extractFirstRuleText(input, c, "integer")
				schemaVal, err := strconv.ParseInt(sStr, 10, 64)
				if err != nil || schemaVal < -4 || schemaVal > 8 {
					return nil, ParseError{Line: lineNum, Msg: fmt.Sprintf("schema must be between -4 and 8, got %q", sStr)}
				}
				h.Schema = proto.Int32(int32(schemaVal))

			case "nh-zero-threshold":
				ztStr := extractFirstRuleText(input, c, "realnumber")
				ztVal, err := parseOpenMetricsFloat(ztStr)
				if err != nil || math.IsNaN(ztVal) || math.IsInf(ztVal, 0) || ztVal < 0 {
					return nil, ParseError{Line: lineNum, Msg: fmt.Sprintf("zero_threshold must be a non-negative, finite number, got %q", ztStr)}
				}
				h.ZeroThreshold = proto.Float64(ztVal)

			case "nh-zero-count":
				zcStr := extractFirstRuleText(input, c, "number")
				zcVal, err := parseOpenMetricsFloat(zcStr)
				if err != nil || math.IsNaN(zcVal) {
					return nil, ParseError{Line: lineNum, Msg: fmt.Sprintf("invalid zero_count %q", zcStr)}
				}
				if zcVal < 0 {
					return nil, ParseError{Line: lineNum, Msg: "zero_count must not be negative"}
				}
				zeroCountFloat = zcVal
				var zcIsInt bool
				zeroCountUint, zcIsInt = parsePlainUint64(zcStr)
				if !zcIsInt {
					isFloatHistogram = true
				}

			case "nh-negative-spans":
				spans, err := parseNativeSpans(lineNum, input, c)
				if err != nil {
					return nil, err
				}
				h.NegativeSpan = spans

			case "nh-positive-spans":
				spans, err := parseNativeSpans(lineNum, input, c)
				if err != nil {
					return nil, err
				}
				h.PositiveSpan = spans

			case "nh-negative-buckets":
				negBucketStrs = extractNumberList(input, c)
				for _, bs := range negBucketStrs {
					if _, ok := parsePlainInt64(bs); !ok {
						isFloatHistogram = true
					}
				}

			case "nh-positive-buckets":
				posBucketStrs = extractNumberList(input, c)
				for _, bs := range posBucketStrs {
					if _, ok := parsePlainInt64(bs); !ok {
						isFloatHistogram = true
					}
				}
			}
		}
	}

	if isFloatHistogram {
		h.SampleCountFloat = proto.Float64(countFloat)
	} else {
		h.SampleCount = proto.Uint64(countUint)
	}

	if nativeNode != nil {
		if isFloatHistogram {
			h.ZeroCountFloat = proto.Float64(zeroCountFloat)
		} else {
			h.ZeroCount = proto.Uint64(zeroCountUint)
		}

		if err := populateNativeBuckets(lineNum, h, isFloatHistogram, negBucketStrs, posBucketStrs); err != nil {
			return nil, err
		}
	}

	if len(classicBuckets) > 0 {
		h.Bucket = make([]*dto.Bucket, 0, len(classicBuckets))
		for i, cb := range classicBuckets {
			if i > 0 {
				if cb.ub <= classicBuckets[i-1].ub {
					return nil, ParseError{Line: lineNum, Msg: "classic histogram buckets must be sorted in increasing order"}
				}
				if cb.fVal < classicBuckets[i-1].fVal {
					return nil, ParseError{Line: lineNum, Msg: fmt.Sprintf("classic bucket counts must be monotonically increasing: %g < %g", cb.fVal, classicBuckets[i-1].fVal)}
				}
			}
			if math.IsInf(cb.ub, +1) {
				if isFloatHistogram {
					if cb.fVal != countFloat {
						return nil, ParseError{Line: lineNum, Msg: fmt.Sprintf("classic bucket +Inf count (%g) does not match sample count (%g)", cb.fVal, countFloat)}
					}
				} else if cb.uVal != countUint {
					return nil, ParseError{Line: lineNum, Msg: fmt.Sprintf("classic bucket +Inf count (%d) does not match sample count (%d)", cb.uVal, countUint)}
				}
			}
			b := &dto.Bucket{
				UpperBound: proto.Float64(cb.ub),
			}
			if isFloatHistogram {
				b.CumulativeCountFloat = proto.Float64(cb.fVal)
			} else {
				b.CumulativeCount = proto.Uint64(cb.uVal)
			}
			h.Bucket = append(h.Bucket, b)
		}
	}

	return h, nil
}

func parseNativeSpans(lineNum int, input []byte, node *goabnf.ParseTree) ([]*dto.BucketSpan, error) {
	var spans []*dto.BucketSpan
	for _, c := range node.Children {
		if c.Rule != "nh-spans" {
			continue
		}
		for _, sc := range c.Children {
			if sc.Rule != "nh-start-span" && sc.Rule != "nh-span" {
				continue
			}
			var offStr, lenStr string
			for _, part := range sc.Children {
				switch part.Rule {
				case "integer":
					offStr = string(input[part.Start:part.End])
				case "non-negative-integer":
					if offStr == "" && sc.Rule == "nh-span" {
						offStr = string(input[part.Start:part.End])
					} else {
						lenStr = string(input[part.Start:part.End])
					}
				}
			}
			off, err := strconv.ParseInt(offStr, 10, 32)
			if err != nil {
				return nil, ParseError{Line: lineNum, Msg: fmt.Sprintf("invalid span offset %q: %v", offStr, err)}
			}
			length, err := strconv.ParseUint(strings.TrimPrefix(lenStr, "+"), 10, 32)
			if err != nil {
				return nil, ParseError{Line: lineNum, Msg: fmt.Sprintf("invalid span length %q: %v", lenStr, err)}
			}
			spans = append(spans, &dto.BucketSpan{
				Offset: proto.Int32(int32(off)),
				Length: proto.Uint32(uint32(length)),
			})
		}
	}
	return spans, nil
}

func populateNativeBuckets(
	lineNum int,
	h *dto.Histogram,
	isFloat bool,
	negStrs, posStrs []string,
) error {
	var negSpanLen uint64
	for _, s := range h.NegativeSpan {
		negSpanLen += uint64(s.GetLength())
	}
	if negSpanLen != uint64(len(negStrs)) {
		return ParseError{Line: lineNum, Msg: fmt.Sprintf("negative_spans length (%d) does not match negative_buckets count (%d)", negSpanLen, len(negStrs))}
	}

	var posSpanLen uint64
	for _, s := range h.PositiveSpan {
		posSpanLen += uint64(s.GetLength())
	}
	if posSpanLen != uint64(len(posStrs)) {
		return ParseError{Line: lineNum, Msg: fmt.Sprintf("positive_spans length (%d) does not match positive_buckets count (%d)", posSpanLen, len(posStrs))}
	}

	if isFloat {
		// NOTE: For float native histograms, we intentionally omit the exact
		// bucket-observation-sum vs sample-count check to match Prometheus's
		// histogram.FloatHistogram.Validate(). Summing many scaled IEEE-754 float64
		// bucket counts is subject to rounding drift and would cause false-positive
		// rejections, whereas classic histogram "+Inf == count" compares two single
		// cumulative numbers directly.
		if len(negStrs) > 0 {
			h.NegativeCount = make([]float64, 0, len(negStrs))
			for _, s := range negStrs {
				fv, err := parseOpenMetricsFloat(s)
				if err != nil || math.IsNaN(fv) || fv < 0 {
					return ParseError{Line: lineNum, Msg: fmt.Sprintf("invalid negative_buckets value %q", s)}
				}
				h.NegativeCount = append(h.NegativeCount, fv)
			}
		}
		if len(posStrs) > 0 {
			h.PositiveCount = make([]float64, 0, len(posStrs))
			for _, s := range posStrs {
				fv, err := parseOpenMetricsFloat(s)
				if err != nil || math.IsNaN(fv) || fv < 0 {
					return ParseError{Line: lineNum, Msg: fmt.Sprintf("invalid positive_buckets value %q", s)}
				}
				h.PositiveCount = append(h.PositiveCount, fv)
			}
		}
		return nil
	}

	var negTotal, posTotal uint64
	if len(negStrs) > 0 {
		h.NegativeDelta = make([]int64, 0, len(negStrs))
		var prev int64
		for _, s := range negStrs {
			iv, ok := parsePlainInt64(s)
			if !ok || iv < 0 {
				return ParseError{Line: lineNum, Msg: fmt.Sprintf("invalid negative_buckets value %q", s)}
			}
			var carry uint64
			negTotal, carry = bits.Add64(negTotal, uint64(iv), 0)
			if carry != 0 {
				return ParseError{Line: lineNum, Msg: "native histogram bucket observations overflow uint64"}
			}
			h.NegativeDelta = append(h.NegativeDelta, iv-prev)
			prev = iv
		}
	}
	if len(posStrs) > 0 {
		h.PositiveDelta = make([]int64, 0, len(posStrs))
		var prev int64
		for _, s := range posStrs {
			iv, ok := parsePlainInt64(s)
			if !ok || iv < 0 {
				return ParseError{Line: lineNum, Msg: fmt.Sprintf("invalid positive_buckets value %q", s)}
			}
			var carry uint64
			posTotal, carry = bits.Add64(posTotal, uint64(iv), 0)
			if carry != 0 {
				return ParseError{Line: lineNum, Msg: "native histogram bucket observations overflow uint64"}
			}
			h.PositiveDelta = append(h.PositiveDelta, iv-prev)
			prev = iv
		}
	}
	if h.SampleCount != nil {
		obs, c1 := bits.Add64(h.GetZeroCount(), negTotal, 0)
		obs, c2 := bits.Add64(obs, posTotal, c1)
		if c2 != 0 {
			return ParseError{Line: lineNum, Msg: "native histogram bucket observations overflow uint64"}
		}
		if math.IsNaN(h.GetSampleSum()) {
			if obs > h.GetSampleCount() {
				return ParseError{Line: lineNum, Msg: fmt.Sprintf("native histogram bucket observations (%d) exceed sample count (%d)", obs, h.GetSampleCount())}
			}
		} else if obs != h.GetSampleCount() {
			return ParseError{Line: lineNum, Msg: fmt.Sprintf("native histogram bucket observations (%d) do not match sample count (%d)", obs, h.GetSampleCount())}
		}
	}
	return nil
}

func extractFirstRuleText(input []byte, node *goabnf.ParseTree, rule string) string {
	for _, c := range node.Children {
		if c.Rule == rule {
			return string(input[c.Start:c.End])
		}
	}
	return ""
}

func extractNumberList(input []byte, node *goabnf.ParseTree) []string {
	var out []string
	for _, c := range node.Children {
		if c.Rule != "nh-buckets" {
			continue
		}
		for _, nc := range c.Children {
			if nc.Rule == "number" {
				out = append(out, string(input[nc.Start:nc.End]))
			}
		}
	}
	return out
}

func parsePlainUint64(s string) (uint64, bool) {
	if s == "" || strings.ContainsAny(s, ".eE") {
		return 0, false
	}
	s = strings.TrimPrefix(s, "+")
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

func parsePlainInt64(s string) (int64, bool) {
	if s == "" || strings.ContainsAny(s, ".eE") {
		return 0, false
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}
