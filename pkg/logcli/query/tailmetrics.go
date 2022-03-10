package query

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/cespare/xxhash"
	"github.com/fatih/color"
	kitlog "github.com/go-kit/log"
	"github.com/gorilla/websocket"
	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logcli/client"
	"github.com/grafana/loki/pkg/logcli/output"
	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	logqllog "github.com/grafana/loki/pkg/logql/log"
	"github.com/grafana/loki/pkg/util/unmarshal"
	ui "github.com/grafana/termui/v3"
	"github.com/grafana/termui/v3/widgets"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	promql_parser "github.com/prometheus/prometheus/promql/parser"
	"github.com/weaveworks/common/user"
)

// TailMetricQuery connects to the Loki websocket endpoint and tails logs
func (q *Query) TailMetricQuery(delayFor time.Duration, c client.Client, out output.LogOutput) {
	// NOTE: We get metric query.
	// 1. We split log query part of the metric query and make tail request
	// 2. Then we calculate samples from the returned streams (log lines)

	expr, err := logql.ParseExpr(q.QueryString)
	if err != nil {
		log.Fatalf("Tailing logs failed invalid query: %v", err)
	}

	sexpr := expr.(logql.SampleExpr) // We assume only metric query reach here.

	// q.QueryString = rate({cluster="ops-us-east-0", namespace="loki-ops", container="querier"} |= "level=error" [1m])
	// logQ: {cluster="ops-us-east-0", namespace="loki-ops", container="querier"} |= "level=error"
	logQ := sexpr.Selector().String()

	conn, err := c.LiveTailQueryConn(logQ, delayFor, q.Limit, q.Start, q.Quiet)
	if err != nil {
		log.Fatalf("Tailing logs failed: %+v", err)
	}

	go func() {
		stopChan := make(chan os.Signal, 1)
		signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)
		<-stopChan
		if err := conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")); err != nil {
			log.Println("Error closing websocket:", err)
		}
		os.Exit(0)
	}()

	tailResponse := new(loghttp.TailResponse)

	if len(q.IgnoreLabelsKey) > 0 {
		log.Println("Ignoring labels key:", color.RedString(strings.Join(q.IgnoreLabelsKey, ",")))
	}

	if len(q.ShowLabelsKey) > 0 {
		log.Println("Print only labels key:", color.RedString(strings.Join(q.ShowLabelsKey, ",")))
	}

	uic := NewUiController(false, Lines)
	uic.UpdateQuery(q.QueryString)
	uic.Init()
	defer uic.Close()

	// data := make([][]float64, 0)
	do := func() {
		err := unmarshal.ReadTailResponseJSON(tailResponse, conn)
		if err != nil {
			log.Println("Error reading stream:", err)
			return
		}

		series, err := streamsToSeries(loghttp.Streams(tailResponse.Streams).ToProto(), q.QueryString)
		if err != nil {
			panic(err)
		}
		// fmt.Println(series)

		// now := time.Now()

		start := time.Now()
		end := start.Add(1 * time.Hour)

		engine := logql.NewEngine(logql.EngineOpts{}, &tailQuerier{series: series}, logql.NoLimits, kitlog.NewNopLogger())
		params := logql.NewLiteralParams(q.QueryString, start, end, defaultQueryRangeStep(start, end), 1*time.Second, q.resultsDirection(), uint32(q.Limit), nil)

		q := engine.Query(params)

		ctx := context.Background()
		ctx = user.InjectOrgID(ctx, "fake")

		result, err := q.Exec(ctx)
		if err != nil {
			panic(err)
		}

		// err = marshal.WriteQueryResponseJSON(result, os.Stdout)
		// if err != nil {
		// 	panic(err)
		// }

		x := tohttpMatrix(result.Data.(promql.Matrix))

		if len(x) <= 0 {
			return
		}

		xAppended := uic.AppendToMatrix(x)

		uic.UpdateGraph(xAppended)
		uic.Render()
	}

	ticker := time.NewTicker(time.Millisecond * 100)
	uiEvents := ui.PollEvents()

	for stop := false; !stop; {
		select {
		case e := <-uiEvents:
			stop = uic.HandleUiEvent(e)
		case <-ticker.C:
			do()
		}
	}
}

