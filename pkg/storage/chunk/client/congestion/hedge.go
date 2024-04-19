package congestion

import (
	"net/http"

	"github.com/go-kit/log"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client/hedging"
)

type NoopHedger struct{}

func NewNoopHedger(Config) *NoopHedger {
	return &NoopHedger{}
}

func (n *NoopHedger) HTTPClient(hedging.Config) (*http.Client, error) {
	return http.DefaultClient, nil
}

func (n *NoopHedger) withLogger(log.Logger) Hedger { return n }
