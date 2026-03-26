package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/prometheus/prometheus/prompb"
)

// pushMetrics sends time series to a Prometheus remote_write endpoint.
// Single attempt with a 30-second timeout; no retries.
func pushMetrics(url, username, password string, series []prompb.TimeSeries) error {
	req := &prompb.WriteRequest{Timeseries: series}

	data, err := proto.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal write request: %w", err)
	}

	compressed := snappy.Encode(nil, data)

	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(compressed))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/x-protobuf")
	httpReq.Header.Set("Content-Encoding", "snappy")
	httpReq.Header.Set("X-Prometheus-Remote-Write-Version", "0.1.0")

	if username != "" && password != "" {
		httpReq.SetBasicAuth(username, password)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("remote_write request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("remote_write returned %d: %s", resp.StatusCode, string(body))
	}

	return nil
}
