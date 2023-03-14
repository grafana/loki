package manifests_test

import (
	"fmt"
	"testing"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/manifests"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestRulesConfigMap_ReturnsDataEntriesPerRule(t *testing.T) {
	cm, err := manifests.RulesConfigMap(testOptions())
	require.NoError(t, err)
	require.NotNil(t, cm)
	require.Len(t, cm.Data, 4)
	require.Contains(t, cm.Data, "dev-alerting-rules-alerts1.yaml")
	require.Contains(t, cm.Data, "dev-recording-rules-recs1.yaml")
	require.Contains(t, cm.Data, "prod-alerting-rules-alerts2.yaml")
	require.Contains(t, cm.Data, "prod-recording-rules-recs2.yaml")
}

func TestRulesConfigMap_ReturnsTenantMapPerRule(t *testing.T) {
	opts := testOptions()
	cm, err := manifests.RulesConfigMap(opts)
	require.NoError(t, err)
	require.NotNil(t, cm)
	require.Len(t, cm.Data, 4)
	fmt.Print(opts.Tenants.Configs)
	require.Contains(t, opts.Tenants.Configs["tenant-a"].RuleFiles, "dev-alerting-rules-alerts1.yaml")
	require.Contains(t, opts.Tenants.Configs["tenant-a"].RuleFiles, "prod-alerting-rules-alerts2.yaml")
	require.Contains(t, opts.Tenants.Configs["tenant-b"].RuleFiles, "dev-recording-rules-recs1.yaml")
	require.Contains(t, opts.Tenants.Configs["tenant-b"].RuleFiles, "prod-recording-rules-recs2.yaml")
}

func testOptions() *manifests.Options {
	return &manifests.Options{
		Tenants: manifests.Tenants{
			Configs: map[string]manifests.TenantConfig{
				"tenant-a": {},
				"tenant-b": {},
			},
		},
		AlertingRules: []lokiv1.AlertingRule{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "alerting-rules",
					Namespace: "dev",
					UID:       types.UID("alerts1"),
				},
				Spec: lokiv1.AlertingRuleSpec{
					TenantID: "tenant-a",
					Groups: []*lokiv1.AlertingRuleGroup{
						{
							Name: "rule-a",
						},
						{
							Name: "rule-b",
						},
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "alerting-rules",
					Namespace: "prod",
					UID:       types.UID("alerts2"),
				},
				Spec: lokiv1.AlertingRuleSpec{
					TenantID: "tenant-a",
					Groups: []*lokiv1.AlertingRuleGroup{
						{
							Name: "rule-c",
						},
						{
							Name: "rule-d",
						},
					},
				},
			},
		},
		RecordingRules: []lokiv1.RecordingRule{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "recording-rules",
					Namespace: "dev",
					UID:       types.UID("recs1"),
				},
				Spec: lokiv1.RecordingRuleSpec{
					TenantID: "tenant-b",
					Groups: []*lokiv1.RecordingRuleGroup{
						{
							Name: "rule-a",
						},
						{
							Name: "rule-b",
						},
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "recording-rules",
					Namespace: "prod",
					UID:       types.UID("recs2"),
				},
				Spec: lokiv1.RecordingRuleSpec{
					TenantID: "tenant-b",
					Groups: []*lokiv1.RecordingRuleGroup{
						{
							Name: "rule-c",
						},
						{
							Name: "rule-d",
						},
					},
				},
			},
		},
	}
}
