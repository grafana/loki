package sizes

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/ViaQ/logerr/v2/kverrors"
	"github.com/prometheus/client_golang/api"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
)

var (
	metricsClient Client

	// durationDataHour is the default time duration to consider for metric scraping.
	// It is passed as first parameter to predict_linear.
	durationDataHour = "1h"
	// timeoutClient is the timeout duration for prometheus client.
	timeoutClient = 10 * time.Second

	// promURL is the URL of the prometheus thanos querier
	promURL string
	// promToken is the token to connect to prometheus thanos querier.
	promToken string
)

type client struct {
	api     promv1.API
	timeout time.Duration
}

// Client is the interface which contains methods for querying and extracting metrics.
type Client interface {
	LogLoggedBytesReceivedTotal(duration model.Duration) (float64, error)
}

func newClient(url, token string) (*client, error) {
	httpConfig := config.HTTPClientConfig{
		BearerToken: config.Secret(token),
		TLSConfig: config.TLSConfig{
			InsecureSkipVerify: true,
		},
	}

	rt, rtErr := config.NewRoundTripperFromConfig(httpConfig, "size-calculator-metrics")

	if rtErr != nil {
		return nil, kverrors.Wrap(rtErr, "failed creating prometheus configuration")
	}

	pc, err := api.NewClient(api.Config{
		Address:      url,
		RoundTripper: rt,
	})
	if err != nil {
		return nil, kverrors.Wrap(err, "failed creating prometheus client")
	}

	return &client{
		api:     promv1.NewAPI(pc),
		timeout: timeoutClient,
	}, nil
}

func (c *client) executeScalarQuery(query string) (float64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	res, _, err := c.api.Query(ctx, query, time.Now())
	if err != nil {
		return 0.0, kverrors.Wrap(err, "failed executing query",
			"query", query)
	}

	if res.Type() == model.ValScalar {
		value := res.(*model.Scalar)
		return float64(value.Value), nil
	}

	if res.Type() == model.ValVector {
		vec := res.(model.Vector)
		if vec.Len() == 0 {
			return 0.0, nil
		}

		return float64(vec[0].Value), nil
	}

	return 0.0, kverrors.Wrap(nil, "failed to parse result for query",
		"query", query)
}

func (c *client) LogLoggedBytesReceivedTotal(duration model.Duration) (float64, error) {
	query := fmt.Sprintf(
		`sum(predict_linear(log_logged_bytes_total[%s], %d))`,
		durationDataHour,
		int(time.Duration(duration).Seconds()),
	)

	return c.executeScalarQuery(query)
}

// PredictFor takes the default duration and predicts
// the amount of logs expected in 1 day
func PredictFor(duration model.Duration) (logsCollected float64, err error) {
	promURL = os.Getenv("PROMETHEUS_URL")
	promToken = os.Getenv("PROMETHEUS_TOKEN")

	// Create a client to collect metrics
	metricsClient, err = newClient(promURL, promToken)
	if err != nil {
		return 0, kverrors.Wrap(err, "Failed to create metrics client")
	}

	logsCollected, err = metricsClient.LogLoggedBytesReceivedTotal(duration)
	if err != nil {
		return 0, err
	}

	return logsCollected, nil
}
