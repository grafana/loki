package kfake

import "sort"

type tps[V any] map[string]map[int32]*V

func (tps *tps[V]) getp(t string, p int32) (*V, bool) {
	if *tps == nil {
		return nil, false
	}
	ps := (*tps)[t]
	if ps == nil {
		return nil, false
	}
	v, ok := ps[p]
	return v, ok
}

func (tps *tps[V]) gett(t string) (map[int32]*V, bool) {
	if tps == nil {
		return nil, false
	}
	ps, ok := (*tps)[t]
	return ps, ok
}

func (tps *tps[V]) mkt(t string) map[int32]*V {
	if *tps == nil {
		*tps = make(map[string]map[int32]*V)
	}
	ps := (*tps)[t]
	if ps == nil {
		ps = make(map[int32]*V)
		(*tps)[t] = ps
	}
	return ps
}

func (tps *tps[V]) mkp(t string, p int32, newFn func() *V) *V {
	ps := tps.mkt(t)
	v, ok := ps[p]
	if !ok {
		v = newFn()
		ps[p] = v
	}
	return v
}

func (tps *tps[V]) mkpDefault(t string, p int32) *V {
	return tps.mkp(t, p, func() *V { return new(V) })
}

func (tps *tps[V]) set(t string, p int32, v V) {
	*tps.mkpDefault(t, p) = v
}

func (tps *tps[V]) each(fn func(t string, p int32, v *V)) {
	for t, ps := range *tps {
		for p, v := range ps {
			fn(t, p, v)
		}
	}
}

func (tps *tps[V]) delp(t string, p int32) {
	if *tps == nil {
		return
	}
	ps := (*tps)[t]
	if ps == nil {
		return
	}
	delete(ps, p)
	if len(ps) == 0 {
		delete(*tps, t)
	}
}

// TopicInfo contains snapshot-in-time metadata about an existing topic.
type TopicInfo struct {
	Topic       string             // Topic is the topic this info is for.
	TopicID     [16]byte           // TopicID is the UUID of the topic.
	NumReplicas int                // NumReplicas is the replication factor for all partitions in this topic.
	Configs     map[string]*string // Configs contains all configuration values specified for this topic.
}

// PartitionInfo contains snapshot-in-time metadata about an existing partition.
type PartitionInfo struct {
	Partition        int32 // Partition is the partition this info is for.
	HighWatermark    int64 // HighWatermark is the latest offset present in the partition.
	LastStableOffset int64 // LastStableOffset is the last stable offset.
	LogStartOffset   int64 // LogStartOffsets is the first offset present in the partition.
	Epoch            int32 //  Epoch is the current "epoch" of the partition -- how many times the partition transferred leaders.
	MaxTimestamp     int64 // MaxTimestamp is the current max timestamp across all batches.
	NumBytes         int64 // NumBytes is the current amount of data stored in the partition.
	Leader           int32 // Leader is the current leader of the partition.
}

func (pd *partData) info() *PartitionInfo {
	return &PartitionInfo{
		Partition:        pd.p,
		HighWatermark:    pd.highWatermark,
		LastStableOffset: pd.lastStableOffset,
		LogStartOffset:   pd.logStartOffset,
		Epoch:            pd.epoch,
		MaxTimestamp:     pd.maxTimestamp,
		NumBytes:         pd.nbytes,
		Leader:           pd.leader.node,
	}
}

func cloneConfigs(m map[string]*string) map[string]*string { // a deeper maps.Clone
	m2 := make(map[string]*string, len(m))
	for k, v := range m {
		var v2 *string
		if v != nil {
			vv := *v
			v2 = &vv
		}
		m2[k] = v2
	}
	return m2
}

// TopicInfo returns information about a topic if it exists.
func (c *Cluster) TopicInfo(topic string) *TopicInfo {
	var i *TopicInfo
	c.admin(func() {
		id, exists := c.data.t2id[topic]
		if !exists {
			return
		}
		i = &TopicInfo{
			Topic:       topic,
			TopicID:     id,
			NumReplicas: c.data.treplicas[topic],
			Configs:     cloneConfigs(c.data.tcfgs[topic]),
		}
	})
	return i
}

// TopicIDInfo returns the topic for a topic ID if the topic ID exists.
func (c *Cluster) TopicIDInfo(id [16]byte) *TopicInfo {
	var i *TopicInfo
	c.admin(func() {
		topic, exists := c.data.id2t[id]
		if !exists {
			return
		}
		i = &TopicInfo{
			Topic:       topic,
			TopicID:     id,
			NumReplicas: c.data.treplicas[topic],
			Configs:     cloneConfigs(c.data.tcfgs[topic]),
		}
	})
	return i
}

// PartitionInfo returns information about a partition if it exists.
func (c *Cluster) PartitionInfo(topic string, partition int32) *PartitionInfo {
	var i *PartitionInfo
	c.admin(func() {
		pd, ok := c.data.tps.getp(topic, partition)
		if !ok {
			return
		}
		i = pd.info()
	})
	return i
}

// PartitionInfos returns information about all partitions in a topic,
// if it exists. The partitions are returned in sorted partition order,
// with partition 0 at index 0, partition 1 at index 1, etc.
func (c *Cluster) PartitionInfos(topic string) []*PartitionInfo {
	var is []*PartitionInfo
	c.admin(func() {
		t, ok := c.data.tps.gett(topic)
		if !ok {
			return
		}
		partitions := make([]int32, 0, len(t))
		for p := range t {
			partitions = append(partitions, p)
		}
		sort.Slice(partitions, func(i, j int) bool {
			return partitions[i] < partitions[j]
		})
		for _, p := range partitions {
			pd, _ := c.data.tps.getp(topic, p)
			is = append(is, pd.info())
		}
	})
	return is
}
