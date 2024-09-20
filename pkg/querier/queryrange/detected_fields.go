package queryrange

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/axiomhq/hyperloglog"
	"github.com/dustin/go-humanize"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/loki/pkg/push"
	"github.com/grafana/loki/v3/pkg/logproto"
	logql_log "github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/querier/plan"
	base "github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/prometheus/prometheus/model/labels"
)

func NewDetectedFieldsHandler(
	limitedHandler base.Handler,
	logHandler base.Handler,
	limits Limits,
) base.Middleware {
	return base.MiddlewareFunc(func(next base.Handler) base.Handler {
		return base.HandlerFunc(
			func(ctx context.Context, req base.Request) (base.Response, error) {
				var resp base.Response
				var err error
				switch r := req.(type) {
				case *DetectedFieldsRequest:
					expr, err := syntax.ParseLogSelector(r.Query, true)
					if err != nil {
						return nil, httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error())
					}

					if err := validateMaxEntriesLimits(ctx, r.LineLimit, limits); err != nil {
						return nil, httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error())
					}

					if err := validateMatchers(ctx, limits, expr.Matchers()); err != nil {
						return nil, httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error())
					}

					lokiReq := &LokiRequest{
						Query:     r.GetQuery(),
						Step:      r.GetStep(),
						StartTs:   r.GetStartTs(),
						EndTs:     r.GetEndTs(),
						Direction: logproto.BACKWARD,
						Limit:     r.GetLineLimit(),
						Path:      "/loki/api/v1/query_range",
					}

					lokiReq.Plan = &plan.QueryPlan{
						AST: expr,
					}

					if !expr.HasFilter() {
						resp, err = limitedHandler.Do(ctx, lokiReq)
					}
					resp, err = logHandler.Do(ctx, lokiReq)
					if err != nil {
						return nil, err
					}

					switch re := resp.(type) {
					case *LokiResponse:
						if re.Status != "success" {
							return resp, nil
						}

						detectedFields := ParseDetectedFields(r.FieldLimit, re.Data.Result)

						fields := make([]*logproto.DetectedField, len(detectedFields))
						fieldCount := 0
						for k, v := range detectedFields {
							p := v.Parsers
							if len(p) == 0 {
								p = nil
							}
							fields[fieldCount] = &logproto.DetectedField{
								Label:       k,
								Type:        v.FieldType,
								Cardinality: v.Estimate(),
								Parsers:     p,
							}

							fieldCount++
						}

						return &DetectedFieldsResponse{
							Response: &logproto.DetectedFieldsResponse{
								Fields:     fields,
								FieldLimit: r.GetFieldLimit(),
							},
							Headers: re.Headers,
						}, nil
					}
				default:
					resp, err = next.Do(ctx, req)
				}

				return resp, err
			},
		)
	})
}

type ParsedFields struct {
	Sketch    *hyperloglog.Sketch
	FieldType logproto.DetectedFieldType
	Parsers   []string
}

func newParsedFields(parsers []string) *ParsedFields {
	return &ParsedFields{
		Sketch:    hyperloglog.New(),
		FieldType: logproto.DetectedFieldString,
		Parsers:   parsers,
	}
}

func newParsedLabels() *ParsedFields {
	return &ParsedFields{
		Sketch:    hyperloglog.New(),
		FieldType: logproto.DetectedFieldString,
	}
}

func (p *ParsedFields) Insert(value string) {
	p.Sketch.Insert([]byte(value))
}

func (p *ParsedFields) Estimate() uint64 {
	return p.Sketch.Estimate()
}

func (p *ParsedFields) Marshal() ([]byte, error) {
	return p.Sketch.MarshalBinary()
}

func (p *ParsedFields) DetermineType(value string) {
	p.FieldType = determineType(value)
}

func determineType(value string) logproto.DetectedFieldType {
	if _, err := strconv.ParseInt(value, 10, 64); err == nil {
		return logproto.DetectedFieldInt
	}

	if _, err := strconv.ParseFloat(value, 64); err == nil {
		return logproto.DetectedFieldFloat
	}

	if _, err := strconv.ParseBool(value); err == nil {
		return logproto.DetectedFieldBoolean
	}

	if _, err := time.ParseDuration(value); err == nil {
		return logproto.DetectedFieldDuration
	}

	if _, err := humanize.ParseBytes(value); err == nil {
		return logproto.DetectedFieldBytes
	}

	return logproto.DetectedFieldString
}

