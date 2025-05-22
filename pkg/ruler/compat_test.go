package ruler

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/model/rulefmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gopkg.in/yaml.v3"

	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logql"
	rulerbase "github.com/grafana/loki/v3/pkg/ruler/base"
	"github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/validation"
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

// TestInvalidRuleExprParsing tests that a validation error is raised when rule expression is invalid
func TestInvalidRuleExprParsing(t *testing.T) {
	expectedAlertErrorMsg := "could not parse expression for alert 'alert-1-name' in group 'test': parse error"
	alertRuleExprInvalid := &rulefmt.RuleNode{
		Alert: yaml.Node{Value: "alert-1-name"},
		Expr:  yaml.Node{Value: "bad_value"},
	}

	alertErr := validateRuleNode(alertRuleExprInvalid, "test")
	assert.Containsf(t, alertErr.Error(), expectedAlertErrorMsg, "expected error containing '%s', got '%s'", expectedAlertErrorMsg, alertErr)

	expectedRecordErrorMsg := "could not parse expression for record 'record-1-name' in group 'test': parse error"
	recordRuleExprInvalid := &rulefmt.RuleNode{
		Record: yaml.Node{Value: "record-1-name"},
		Expr:   yaml.Node{Value: "bad_value"},
	}

	recordErr := validateRuleNode(recordRuleExprInvalid, "test")
	assert.Containsf(t, recordErr.Error(), expectedRecordErrorMsg, "expected error containing '%s', got '%s'", expectedRecordErrorMsg, recordErr)
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

	queryFunc := queryFunc(eval, fakeChecker{}, "fake", log)

	_, err = queryFunc(context.TODO(), `{job="nginx"}`, time.Now())
	require.Error(t, err, "rule result is not a vector or scalar")
}

type FakeQuerier struct{}

func (q *FakeQuerier) SelectLogs(context.Context, logql.SelectLogParams) (iter.EntryIterator, error) {
	return iter.NoopEntryIterator, nil
}

func (q *FakeQuerier) SelectSamples(context.Context, logql.SelectSampleParams) (iter.SampleIterator, error) {
	return iter.NoopSampleIterator, nil
}

type fakeChecker struct{}

func (f fakeChecker) isReady(_ string) bool {
	return true
}

func TestAddAndGetRuleDetailsFromContext(t *testing.T) {
	ctx := context.Background()
	ruleName := "test_rule"
	ruleType := "test_type"

	// Add rule details to context
	ctx = AddRuleDetailsToContext(ctx, ruleName, ruleType)

	// Retrieve rule details from context
	retrievedRuleName, retrievedRuleType := GetRuleDetailsFromContext(ctx)

	// Assert that the retrieved values match the expected values
	assert.Equal(t, ruleName, retrievedRuleName, "Expected rule name to match")
	assert.Equal(t, ruleType, retrievedRuleType, "Expected rule type to match")
}
