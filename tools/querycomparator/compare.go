package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"time"

	"github.com/alecthomas/kingpin/v2"
	"github.com/go-kit/log/level"
	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/prometheus/prometheus/model/labels"
	"golang.org/x/sync/errgroup"
)

// addCompareCommand adds the compare command to the application
func addCompareCommand(app *kingpin.Application) {
	var cfg Config
	var host1, host2 string

	cmd := app.Command("compare", "Compare query results between two remote Loki instances by executing queries against them and dumping the output json.")
	cmd.Flag("bucket", "Remote bucket name").Required().StringVar(&cfg.Bucket)
	cmd.Flag("org-id", "Organization ID").Required().StringVar(&cfg.OrgID)
	cmd.Flag("start", "Start time (RFC3339 format)").Required().StringVar(&cfg.Start)
	cmd.Flag("end", "End time (RFC3339 format)").Required().StringVar(&cfg.End)
	cmd.Flag("query", "LogQL query to execute").Required().StringVar(&cfg.Query)
	cmd.Flag("limit", "Maximum number of entries to return").Default("100").IntVar(&cfg.Limit)
	cmd.Flag("host1", "First Loki host").Default("localhost:3101").StringVar(&host1)
	cmd.Flag("host2", "Second Loki host").Default("localhost:3102").StringVar(&host2)

	cmd.Action(func(_ *kingpin.ParseContext) error {
		storageBucket = cfg.Bucket
		orgID = cfg.OrgID

		parsed, err := parseTimeConfig(&cfg)
		if err != nil {
			return err
		}

		params, err := logql.NewLiteralParams(cfg.Query, parsed.StartTime, parsed.EndTime, 0, 0, logproto.BACKWARD, uint32(cfg.Limit), nil, nil)
		if err != nil {
			return err
		}

		return doComparison(params, host1, host2)
	})
}

// doComparison compares query resultsu from executing a loki http query against two hosts
func doComparison(params logql.LiteralParams, host1, host2 string) error {
	level.Info(logger).Log("msg", "executing comparison", "start", params.Start().Format(time.RFC3339), "end", params.End().Format(time.RFC3339))
	hosts := []string{host1, host2}

	g, _ := errgroup.WithContext(context.Background())
	responses := make([]loghttp.QueryResponseData, len(hosts))

	for i, host := range hosts {
		g.Go(func() error {
			respData, err := doRemoteQuery(host, params.QueryString(), params.Start(), params.End(), int(params.Limit()))
			if err != nil {
				return err
			}
			level.Info(logger).Log("host", host, "total_entries", respData.Statistics.Summary.TotalEntriesReturned, "results_file", fmt.Sprintf("host-%s.json", host))
			responses[i] = respData
			return nil
		})
	}
	err := g.Wait()
	if err != nil {
		return err
	}

	typ := responses[0].ResultType
	if responses[1].ResultType != typ {
		return fmt.Errorf("result types mismatch: %s != %s", typ, responses[1].ResultType)
	}

	switch typ {
	case loghttp.ResultTypeStream:
		results := make([]loghttp.Streams, len(responses))
		for i, response := range responses {
			results[i] = response.Result.(loghttp.Streams)
		}
		return checkLogStreams(results)

	case loghttp.ResultTypeMatrix:
		results := make([]loghttp.Matrix, len(responses))
		for i, response := range responses {
			results[i] = response.Result.(loghttp.Matrix)
		}
		return checkLogMatrix(results)
	default:
		return fmt.Errorf("unsupported result type: %s", typ)
	}
}

