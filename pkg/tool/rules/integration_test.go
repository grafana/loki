package rules

import (
	"context"
	"testing"
	"time"

	"os"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
)

// TestIntegrationAlertFires exercises the full pipeline: load streams, load rules,
// evaluate over time, and check that an alert fires with correct labels and annotations.
func TestIntegrationAlertFires(t *testing.T) {
	// Write a temporary rule file
	ruleContent := `---
groups:
  - name: test-rules
    interval: 1m
    rules:
      - alert: TestAlert
        expr: 'count_over_time({container="test"} |= "ERROR"[5m]) > 0'
        annotations:
          summary: "Test alert fired"
          description: "Error in pod {{ $labels.pod }} in namespace {{ $labels.namespace }}"
          severity: "critical"
`
	ruleFile := t.TempDir() + "/test.rules"
	if err := writeFile(ruleFile, ruleContent); err != nil {
		t.Fatalf("failed to write rule file: %v", err)
	}

	// Set up storage with test streams
	storage := newTestStorage()
	err := storage.parseAndLoadStreams([]stream{
		{
			Labels: `{container="test", pod="web-0", namespace="default"}`,
			Lines:  []string{"ERROR: something went wrong", "ERROR: it happened again"},
		},
	}, model.Duration(time.Minute))
	if err != nil {
		t.Fatalf("failed to load streams: %v", err)
	}

	// Create evaluator and load rules
	logger := log.NewNopLogger()
	evaluator := newTestEvaluator(storage, logger)
	err = evaluator.loadRules([]string{ruleFile}, model.Duration(time.Minute), labels.EmptyLabels(), "")
	if err != nil {
		t.Fatalf("failed to load rules: %v", err)
	}

	// Step through time
	ctx := user.InjectOrgID(context.Background(), "fake")
	for ts := time.Duration(0); ts <= 5*time.Minute; ts += time.Minute {
		evalTime := time.Unix(0, 0).UTC().Add(ts)
		if err := evaluator.evaluateAtTime(ctx, evalTime); err != nil {
			t.Fatalf("evaluation failed at %v: %v", ts, err)
		}
	}

	// Check alert state at t=5m
	alerts := evaluator.getActiveAlertsForRule("TestAlert")

	if len(alerts) != 1 {
		t.Fatalf("expected 1 firing alert, got %d", len(alerts))
	}

	alert := alerts[0]

	// Verify labels
	if alert.Labels.Get("container") != "test" {
		t.Errorf("expected container=test, got %s", alert.Labels.Get("container"))
	}
	if alert.Labels.Get("pod") != "web-0" {
		t.Errorf("expected pod=web-0, got %s", alert.Labels.Get("pod"))
	}
	if alert.Labels.Get("namespace") != "default" {
		t.Errorf("expected namespace=default, got %s", alert.Labels.Get("namespace"))
	}

	// Verify annotations are expanded
	if alert.Annotations.Get("summary") != "Test alert fired" {
		t.Errorf("expected summary='Test alert fired', got %q", alert.Annotations.Get("summary"))
	}
	expectedDesc := "Error in pod web-0 in namespace default"
	if alert.Annotations.Get("description") != expectedDesc {
		t.Errorf("expected description=%q, got %q", expectedDesc, alert.Annotations.Get("description"))
	}
}

// TestIntegrationAlertDoesNotFire verifies that an alert does not fire when input
// doesn't match the rule's filter.
func TestIntegrationAlertDoesNotFire(t *testing.T) {
	ruleContent := `---
groups:
  - name: test-rules
    interval: 1m
    rules:
      - alert: TestAlert
        expr: 'count_over_time({container="test"} |= "ERROR"[5m]) > 0'
        annotations:
          summary: "Test alert fired"
`
	ruleFile := t.TempDir() + "/test.rules"
	if err := writeFile(ruleFile, ruleContent); err != nil {
		t.Fatalf("failed to write rule file: %v", err)
	}

	storage := newTestStorage()
	err := storage.parseAndLoadStreams([]stream{
		{
			Labels: `{container="test", pod="web-0"}`,
			Lines:  []string{"INFO: all is well", "INFO: still fine"},
		},
	}, model.Duration(time.Minute))
	if err != nil {
		t.Fatalf("failed to load streams: %v", err)
	}

	logger := log.NewNopLogger()
	evaluator := newTestEvaluator(storage, logger)
	err = evaluator.loadRules([]string{ruleFile}, model.Duration(time.Minute), labels.EmptyLabels(), "")
	if err != nil {
		t.Fatalf("failed to load rules: %v", err)
	}

	ctx := user.InjectOrgID(context.Background(), "fake")
	for ts := time.Duration(0); ts <= 5*time.Minute; ts += time.Minute {
		evalTime := time.Unix(0, 0).UTC().Add(ts)
		if err := evaluator.evaluateAtTime(ctx, evalTime); err != nil {
			t.Fatalf("evaluation failed at %v: %v", ts, err)
		}
	}

	alerts := evaluator.getActiveAlertsForRule("TestAlert")

	if len(alerts) != 0 {
		t.Errorf("expected 0 alerts, got %d", len(alerts))
	}
}

