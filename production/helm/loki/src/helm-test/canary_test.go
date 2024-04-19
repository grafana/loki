//go:build helm_test
// +build helm_test

package test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	promConfig "github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/textparse"
	"github.com/stretchr/testify/require"
)

type testResultFunc func(t *testing.T, ctx context.Context, metric string, test func(model.SampleValue) bool, msg string) error

func TestCanary(t *testing.T) {

	var testResult testResultFunc

	// Default to directly querying a canary and looking for specific metrics.
	testResult = testResultCanary
	totalEntries := "loki_canary_entries_total"
	totalEntriesMissing := "loki_canary_missing_entries_total"

	// For backwards compatibility and also for anyone who wants to validate with prometheus instead of querying
	// a canary directly, if the CANARY_PROMETHEUS_ADDRESS is specified we will use prometheus to validate.
	address := os.Getenv("CANARY_PROMETHEUS_ADDRESS")
	if address != "" {
		testResult = testResultPrometheus
		// Use the sum function to aggregate the results from multiple canaries.
		totalEntries = "sum(loki_canary_entries_total)"
		totalEntriesMissing = "sum(loki_canary_missing_entries_total)"
	}

	timeout := getEnv("CANARY_TEST_TIMEOUT", "1m")
	timeoutDuration, err := time.ParseDuration(timeout)
	require.NoError(t, err, "Failed to parse timeout. Please set CANARY_TEST_TIMEOUT to a valid duration.")

	ctx, cancel := context.WithTimeout(context.Background(), timeoutDuration)

	t.Cleanup(func() {
		cancel()
	})

	t.Run("Canary should have entries", func(t *testing.T) {
		eventually(t, func() error {
			return testResult(t, ctx, totalEntries, func(v model.SampleValue) bool {
				return v > 0
			}, fmt.Sprintf("Expected %s to be greater than 0", totalEntries))
		}, timeoutDuration, "Expected Loki Canary to have entries")
	})

	t.Run("Canary should not have missed any entries", func(t *testing.T) {
		eventually(t, func() error {
			return testResult(t, ctx, totalEntriesMissing, func(v model.SampleValue) bool {
				return v == 0
			}, fmt.Sprintf("Expected %s to equal 0", totalEntriesMissing))
		}, timeoutDuration, "Expected Loki Canary to not have any missing entries")
	})
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func testResultPrometheus(t *testing.T, ctx context.Context, query string, test func(model.SampleValue) bool, msg string) error {
	// TODO (ewelch): if we did a lot of these, we'd want to reuse the client but right now we only run a couple tests
	client := newClient(t)
	result, _, err := client.Query(ctx, query, time.Now())
	if err != nil {
		return err
	}
	if v, ok := result.(model.Vector); ok {
		for _, s := range v {
			t.Logf("%s => %v\n", query, s.Value)
			if !test(s.Value) {
				return errors.New(msg)
			}
		}
		return nil
	}

	return fmt.Errorf("unexpected Prometheus result type: %v ", result.Type())
}

func newClient(t *testing.T) v1.API {
	address := os.Getenv("CANARY_PROMETHEUS_ADDRESS")
	require.NotEmpty(t, address, "CANARY_PROMETHEUS_ADDRESS must be set to a valid prometheus address")

	client, err := api.NewClient(api.Config{
		Address: address,
	})
	require.NoError(t, err, "Failed to create Loki Canary client")

	return v1.NewAPI(client)
}

func testResultCanary(t *testing.T, ctx context.Context, metric string, test func(model.SampleValue) bool, msg string) error {
	address := os.Getenv("CANARY_SERVICE_ADDRESS")
	require.NotEmpty(t, address, "CANARY_SERVICE_ADDRESS must be set to a valid kubernetes service for the Loki canaries")

	// TODO (ewelch): if we did a lot of these, we'd want to reuse the client but right now we only run a couple tests
	client, err := promConfig.NewClientFromConfig(promConfig.HTTPClientConfig{}, "canary-test")
	require.NoError(t, err, "Failed to create Prometheus client")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	require.NoError(t, err, "Failed to create request")

	rsp, err := client.Do(req)
	if rsp != nil {
		defer rsp.Body.Close()
	}
	require.NoError(t, err, "Failed to scrape metrics")

	body, err := io.ReadAll(rsp.Body)
	require.NoError(t, err, "Failed to read response body")

	p, err := textparse.New(body, rsp.Header.Get("Content-Type"), true, nil)
	require.NoError(t, err, "Failed to create Prometheus parser")

	for {
		e, err := p.Next()
		if err == io.EOF {
			return errors.New("metric not found")
		}

		if e != textparse.EntrySeries {
			continue
		}

		l := labels.Labels{}
		p.Metric(&l)

		// Currently we aren't validating any labels, just the metric name, however this could be extended to do so.
		name := l.Get(model.MetricNameLabel)
		if name != metric {
			continue
		}

		_, _, val := p.Series()
		t.Logf("%s => %v\n", metric, val)

		// Note: SampleValue has functions for comparing the equality of two floats which is
		// why we convert this back to a SampleValue here for easier use intests.
		if !test(model.SampleValue(val)) {
			return errors.New(msg)
		}

		// Returning here will only validate that one series was found matching the label name that met the condition
		// it could be possible since we don't validate the rest of the labels that there is mulitple series
		// but currently this meets the spirit of the test.
		return nil
	}
}

func eventually(t *testing.T, test func() error, timeoutDuration time.Duration, msg string) {
	require.Eventually(t, func() bool {
		queryError := test()
		if queryError != nil {
			t.Logf("Query failed\n%+v\n", queryError)
		}
		return queryError == nil
	}, timeoutDuration, 1*time.Second, msg)
}
