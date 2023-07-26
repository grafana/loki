package ruler

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/model/rulefmt"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logql"
	rulerbase "github.com/grafana/loki/pkg/ruler/base"
	"github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/validation"
	"gopkg.in/yaml.v3"
)

// TestInvalidRuleGroup tests that a validation error is raised when rule group is invalid
func TestInvalidRuleGroup(t *testing.T) {
	ruleGroupValid := rulefmt.RuleGroup{
		Name: "test",
		Rules: []rulefmt.RuleNode{
			{
				Alert: yaml.Node{Value: "alert-1-name"},
				Expr:  yaml.Node{Value: "sum by (job) (rate({namespace=~\"test\"} [5m]) > 0)"},
			},
			{
				Alert: yaml.Node{Value: "record-1-name"},
				Expr:  yaml.Node{Value: "sum by (job) (rate({namespace=~\"test\"} [5m]) > 0)"},
			},
		},
	}
	require.Nil(t, ValidateGroups(ruleGroupValid))

	ruleGroupInValid := rulefmt.RuleGroup{
		Name: "test",
		Rules: []rulefmt.RuleNode{
			{
				Alert: yaml.Node{Value: "alert-1-name"},
				Expr:  yaml.Node{Value: "bad_value"},
			},
			{
				Record: yaml.Node{Value: "record-1-name"},
				Expr:   yaml.Node{Value: "bad_value"},
			},
		},
	}
	require.Error(t, ValidateGroups(ruleGroupInValid)[0])
	require.Error(t, ValidateGroups(ruleGroupInValid)[1])
}

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

func (f fakeChecker) isReady(_ string) bool {
	return true
}
