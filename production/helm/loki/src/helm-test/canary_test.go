//go:build helm_test
// +build helm_test

package test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
)

func TestCanary(t *testing.T) {
	totalEntriesQuery := "sum(loki_canary_entries_total)"
	totalEntriesMissingQuery := "sum(loki_canary_missing_entries_total)"

	timeout := getEnv("CANARY_TEST_TIMEOUT", "1m")
	timeoutDuration, err := time.ParseDuration(timeout)
	require.NoError(t, err, "Failed to parse timeout. Please set CANARY_TEST_TIMEOUT to a valid duration.")

	ctx, cancel := context.WithTimeout(context.Background(), timeoutDuration)

	t.Cleanup(func() {
		cancel()
	})

	t.Run("Canary should have entries", func(t *testing.T) {
		client := newClient(t)

		eventually(t, func() error {
			result, _, err := client.Query(ctx, totalEntriesQuery, time.Now(), v1.WithTimeout(timeoutDuration))
			if err != nil {
				return err
			}
			return testResult(t, result, totalEntriesQuery, func(v model.SampleValue) bool {
				return v > 0
			}, fmt.Sprintf("Expected %s to be greater than 0", totalEntriesQuery))
		}, timeoutDuration, "Expected Loki Canary to have entries")
	})

	t.Run("Canary should not have missed any entries", func(t *testing.T) {
		client := newClient(t)

		eventually(t, func() error {
			result, _, err := client.Query(ctx, totalEntriesMissingQuery, time.Now(), v1.WithTimeout(timeoutDuration))
			if err != nil {
				return err
			}
			return testResult(t, result, totalEntriesMissingQuery, func(v model.SampleValue) bool {
				return v == 0
			}, fmt.Sprintf("Expected %s to equal 0", totalEntriesMissingQuery))
		}, timeoutDuration, "Expected Loki Canary to not have any missing entries")
	})
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func testResult(t *testing.T, result model.Value, query string, test func(model.SampleValue) bool, msg string) error {
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

func eventually(t *testing.T, test func() error, timeoutDuration time.Duration, msg string) {
	require.Eventually(t, func() bool {
		queryError := test()
		if queryError != nil {
			t.Logf("Query failed\n%+v\n", queryError)
		}
		return queryError == nil
	}, timeoutDuration, 1*time.Second, msg)
}