func checkLogStreams(results []loghttp.Streams) error {
	streamsInResponses := make([][]string, len(results))
	chunkEntries := make(map[string][]loghttp.Entry)
	dataobjEntries := make(map[string][]loghttp.Entry)

	for i, result := range results {
		streams := result
		uniq := make(map[string]struct{})
		for _, stream := range streams {
			lbls, err := syntax.ParseLabels(stream.Labels.String())
			if err != nil {
				return fmt.Errorf("failed to parse labels: %w", err)
			}
			lblsMap := lbls.Map()
			newLbls := labels.FromMap(lblsMap)
			if i == 0 {
				chunkEntries[newLbls.String()] = stream.Entries
			} else {
				dataobjEntries[newLbls.String()] = stream.Entries
			}
			if _, ok := uniq[newLbls.String()]; ok {
				continue
			}
			uniq[newLbls.String()] = struct{}{}
			streamsInResponses[i] = append(streamsInResponses[i], newLbls.String())
		}
	}

	level.Info(logger).Log("msg", "checking missing streams from first response...")
	for _, lbls := range streamsInResponses[1] {
		if !slices.Contains(streamsInResponses[0], lbls) {
			level.Warn(logger).Log("msg", "stream missing from first response", "labels", lbls)
		}
	}

	level.Info(logger).Log("msg", "checking missing streams from second response...")
	valid := 0
	for _, lbls := range streamsInResponses[0] {
		if !slices.Contains(streamsInResponses[1], lbls) {
			level.Warn(logger).Log("msg", "stream missing from second response", "labels", lbls)
			continue
		}
		valid++
	}

	if len(streamsInResponses[0]) == len(streamsInResponses[1]) {
		for i, lbls := range streamsInResponses[0] {
			if lbls != streamsInResponses[1][i] {
				level.Warn(logger).Log("msg", "stream ordering mismatch", "expected", lbls, "actual", streamsInResponses[1][i])
				continue
			}
		}
	}
	level.Info(logger).Log("msg", "stream comparison complete", "matching_streams", valid)

	level.Info(logger).Log("msg", "starting entry comparison...", "matching_streams", valid)
	for lbls, entries := range chunkEntries {
		printed := false
		dataobjEntries, ok := dataobjEntries[lbls]
		if !ok {
			level.Warn(logger).Log("msg", "stream missing from second response", "labels", lbls)
			continue
		}
		for i, entry := range entries {
			if entry.Line != dataobjEntries[i].Line {
				if !printed {
					level.Warn(logger).Log("msg", "line mismatch found in stream", "stream", lbls)
					printed = true
				}
				level.Debug(logger).Log("msg", "mismatched line", "line", entry.Line)
				continue
			}
		}
	}
	level.Info(logger).Log("msg", "entry comparison complete")
	return nil
}

func checkLogMatrix(results []loghttp.Matrix) error {
	for i, sampleA := range results[0] {
		sampleB := results[1][i]

		for j, valueA := range sampleA.Values {
			valueB := sampleB.Values[j]
			if valueA.Timestamp != valueB.Timestamp {
				level.Warn(logger).Log("msg", "timestamp mismatch", "expected", valueA.Timestamp, "actual", valueB.Timestamp)
			}
			if !valueA.Value.Equal(valueB.Value) {
				level.Warn(logger).Log("msg", "value mismatch", "expected", valueA.Value, "actual", valueB.Value)
			}
		}
	}
	return nil
}

// doRemoteQuery executes a query against a Loki host
func doRemoteQuery(host string, query string, start time.Time, end time.Time, limit int) (loghttp.QueryResponseData, error) {
	if limit == 0 {
		limit = 100
	}
	path := "/loki/api/v1/query_range"
	params := url.Values{
		"query":     {query},
		"start":     {fmt.Sprintf("%d", start.UnixNano())},
		"end":       {fmt.Sprintf("%d", end.UnixNano())},
		"direction": {"BACKWARD"},
		"limit":     {fmt.Sprintf("%d", limit)},
	}.Encode()
	url := "http://" + host + path + "?" + params

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return loghttp.QueryResponseData{}, err
	}
	req.Header.Add("X-Scope-OrgID", orgID)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return loghttp.QueryResponseData{}, err
	}

	file, err := os.Create(fmt.Sprintf("host-%s.json", host))
	if err != nil {
		return loghttp.QueryResponseData{}, err
	}
	defer file.Close()
	teeReader := io.TeeReader(resp.Body, file)

	body, err := io.ReadAll(teeReader)
	if err != nil {
		return loghttp.QueryResponseData{}, err
	}

	var respData loghttp.QueryResponse
	err = json.Unmarshal(body, &respData)
	if err != nil {
		return loghttp.QueryResponseData{}, fmt.Errorf("unmarshalling response: %s %w", string(body), err)
	}
	return respData.Data, nil
}
