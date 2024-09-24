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
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logproto"
	logql_log "github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/querier/plan"
	base "github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/util/httpreq"

	"github.com/grafana/loki/pkg/push"
)

func NewDetectedFieldsHandler(
	limitedHandler base.Handler,
	logHandler base.Handler,
	limits Limits,
) base.Middleware {
	return base.MiddlewareFunc(func(next base.Handler) base.Handler {
		return base.HandlerFunc(
			func(ctx context.Context, req base.Request) (base.Response, error) {
				r, ok := req.(*DetectedFieldsRequest)
				if !ok {
					return nil, httpgrpc.Errorf(
						http.StatusBadRequest,
						"invalid request type, expected *DetectedFieldsRequest",
					)
				}

				resp, err := makeDownstreamRequest(ctx, limits, limitedHandler, logHandler, r)
				if err != nil {
					return nil, err
				}

				re, ok := resp.(*LokiResponse)
				if !ok || re.Status != "success" {
					return resp, nil
				}

				detectedFields := parseDetectedFields(r.FieldLimit, re.Data.Result)
				fields := make([]*logproto.DetectedField, len(detectedFields))
				fieldCount := 0
				for k, v := range detectedFields {
					p := v.parsers
					if len(p) == 0 {
						p = nil
					}
					fields[fieldCount] = &logproto.DetectedField{
						Label:       k,
						Type:        v.fieldType,
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
			})
	})
}

func makeDownstreamRequest(
	ctx context.Context,
	limits Limits,
	limitedHandler, logHandler base.Handler,
	req *DetectedFieldsRequest,
) (base.Response, error) {
	expr, err := syntax.ParseLogSelector(req.Query, true)
	if err != nil {
		return nil, httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error())
	}

	if err := validateMaxEntriesLimits(ctx, req.LineLimit, limits); err != nil {
		return nil, httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error())
	}

	if err := validateMatchers(ctx, limits, expr.Matchers()); err != nil {
		return nil, httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error())
	}

	lokiReq := &LokiRequest{
		Query:     req.GetQuery(),
		Step:      req.GetStep(),
		StartTs:   req.GetStartTs(),
		EndTs:     req.GetEndTs(),
		Direction: logproto.BACKWARD,
		Limit:     req.GetLineLimit(),
		Path:      "/loki/api/v1/query_range",
	}

	lokiReq.Plan = &plan.QueryPlan{
		AST: expr,
	}

	// Note(twhitney): The logic for parsing detected fields relies on the Entry.Parsed field being populated.
	// The behavior of populating Entry.Parsed is different in ingesters and stores.
	// We need to set this header to make sure Entry.Parsed is populated when getting logs from the store.
	// Entries from the head block in the ingester always have the Parsed field populated.
	ctx = httpreq.InjectHeader(
		ctx,
		httpreq.LokiEncodingFlagsHeader,
		(string)(httpreq.FlagCategorizeLabels),
	)
	if expr.HasFilter() {
		return logHandler.Do(ctx, lokiReq)
	}
	return limitedHandler.Do(ctx, lokiReq)
}

type parsedFields struct {
	sketch    *hyperloglog.Sketch
	fieldType logproto.DetectedFieldType
	parsers   []string
}

func newParsedFields(parsers []string) *parsedFields {
	return &parsedFields{
		sketch:    hyperloglog.New(),
		fieldType: logproto.DetectedFieldString,
		parsers:   parsers,
	}
}

func newParsedLabels() *parsedFields {
	return &parsedFields{
		sketch:    hyperloglog.New(),
		fieldType: logproto.DetectedFieldString,
	}
}

func (p *parsedFields) Insert(value string) {
	p.sketch.Insert([]byte(value))
}

func (p *parsedFields) Estimate() uint64 {
	return p.sketch.Estimate()
}

func (p *parsedFields) Marshal() ([]byte, error) {
	return p.sketch.MarshalBinary()
}

func (p *parsedFields) DetermineType(value string) {
	p.fieldType = determineType(value)
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

func parseDetectedFields(limit uint32, streams logqlmodel.Streams) map[string]*parsedFields {
	detectedFields := make(map[string]*parsedFields, limit)
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

			entryLbls := logql_log.NewBaseLabelsBuilder().ForLabels(streamLbls, streamLbls.Hash())
			parsedLabels, parsers := parseEntry(entry, entryLbls)
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
					if !slices.Contains(df.parsers, parser) {
						df.parsers = append(df.parsers, parser)
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

	// if the original query has any parser expressions, then we need to differentiate the
	// original stream labels from any parsed labels
	for name := range origParsed {
		lbls.Del(name)
	}
	streamLbls := lbls.LabelsResult().Stream()
	lblBuilder := lbls.ForLabels(streamLbls, streamLbls.Hash())

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
	_, jsonSuccess := jsonParser.Process(0, []byte(line), lblBuilder)
	if !jsonSuccess || lblBuilder.HasErr() {
		lblBuilder.Reset()

		logFmtParser := logql_log.NewLogfmtParser(false, false)
		parser = "logfmt"
		_, logfmtSuccess := logFmtParser.Process(0, []byte(line), lblBuilder)
		if !logfmtSuccess || lblBuilder.HasErr() {
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

	lblsResult := lblBuilder.LabelsResult().Parsed()
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
