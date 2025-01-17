package manifests

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/manifests/internal/rules"
	"github.com/grafana/loki/operator/internal/manifests/openshift"
)

type RuleName struct {
	cmName   string
	tenantID string
	filename string
}

// RulesConfigMapShards returns a ConfigMap resource that contains
// all loki alerting and recording rules as YAML data.
// If the size of the data is more than 1MB, the ConfigMap will
// be split into multiple shards, and this function will return
// the list of shards
func RulesConfigMapShards(opts *Options) ([]*corev1.ConfigMap, error) {
	l := ComponentLabels(LabelRulerComponent, opts.Name)
	template := newConfigMapTemplate(opts, l)

	shardedCM := NewShardedConfigMap(template, RulesConfigMapName(opts.Name))

	for _, r := range opts.AlertingRules {
		if opts.Stack.Tenants != nil {
			configureAlertingRuleForMode(&r, opts.Stack.Tenants.Mode)
		}

		c, err := rules.MarshalAlertingRule(r)
		if err != nil {
			return nil, err
		}
		key := RuleName{
			tenantID: r.Spec.TenantID,
			filename: fmt.Sprintf("%s-%s-%s", r.Namespace, r.Name, r.UID),
		}
		shardedCM.data[key.toString()] = c
	}

	for _, r := range opts.RecordingRules {
		if opts.Stack.Tenants != nil {
			configureRecordingRuleForMode(&r, opts.Stack.Tenants.Mode)
		}

		c, err := rules.MarshalRecordingRule(r)
		if err != nil {
			return nil, err
		}
		key := RuleName{
			tenantID: r.Spec.TenantID,
			filename: fmt.Sprintf("%s-%s-%s", r.Namespace, r.Name, r.UID),
		}
		shardedCM.data[key.toString()] = c
	}

	// If configmap size exceeds 1MB, split it into shards, identified by "prefix+index"
	shards := shardedCM.Shard(opts)

	return shards, nil
}

func newConfigMapTemplate(opts *Options, l map[string]string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      RulesConfigMapName(opts.Name),
			Namespace: opts.Namespace,
			Labels:    l,
		},
		Data: make(map[string]string),
	}
}

func (rn RuleName) toString() string {
	return fmt.Sprintf("%s%s%s.yaml", rn.tenantID, rulePartsSeparator, rn.filename)
}

func configureAlertingRuleForMode(ar *lokiv1.AlertingRule, mode lokiv1.ModeType) {
	switch mode {
	case lokiv1.Static, lokiv1.Dynamic:
		// Do nothing
	case lokiv1.OpenshiftLogging:
		openshift.AlertingRuleTenantLabels(ar)
	case lokiv1.OpenshiftNetwork:
		// Do nothing
	}
}

func configureRecordingRuleForMode(r *lokiv1.RecordingRule, mode lokiv1.ModeType) {
	switch mode {
	case lokiv1.Static, lokiv1.Dynamic:
		// Do nothing
	case lokiv1.OpenshiftLogging:
		openshift.RecordingRuleTenantLabels(r)
	case lokiv1.OpenshiftNetwork:
		// Do nothing
	}
}
