package manifests

import (
	"fmt"

	"github.com/grafana/loki/operator/internal/manifests/internal/rules"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RulesConfigMap returns a ConfigMap resource that contains
// all loki alerting and recording rules as YAML data.
// If the size of the data is more than 1MB, the ConfigMap will
// be split into multiple shards, and this function will return
// the list of shards
func RulesConfigMapShards(opts *Options) ([]*corev1.ConfigMap, error) {
	l := commonLabels(opts.Name)

	template := newConfigMapTemplate(opts, l)

	shardedConfigMap := NewShardedConfigMap(template, RulesConfigMapName(opts.Namespace))

	for _, r := range opts.AlertingRules {
		c, err := rules.MarshalAlertingRule(r)
		if err != nil {
			return nil, err
		}

		key := fmt.Sprintf("%s-%s-%s.yaml", r.Namespace, r.Name, r.UID)
		if tenant, ok := opts.Tenants.Configs[r.Spec.TenantID]; ok {
			tenant.RuleFiles = append(tenant.RuleFiles, key)
			shardedConfigMap.data[key] = c
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
			shardedConfigMap.data[key] = c
			opts.Tenants.Configs[r.Spec.TenantID] = tenant
		}
	}

	// start the sharding process, shards will contain a list of the resulting
	// configmaps, identified by a "prefix+index"
	shards := shardedConfigMap.Shard()

	// TODO: what happens when there's an update to the rules?

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