// TestIntegrationForClause verifies that the `for` clause delays alert firing.
func TestIntegrationForClause(t *testing.T) {
	ruleContent := `---
groups:
  - name: test-rules
    interval: 1m
    rules:
      - alert: SlowAlert
        expr: 'count_over_time({container="test"} |= "ERROR"[5m]) > 0'
        for: 3m
        annotations:
          summary: "Slow alert"
`
	ruleFile := t.TempDir() + "/test.rules"
	if err := writeFile(ruleFile, ruleContent); err != nil {
		t.Fatalf("failed to write rule file: %v", err)
	}

	storage := newTestStorage()
	err := storage.parseAndLoadStreams([]stream{
		{
			Labels: `{container="test", pod="web-0"}`,
			Lines:  []string{"ERROR: fail", "ERROR: fail", "ERROR: fail", "ERROR: fail", "ERROR: fail"},
		},
	}, model.Duration(time.Minute))
	if err != nil {
		t.Fatalf("failed to load streams: %v", err)
	}

	logger := log.NewNopLogger()
	evaluator := newTestEvaluator(storage, logger)
	err = evaluator.loadRules([]string{ruleFile}, model.Duration(time.Minute), labels.EmptyLabels(), "")
	if err != nil {
		t.Fatalf("failed to load rules: %v", err)
	}

	ctx := user.InjectOrgID(context.Background(), "fake")

	// Evaluate through t=5m
	for ts := time.Duration(0); ts <= 5*time.Minute; ts += time.Minute {
		evalTime := time.Unix(0, 0).UTC().Add(ts)
		if err := evaluator.evaluateAtTime(ctx, evalTime); err != nil {
			t.Fatalf("evaluation failed at %v: %v", ts, err)
		}
	}

	// At t=2m the condition is true but for:3m not yet met
	alerts2m := evaluator.getActiveAlertsForRule("SlowAlert")
	if len(alerts2m) != 0 {
		t.Errorf("expected 0 firing alerts at 2m (for:3m not met), got %d", len(alerts2m))
	}

	// At t=5m the for:3m should be satisfied (condition true since ~t=1m)
	alerts5m := evaluator.getActiveAlertsForRule("SlowAlert")
	if len(alerts5m) != 1 {
		t.Errorf("expected 1 firing alert at 5m, got %d", len(alerts5m))
	}
}

// TestIntegrationSumByPreservesLabels verifies that sum by() correctly preserves
// only the labels listed in the by clause.
func TestIntegrationSumByPreservesLabels(t *testing.T) {
	ruleContent := `---
groups:
  - name: test-rules
    interval: 1m
    rules:
      - alert: AggAlert
        expr: 'sum by(namespace, pod) (count_over_time({container="test"} |= "ERROR"[5m])) > 0'
        annotations:
          summary: "Error in {{ $labels.pod }}"
        labels:
          severity: critical
`
	ruleFile := t.TempDir() + "/test.rules"
	if err := writeFile(ruleFile, ruleContent); err != nil {
		t.Fatalf("failed to write rule file: %v", err)
	}

	storage := newTestStorage()
	err := storage.parseAndLoadStreams([]stream{
		{
			Labels: `{container="test", pod="web-0", namespace="default", app="myapp"}`,
			Lines:  []string{"ERROR: something went wrong", "ERROR: it happened again"},
		},
	}, model.Duration(time.Minute))
	if err != nil {
		t.Fatalf("failed to load streams: %v", err)
	}

	logger := log.NewNopLogger()
	evaluator := newTestEvaluator(storage, logger)
	err = evaluator.loadRules([]string{ruleFile}, model.Duration(time.Minute), labels.EmptyLabels(), "")
	if err != nil {
		t.Fatalf("failed to load rules: %v", err)
	}

	ctx := user.InjectOrgID(context.Background(), "fake")
	for ts := time.Duration(0); ts <= 5*time.Minute; ts += time.Minute {
		evalTime := time.Unix(0, 0).UTC().Add(ts)
		if err := evaluator.evaluateAtTime(ctx, evalTime); err != nil {
			t.Fatalf("evaluation failed at %v: %v", ts, err)
		}
	}

	alerts := evaluator.getActiveAlertsForRule("AggAlert")
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}

	a := alerts[0]

	// namespace and pod should be preserved (in by clause)
	if a.Labels.Get("namespace") != "default" {
		t.Errorf("expected namespace=default, got %q", a.Labels.Get("namespace"))
	}
	if a.Labels.Get("pod") != "web-0" {
		t.Errorf("expected pod=web-0, got %q", a.Labels.Get("pod"))
	}

	// rule-defined label should be present
	if a.Labels.Get("severity") != "critical" {
		t.Errorf("expected severity=critical, got %q", a.Labels.Get("severity"))
	}

	// container and app should NOT be preserved (not in by clause)
	if a.Labels.Get("container") != "" {
		t.Errorf("expected container to be stripped, got %q", a.Labels.Get("container"))
	}
	if a.Labels.Get("app") != "" {
		t.Errorf("expected app to be stripped, got %q", a.Labels.Get("app"))
	}

	// Annotation should be expanded with surviving labels
	if a.Annotations.Get("summary") != "Error in web-0" {
		t.Errorf("expected summary='Error in web-0', got %q", a.Annotations.Get("summary"))
	}
}

// writeFile is a test helper.
func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}
