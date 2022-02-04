package usagestats

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	jsoniter "github.com/json-iterator/go"
	prom "github.com/prometheus/prometheus/web/api/v1"
)

var (
	httpClient    = http.Client{Timeout: 5 * time.Second}
	usageStatsURL = "https://stats.grafana.org/loki-usage-report"
)

type Stats struct {
	ClusterID              string    `json:"clusterID"`
	CreatedAt              time.Time `json:"createdAt"`
	Interval               time.Time `json:"interval"`
	Target                 string    `json:"target"`
	prom.PrometheusVersion `json:"version"`
	Os                     string                 `json:"os"`
	Arch                   string                 `json:"arch"`
	Edition                string                 `json:"edition"`
	Metrics                map[string]interface{} `json:"metrics"`
}

func sendStats(ctx context.Context, stats Stats) error {
	out, err := jsoniter.MarshalIndent(stats, "", " ")
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, usageStatsURL, bytes.NewBuffer(out))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req.WithContext(ctx))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		return fmt.Errorf("failed to send usage stats: %s  body: %s", resp.Status, string(data))
	}
	return nil
}
