package manifests

import (
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
)

// MaxConfigMapDataSizeBytes is the maximum data size in bytes that a single ConfigMap
// may contain. This is lower than 1MB  in order to reserve space for
// metadata and the rest of the ConfigMap k8s object.
const MaxConfigMapDataSizeBytes = (1 * 1024 * 1024) - 50_000

// ShardedConfigMap is the configmap data that is sharded across multiple
// k8s configmaps in case the maximum data size is exceeded
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

/* func (cm *ShardedConfigMap) AppendData(key string, data string) {
	if cm == nil {
		return
	}
	cm.data[key] = data
} */

func (cm *ShardedConfigMap) newConfigMapShard(index int) *corev1.ConfigMap {
	newShardCM := cm.template.DeepCopy()
	newShardCM.Data = make(map[string]string)
	newShardCM.Name = makeShardConfigMapName(newShardCM.Name, index)
	return newShardCM
}

func makeShardConfigMapName(prefix string, index int) string {
	return fmt.Sprintf("%s-%d", prefix, index)
}

func (cm *ShardedConfigMap) Shard() []*corev1.ConfigMap {
	var keys []string
	for k := range cm.data {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	cm.configMapShards = []*corev1.ConfigMap{}
	currentCMIndex := 0
	currentCMSize := 0
	currentCM := cm.newConfigMapShard(currentCMIndex)

	for _, key := range keys {
		v := cm.data[key]
		dataSize := len(key) + len(v)
		if currentCMSize+dataSize > MaxConfigMapDataSizeBytes {
			cm.configMapShards = append(cm.configMapShards, currentCM)
			currentCMIndex++
			currentCMSize = 0
			currentCM = cm.newConfigMapShard(currentCMIndex)
		}
		currentCMSize += dataSize
		currentCM.Data[key] = v
	}
	cm.configMapShards = append(cm.configMapShards, currentCM)
	return cm.configMapShards
}
