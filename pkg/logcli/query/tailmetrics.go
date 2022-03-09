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
	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
	kitlog "github.com/go-kit/log"
	"github.com/gorilla/websocket"
	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logcli/client"
	"github.com/grafana/loki/pkg/logcli/output"
	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	logqllog "github.com/grafana/loki/pkg/logql/log"
	"github.com/grafana/loki/pkg/util/marshal"
	"github.com/grafana/loki/pkg/util/unmarshal"
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

	// if err := ui.Init(); err != nil {
	// 	panic(err)
	// }
	// defer ui.Close()

	for {
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

		engine := logql.NewEngine(logql.EngineOpts{}, &tailQuerier{series: series}, logql.NoLimits, kitlog.NewNopLogger())
		params := logql.NewLiteralParams(q.QueryString, q.Start, time.Now(), defaultQueryRangeStep(q.Start, time.Now()), q.Interval, q.resultsDirection(), uint32(q.Limit), nil)

		q := engine.Query(params)

		ctx := context.Background()
		ctx = user.InjectOrgID(ctx, "fake")

		result, err := q.Exec(ctx)
		if err != nil {
			panic(err)
		}

		err = marshal.WriteQueryResponseJSON(result, os.Stdout)
		if err != nil {
			panic(err)
		}

		// prettyPrint(result.Data.(promql.Matrix))

		// uiEvents := ui.PollEvents()
		// select {
		// case e := <-uiEvents:
		// 	if e.ID == "q" || e.ID == "<C-c>" {
		// 		return
		// 	}
		// default:
		// 	continue

		// }

		time.Sleep(3 * time.Second)
	}
}

func prettyPrint(matrix promql.Matrix) {
	data := make([][]float64, 0)

	for _, s := range matrix {
		fv := make([]float64, len(s.Points))
		for i, v := range s.Points {
			fv[i] = float64(v.V)
		}
		data = append(data, fv)
	}

	fmt.Fprintf(os.Stdout, "%+v\n", data)

	panel := widgets.NewPlot()
	panel.Title = "Live"
	panel.Data = data
	width, height := ui.TerminalDimensions()
	panel.SetRect(0, 0, width, height)

	ui.Render(panel)
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