func ParseDetectedFields(limit uint32, streams logqlmodel.Streams) map[string]*ParsedFields {
	detectedFields := make(map[string]*ParsedFields, limit)
	fieldCount := uint32(0)
	emtpyparsers := []string{}

	for _, stream := range streams {
		streamLbls, err := syntax.ParseLabels(stream.Labels)
		if err != nil {
			streamLbls = labels.EmptyLabels()
		}

		for _, entry := range stream.Entries {
			structuredMetadata := getStructuredMetadata(entry)
			for k, vals := range structuredMetadata {
				df, ok := detectedFields[k]
				if !ok && fieldCount < limit {
					df = newParsedFields(emtpyparsers)
					detectedFields[k] = df
					fieldCount++
				}

				if df == nil {
					continue
				}

				detectType := true
				for _, v := range vals {
					parsedFields := detectedFields[k]
					if detectType {
						// we don't want to determine the type for every line, so we assume the type in each stream will be the same, and re-detect the type for the next stream
						parsedFields.DetermineType(v)
						detectType = false
					}

					parsedFields.Insert(v)
				}
			}

			streamLbls := logql_log.NewBaseLabelsBuilder().ForLabels(streamLbls, streamLbls.Hash())
			parsedLabels, parsers := parseEntry(entry, streamLbls)
			for k, vals := range parsedLabels {
				df, ok := detectedFields[k]
				if !ok && fieldCount < limit {
					df = newParsedFields(parsers)
					detectedFields[k] = df
					fieldCount++
				}

				if df == nil {
					continue
				}

				for _, parser := range parsers {
					if !slices.Contains(df.Parsers, parser) {
						df.Parsers = append(df.Parsers, parser)
					}
				}

				detectType := true
				for _, v := range vals {
					parsedFields := detectedFields[k]
					if detectType {
						// we don't want to determine the type for every line, so we assume the type in each stream will be the same, and re-detect the type for the next stream
						parsedFields.DetermineType(v)
						detectType = false
					}

					parsedFields.Insert(v)
				}
			}
		}
	}

	return detectedFields
}

func getStructuredMetadata(entry push.Entry) map[string][]string {
	labels := map[string]map[string]struct{}{}
	for _, lbl := range entry.StructuredMetadata {
		if values, ok := labels[lbl.Name]; ok {
			values[lbl.Value] = struct{}{}
		} else {
			labels[lbl.Name] = map[string]struct{}{lbl.Value: {}}
		}
	}

	result := make(map[string][]string, len(labels))
	for lbl, values := range labels {
		vals := make([]string, 0, len(values))
		for v := range values {
			vals = append(vals, v)
		}
		result[lbl] = vals
	}

	return result
}

func parseEntry(entry push.Entry, lbls *logql_log.LabelsBuilder) (map[string][]string, []string) {
	origParsed := getParsedLabels(entry)
	parsed := make(map[string][]string, len(origParsed))

	for lbl, values := range origParsed {
		if lbl == logqlmodel.ErrorLabel || lbl == logqlmodel.ErrorDetailsLabel ||
			lbl == logqlmodel.PreserveErrorLabel {
			continue
		}

		parsed[lbl] = values
	}

	line := entry.Line
	parser := "json"
	jsonParser := logql_log.NewJSONParser()
	_, jsonSuccess := jsonParser.Process(0, []byte(line), lbls)
	if !jsonSuccess || lbls.HasErr() {
		lbls.Reset()

		logFmtParser := logql_log.NewLogfmtParser(false, false)
		parser = "logfmt"
		_, logfmtSuccess := logFmtParser.Process(0, []byte(line), lbls)
		if !logfmtSuccess || lbls.HasErr() {
			return parsed, nil
		}
	}

	parsedLabels := map[string]map[string]struct{}{}
	for lbl, values := range parsed {
		if vals, ok := parsedLabels[lbl]; ok {
			for _, value := range values {
				vals[value] = struct{}{}
			}
		} else {
			parsedLabels[lbl] = map[string]struct{}{}
			for _, value := range values {
				parsedLabels[lbl][value] = struct{}{}
			}
		}
	}

	lblsResult := lbls.LabelsResult().Parsed()
	for _, lbl := range lblsResult {
		if values, ok := parsedLabels[lbl.Name]; ok {
			values[lbl.Value] = struct{}{}
		} else {
			parsedLabels[lbl.Name] = map[string]struct{}{lbl.Value: {}}
		}
	}

	result := make(map[string][]string, len(parsedLabels))
	for lbl, values := range parsedLabels {
		if lbl == logqlmodel.ErrorLabel || lbl == logqlmodel.ErrorDetailsLabel ||
			lbl == logqlmodel.PreserveErrorLabel {
			continue
		}
		vals := make([]string, 0, len(values))
		for v := range values {
			vals = append(vals, v)
		}
		result[lbl] = vals
	}

	return result, []string{parser}
}

func getParsedLabels(entry push.Entry) map[string][]string {
	labels := map[string]map[string]struct{}{}
	for _, lbl := range entry.Parsed {
		if values, ok := labels[lbl.Name]; ok {
			values[lbl.Value] = struct{}{}
		} else {
			labels[lbl.Name] = map[string]struct{}{lbl.Value: {}}
		}
	}

	result := make(map[string][]string, len(labels))
	for lbl, values := range labels {
		vals := make([]string, 0, len(values))
		for v := range values {
			vals = append(vals, v)
		}
		result[lbl] = vals
	}

	return result
}
