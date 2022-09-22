package canary

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

type Client struct {
	address string
	api     v1.API
  timeBetweenRetries time.Duration
}

func DefaultCanaryClient(address string, timeBetweenRetries time.Duration) (*Client, error) {
	client, err := api.NewClient(api.Config{
		Address: address,
	})

	if err != nil {
		return nil, fmt.Errorf("error creating Prometheus client: %w", err)
	}

	return NewCanaryClient(address, v1.NewAPI(client), timeBetweenRetries), nil
}

func NewCanaryClient(address string, api v1.API, timeBetweenRetries time.Duration) *Client {
	return &Client{
		address: address,
		api:     api,
    timeBetweenRetries: timeBetweenRetries,
	}
}

func (c *Client) Run(retries int) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var err error
	for i := 0; i < retries; i++ {
		totalEntriesQuery := "sum(loki_canary_entries_total)"
		totalEntriesMissingQuery := "sum(loki_canary_missing_entries_total)"

		var sumTotalResult model.Value
		sumTotalResult, err = c.execQuery(ctx, totalEntriesQuery)
		if err != nil {
			continue
		}
		err = testResult(sumTotalResult, totalEntriesQuery, func(v model.SampleValue) bool {
			return v > 0
		}, fmt.Sprintf("Expected %s to be greater than 0", totalEntriesQuery))
		if err != nil {
			continue
		}

		var sumTotalMissingResult model.Value
		sumTotalMissingResult, err = c.execQuery(ctx, totalEntriesMissingQuery)
		if err != nil {
			continue
		}
		err = testResult(sumTotalMissingResult, totalEntriesMissingQuery, func(v model.SampleValue) bool {
			return v == 0
		}, fmt.Sprintf("Expected %s to equal 0", totalEntriesMissingQuery))
		if err != nil {
			continue
		}

    break
	}

	if err != nil {
		return err
	}

	return nil
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

func (c *Client) execQuery(ctx context.Context, query string) (model.Value, error) {
	result, warnings, err := c.api.Query(ctx, query, time.Now(), v1.WithTimeout(5*time.Second))
	if err != nil {
		return nil, fmt.Errorf("error sending \"%s\" query to Prometheus: %w", query, err)
	}
	if len(warnings) > 0 {
		fmt.Printf("Warnings while querying Prometheus: %v", warnings)
	}
	return result, nil
}
