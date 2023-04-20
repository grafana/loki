package ruler

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/prometheus/config"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logql"
	rulerbase "github.com/grafana/loki/pkg/ruler/base"
	"github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/validation"
)

// TestInvalidRemoteWriteConfig tests that a validation error is raised when config is invalid
func TestInvalidRemoteWriteConfig(t *testing.T) {
	// if remote-write is not enabled, validation fails
	cfg := Config{
		Config: rulerbase.Config{},
		RemoteWrite: RemoteWriteConfig{
			Enabled: false,
		},
	}
	require.Nil(t, cfg.RemoteWrite.Validate())

	// if no remote-write URL is configured, validation fails
	cfg = Config{
		Config: rulerbase.Config{},
		RemoteWrite: RemoteWriteConfig{
			Enabled: true,
			Client: &config.RemoteWriteConfig{
				URL: nil,
			},
		},
	}
	require.Error(t, cfg.RemoteWrite.Validate())
}

// TestNonMetricQuery tests that only metric queries can be executed in the query function,
// as both alert and recording rules rely on metric queries being run
func TestNonMetricQuery(t *testing.T) {
	overrides, err := validation.NewOverrides(validation.Limits{}, nil)
	require.Nil(t, err)

	log := log.Logger
	engine := logql.NewEngine(logql.EngineOpts{}, &FakeQuerier{}, overrides, log)
	eval, err := NewLocalEvaluator(engine, log)
	require.NoError(t, err)

	queryFunc := queryFunc(eval, overrides, fakeChecker{}, "fake", log)

	_, err = queryFunc(context.TODO(), `{job="nginx"}`, time.Now())
	require.Error(t, err, "rule result is not a vector or scalar")
}

type FakeQuerier struct{}

func (q *FakeQuerier) SelectLogs(context.Context, logql.SelectLogParams) (iter.EntryIterator, error) {
	return iter.NoopIterator, nil
}

func (q *FakeQuerier) SelectSamples(context.Context, logql.SelectSampleParams) (iter.SampleIterator, error) {
	return iter.NoopIterator, nil
}

type fakeChecker struct{}

func (f fakeChecker) isReady(tenant string) bool {
	return true
}
