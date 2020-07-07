package ruler

import (
	"errors"
	"time"

	"github.com/cortexproject/cortex/pkg/ruler/rules"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/storage"
)

func InMemoryAppendableHistory(r prometheus.Registerer) func(string, *rules.ManagerOptions) (rules.Appendable, rules.TenantAlertHistory) {
	metrics := NewMetrics(r)
	return func(userID string, opts *rules.ManagerOptions) (rules.Appendable, rules.TenantAlertHistory) {
		hist := NewMemHistory(userID, 5*time.Minute, opts, metrics)
		return hist, hist
	}
}

type NoopAppender struct{}

func (a NoopAppender) Appender() (storage.Appender, error)                     { return a, nil }
func (a NoopAppender) Add(l labels.Labels, t int64, v float64) (uint64, error) { return 0, nil }
func (a NoopAppender) AddFast(ref uint64, t int64, v float64) error {
	return errors.New("unimplemented")
}
func (a NoopAppender) Commit() error   { return nil }
func (a NoopAppender) Rollback() error { return nil }
