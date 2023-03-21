package manifests

import (
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// MaxConfigMapDataSizeBytes is the maximum data size in bytes that a single ConfigMap
// may contain. This is lower than 1MB  in order to reserve space for metadata
const MaxConfigMapDataSizeBytes = (1 * 1024 * 1024) - 50_000

// ShardedConfigMap is the configmap data that is sharded across multiple
// configmaps in case MaxConfigMapDataSizeBytes is exceeded
type ShardedConfigMap struct {
	namePrefix      string
	template        *corev1.ConfigMap
	data            map[string]string
	configMapShards []*corev1.ConfigMap
}

// NewShardedConfigMap takes a corev1.ConfigMap as template and a name prefix and
// returns a new ShardedConfigMap.
func NewShardedConfigMap(template *corev1.ConfigMap, namePrefix string) *ShardedConfigMap {
	return &ShardedConfigMap{
		namePrefix: namePrefix,
		template:   template,
		data:       make(map[string]string),
	}
}

func (cm *ShardedConfigMap) newConfigMapShard(index int) *corev1.ConfigMap {
	newShardCM := cm.template.DeepCopy()
	newShardCM.Data = make(map[string]string)
	newShardCM.Name = shardConfigMapName(newShardCM.Name, index)
	return newShardCM
}

func shardConfigMapName(prefix string, index int) string {
	return fmt.Sprintf("%s-%d", prefix, index)
}

func extractRuleNameComponents(rn string) RuleName {
	ruleNameComponents := strings.Split(rn, rulePartsSeparator)
	if len(ruleNameComponents) == 3 {
		return RuleName{
			cmName:   ruleNameComponents[0],
			tenantID: ruleNameComponents[1],
			filename: ruleNameComponents[2],
		}
	} else {
		return RuleName{
			tenantID: ruleNameComponents[0],
			filename: ruleNameComponents[1],
		}
	}
}

func (cm *ShardedConfigMap) Shard(opts *Options) []*corev1.ConfigMap {
	var keys []string
	for k := range cm.data {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	cm.configMapShards = []*corev1.ConfigMap{}
	currentCMIndex := 0
	currentCMSize := 0
	currentCM := cm.newConfigMapShard(currentCMIndex)

	for _, k := range keys {
		v := cm.data[k]
		dataSize := len(k) + len(v)
		if currentCMSize+dataSize > MaxConfigMapDataSizeBytes {
			cm.configMapShards = append(cm.configMapShards, currentCM)
			currentCMIndex++
			currentCMSize = 0
			currentCM = cm.newConfigMapShard(currentCMIndex)
		}
		// extract tenantID from the key to later match it
		// to the tenant when mounting to the pod
		tenantID := extractRuleNameComponents(k).tenantID

		// add the current configMap name to the rule file name
		// this is also to help mount rule files to the pod
		ruleFileName := fmt.Sprintf("%s%s%s", currentCM.Name, rulePartsSeparator, k)

		if tenant, ok := opts.Tenants.Configs[tenantID]; ok {
			tenant.RuleFiles = append(tenant.RuleFiles, ruleFileName)
			opts.Tenants.Configs[tenantID] = tenant
		}

		currentCMSize += dataSize

		// remove the tenantID from the file name
		k = extractRuleNameComponents(k).filename
		currentCM.Data[k] = v
	}
	cm.configMapShards = append(cm.configMapShards, currentCM)

	return cm.configMapShards
}