func prettyPrint(matrix promql.Matrix, data [][]float64) {
	// data := make([][]float64, 0)

	for _, s := range matrix {
		if len(s.Points) <= 1 {
			continue
		}
		fv := make([]float64, 0)
		for _, v := range s.Points {
			fv = append(fv, float64(v.V))
		}
		if len(fv) == 1 {
			panic(fv)
		}
		data = append(data, fv)
	}

	// fmt.Println("data", data)

	panel := widgets.NewPlot()
	panel.Title = "Live"
	panel.Data = data
	// width, height := ui.TerminalDimensions()
	panel.SetRect(0, 0, 130, 40)

	ui.Render(panel)

	// bounded buffer
	if len(data) > 10000 {
		data = data[5000:]
	}
}

func streamsToSeries(streams []logproto.Stream, logQL string) ([]logproto.Series, error) {
	expr, err := logql.ParseExpr(logQL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse in LogCLI: %w", err)
	}

	sampleExpr := expr.(logql.SampleExpr) // TODO: panic if i'ts not metric query for now.

	ext, err := sampleExpr.Extractor()
	if err != nil {
		return nil, fmt.Errorf("failed to get extractor for samples in LogCLI: %w", err)
	}

	return processSeries(streams, ext), nil
}

func processSeries(in []logproto.Stream, ex logqllog.SampleExtractor) []logproto.Series {
	resBySeries := map[string]*logproto.Series{}

	for _, stream := range in {
		for _, e := range stream.Entries {
			exs := ex.ForStream(mustParseLabels2(stream.Labels))
			if f, lbs, ok := exs.Process([]byte(e.Line)); ok {
				var s *logproto.Series
				var found bool
				s, found = resBySeries[lbs.String()]
				if !found {
					s = &logproto.Series{Labels: lbs.String(), StreamHash: exs.BaseLabels().Hash()}
					resBySeries[lbs.String()] = s
				}
				s.Samples = append(s.Samples, logproto.Sample{
					Timestamp: e.Timestamp.UnixNano(),
					Value:     f,
					Hash:      xxhash.Sum64([]byte(e.Line)),
				})
			}
		}
	}
	series := []logproto.Series{}
	for _, s := range resBySeries {
		sort.Sort(s)
		series = append(series, *s)
	}
	return series
}

func mustParseLabels2(s string) labels.Labels {
	labels, err := promql_parser.ParseMetric(s)
	if err != nil {
		log.Fatalf("Failed to parse %s", s)
	}

	return labels
}

// A mock querier that know how to get samples for the given query.
type tailQuerier struct {
	series []logproto.Series
}

func (t *tailQuerier) SelectLogs(ctx context.Context, params logql.SelectLogParams) (iter.EntryIterator, error) {
	// NOTE(kavi): our querier used only for samples selection. It's an error to call selectLogs with this querier.
	return nil, nil
}

func (t *tailQuerier) SelectSamples(ctx context.Context, params logql.SelectSampleParams) (iter.SampleIterator, error) {
	its := make([]iter.SampleIterator, 0)

	for _, s := range t.series {
		its = append(its, iter.NewSeriesIterator(s))
	}

	return iter.NewMergeSampleIterator(ctx, its), nil
}

// This method is to duplicate the same logic of `step` value from `start` and `end`
// done on the loki server side.
// https://github.com/grafana/loki/blob/main/pkg/loghttp/params.go
func defaultQueryRangeStep(start, end time.Time) time.Duration {
	step := int(math.Max(math.Floor(end.Sub(start).Seconds()/250), 1))
	return time.Duration(step) * time.Second
}

func tohttpMatrix(pm promql.Matrix) loghttp.Matrix {
	res := make(loghttp.Matrix, 0, len(pm))

	for _, v := range pm {
		values := make([]model.SamplePair, 0)
		if len(v.Points) <= 1 {
			continue
		}
		for _, v := range v.Points {
			values = append(values, model.SamplePair{
				Timestamp: model.Time(v.T),
				Value:     model.SampleValue(v.V),
			})
		}

		metric := make(map[model.LabelName]model.LabelValue)

		for _, v := range v.Metric {
			metric[model.LabelName(v.Name)] = model.LabelValue(v.Value)
		}

		val := model.SampleStream{
			Metric: model.Metric(metric),
			Values: values,
		}
		res = append(res, val)
	}

	return res
}
