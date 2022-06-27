package manifests

import (
	"fmt"

	"github.com/grafana/loki/operator/internal/manifests/internal/rules"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RulesConfigMap returns a ConfigMap resource that contains
// all loki alerting and recording rules as YAML data.
func RulesConfigMap(opts *Options) (*corev1.ConfigMap, error) {
	data := make(map[string]string)

	for _, r := range opts.AlertingRules {
		c, err := rules.MarshalAlertingRule(r)
		if err != nil {
			return nil, err
		}

		key := fmt.Sprintf("%s-%s-%s.yaml", r.Namespace, r.Name, r.UID)
		if tenant, ok := opts.Tenants.Configs[r.Spec.TenantID]; ok {
			tenant.RuleFiles = append(tenant.RuleFiles, key)
			data[key] = c
			opts.Tenants.Configs[r.Spec.TenantID] = tenant
		}
	}

	for _, r := range opts.RecordingRules {
		c, err := rules.MarshalRecordingRule(r)
		if err != nil {
			return nil, err
		}

		key := fmt.Sprintf("%s-%s-%s.yaml", r.Namespace, r.Name, r.UID)
		if tenant, ok := opts.Tenants.Configs[r.Spec.TenantID]; ok {
			tenant.RuleFiles = append(tenant.RuleFiles, key)
			data[key] = c
			opts.Tenants.Configs[r.Spec.TenantID] = tenant
		}
	}

	l := commonLabels(opts.Name)

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
		Data: data,
	}, nil
}
