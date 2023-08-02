package congestion

import (
	"net/http"

	"github.com/grafana/loki/pkg/storage/chunk/client/hedging"
)

type NoopHedgeStrategy struct{}

func (n NoopHedgeStrategy) HTTPClient(hedging.Config) (*http.Client, error) {
	return http.DefaultClient, nil
}
