package congestion

import (
	"net/http"

	"github.com/grafana/loki/pkg/storage/chunk/client/hedging"
)

type NoopHedger struct{}

func NewNoopHedger(Config) *NoopHedger {
	return &NoopHedger{}
}

func (n NoopHedger) HTTPClient(hedging.Config) (*http.Client, error) {
	return http.DefaultClient, nil
}
