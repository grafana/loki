package test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
)

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func testResult(result model.Value, query string, test func(model.SampleValue) bool, msg string) error {
	if v, ok := result.(model.Vector); ok {
		for _, s := range v {
			fmt.Printf("%s => %v\n", query, s.Value)
			if !test(s.Value) {
				return errors.New(msg)
			}
		}

		return nil
	}

	return fmt.Errorf("unexpected Prometheus result type: %v ", result.Type())
}

func TestCanary(t *testing.T) {
	totalEntriesQuery := "sum(loki_canary_entries_total)"
	totalEntriesMissingQuery := "sum(loki_canary_missing_entries_total)"

	testContext := func(t *testing.T) v1.API {
		address := os.Getenv("CANARY_ADDRESS")
		require.NotEmpty(t, address, "CANARY_ADDRESS must be set to a valid prometheus address")

		client, err := api.NewClient(api.Config{
			Address: address,
		})
		require.NoError(t, err, "Failed to create Loki Canary client")

		return v1.NewAPI(client)
	}

	withRetries := func(t *testing.T, test func() error) error {
		retries := getEnv("CANARY_RETRIES", "3")
		numRetries, err := strconv.Atoi(retries)
		require.NoError(t, err, "Failed to parse number of retries. Please set CANARY_RETRIES to a valid integer.")

		retryDelay := getEnv("CANARY_RETRY_DELAY", "30s")
		retryDelayDuration, err := time.ParseDuration(retryDelay)
		require.NoError(t, err, "Failed to parse retry delay duration. Please set CANARY_RETRY_DELAY to a valid duration.")

		var queryError error
		for i := 0; i < numRetries; i++ {
			fmt.Printf("Running test %s, attempt %d/%d\n", t.Name(), i+1, numRetries)
			queryError = test()

			if queryError != nil {
				time.Sleep(retryDelayDuration)
				continue
			} else {
				break
			}
		}

		return queryError
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

	t.Cleanup(func() {
		cancel()
	})

	t.Run("Canary should have entries", func(t *testing.T) {
		client := testContext(t)

		err := withRetries(t, func() error {
			result, _, err := client.Query(ctx, totalEntriesQuery, time.Now(), v1.WithTimeout(5*time.Second))
			if err != nil {
				return err
			}
			return testResult(result, totalEntriesQuery, func(v model.SampleValue) bool {
				return v > 0
			}, fmt.Sprintf("Expected %s to be greater than 0", totalEntriesQuery))
		})

		require.NoError(t, err, "Expected Loki Canary to have entries")
	})

	t.Run("Canary should not have missed any entries", func(t *testing.T) {
		client := testContext(t)

		err := withRetries(t, func() error {
			result, _, err := client.Query(ctx, totalEntriesMissingQuery, time.Now(), v1.WithTimeout(5*time.Second))
			if err != nil {
				return err
			}
			return testResult(result, totalEntriesMissingQuery, func(v model.SampleValue) bool {
				return v == 0
			}, fmt.Sprintf("Expected %s to equal 0", totalEntriesMissingQuery))
		})

		require.NoError(t, err, "Expected Loki Canary to have entries")
	})
}
