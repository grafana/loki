package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/go-kit/log/level"
)

// lokiQueryResponse mirrors the Loki /api/v1/query_range JSON envelope.
type lokiQueryResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string             `json:"resultType"`
		Result     []lokiStreamResult `json:"result"`
	} `json:"data"`
}

type lokiStreamResult struct {
	Stream map[string]string `json:"stream"`
	Values [][]string        `json:"values"` // each value is [timestamp_ns, line]
}

const maxFetchLimit = 5000

// fetchMismatchLogs queries the given Loki endpoint for query-tee mismatch log lines.
// It always fetches up to maxFetchLimit lines so the analysis pipeline has a
// large pool of candidates to draw from even when individual queries fail.
func fetchMismatchLogs(ctx context.Context, cfg *ParsedAnalyzeConfig) ([]string, error) {
	query := cfg.LokiQuery + ` | logfmt | comparison_status="mismatch"`

	params := url.Values{
		"query":     {query},
		"start":     {fmt.Sprintf("%d", cfg.FromTime.UnixNano())},
		"end":       {fmt.Sprintf("%d", cfg.ToTime.UnixNano())},
		"direction": {"forward"},
		"limit":     {fmt.Sprintf("%d", maxFetchLimit)},
	}

	endpoint := cfg.LokiEndpoint + "/loki/api/v1/query_range?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("X-Scope-OrgID", cfg.OrgID)

	level.Info(logger).Log("msg", "fetching mismatch logs", "endpoint", cfg.LokiEndpoint, "query", query,
		"from", cfg.FromTime.Format(time.RFC3339), "to", cfg.ToTime.Format(time.RFC3339), "fetch_limit", maxFetchLimit)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("querying Loki: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("loki returned status %d: %s", resp.StatusCode, string(body))
	}

	var queryResp lokiQueryResponse
	if err := json.Unmarshal(body, &queryResp); err != nil {
		return nil, fmt.Errorf("unmarshalling Loki response: %w", err)
	}

	if queryResp.Status != statusSuccess {
		return nil, fmt.Errorf("loki query status: %s", queryResp.Status)
	}

	var lines []string
	for _, stream := range queryResp.Data.Result {
		for _, v := range stream.Values {
			if len(v) >= 2 {
				lines = append(lines, v[1])
			}
		}
	}

	level.Info(logger).Log("msg", "fetched mismatch logs", "count", len(lines))
	return lines, nil
}
