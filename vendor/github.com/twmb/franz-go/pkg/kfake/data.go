package kfake

import (
	"crypto/sha256"
	"math/rand"
	"sort"
	"strconv"
	"time"

	"github.com/twmb/franz-go/pkg/kmsg"
)

// TODO
//
// * Write to disk, if configured.
// * When transactional, wait to send out data until txn committed or aborted.

var noID uuid

type (
	uuid [16]byte

	data struct {
		c   *Cluster
		tps tps[partData]

		id2t      map[uuid]string               // topic IDs => topic name
		t2id      map[string]uuid               // topic name => topic IDs
		treplicas map[string]int                // topic name => # replicas
		tcfgs     map[string]map[string]*string // topic name => config name => config value
	}

	partData struct {
		batches []partBatch
		dir     string

		highWatermark    int64
		lastStableOffset int64
		logStartOffset   int64
		epoch            int32 // current epoch
		maxTimestamp     int64 // current max timestamp in all batches
		nbytes           int64

		// abortedTxns
		rf     int8
		leader *broker

		watch map[*watchFetch]struct{}

		createdAt time.Time
	}

	partBatch struct {
		kmsg.RecordBatch
		nbytes int
		epoch  int32 // epoch when appended

		// For list offsets, we may need to return the first offset
		// after a given requested timestamp. Client provided
		// timestamps gan go forwards and backwards. We answer list
		// offsets with a binary search: even if this batch has a small
		// timestamp, this is produced _after_ a potentially higher
		// timestamp, so it is after it in the list offset response.
		//
		// When we drop the earlier timestamp, we update all following
		// firstMaxTimestamps that match the dropped timestamp.
		maxEarlierTimestamp int64
	}
)

func (d *data) mkt(t string, nparts int, nreplicas int, configs map[string]*string) {
	if d.tps != nil {
		if _, exists := d.tps[t]; exists {
			panic("should have checked existence already")
		}
	}
	var id uuid
	for {
		sha := sha256.Sum256([]byte(strconv.Itoa(int(time.Now().UnixNano()))))
		copy(id[:], sha[:])
		if _, exists := d.id2t[id]; !exists {
			break
		}
	}

	if nparts < 0 {
		nparts = d.c.cfg.defaultNumParts
	}
	if nreplicas < 0 {
		nreplicas = 3 // cluster default
	}
	d.id2t[id] = t
	d.t2id[t] = id
	d.treplicas[t] = nreplicas
	if configs != nil {
		d.tcfgs[t] = configs
	}
	for i := 0; i < nparts; i++ {
		d.tps.mkp(t, int32(i), d.c.newPartData)
	}
}

func (c *Cluster) noLeader() *broker {
	return &broker{
		c:    c,
		node: -1,
	}
}

func (c *Cluster) newPartData() *partData {
	return &partData{
		dir:       defLogDir,
		leader:    c.bs[rand.Intn(len(c.bs))],
		watch:     make(map[*watchFetch]struct{}),
		createdAt: time.Now(),
	}
}

func (pd *partData) pushBatch(nbytes int, b kmsg.RecordBatch) {
	maxEarlierTimestamp := b.FirstTimestamp
	if maxEarlierTimestamp < pd.maxTimestamp {
		maxEarlierTimestamp = pd.maxTimestamp
	} else {
		pd.maxTimestamp = maxEarlierTimestamp
	}
	b.FirstOffset = pd.highWatermark
	b.PartitionLeaderEpoch = pd.epoch
	pd.batches = append(pd.batches, partBatch{b, nbytes, pd.epoch, maxEarlierTimestamp})
	pd.highWatermark += int64(b.NumRecords)
	pd.lastStableOffset += int64(b.NumRecords) // TODO
	pd.nbytes += int64(nbytes)
	for w := range pd.watch {
		w.push(nbytes)
	}
}

func (pd *partData) searchOffset(o int64) (index int, found bool, atEnd bool) {
	if o < pd.logStartOffset || o > pd.highWatermark {
		return 0, false, false
	}
	if len(pd.batches) == 0 {
		if o == 0 {
			return 0, false, true
		}
	} else {
		lastBatch := pd.batches[len(pd.batches)-1]
		if end := lastBatch.FirstOffset + int64(lastBatch.LastOffsetDelta) + 1; end == o {
			return 0, false, true
		}
	}

	index, found = sort.Find(len(pd.batches), func(idx int) int {
		b := &pd.batches[idx]
		if o < b.FirstOffset {
			return -1
		}
		if o >= b.FirstOffset+int64(b.LastOffsetDelta)+1 {
			return 1
		}
		return 0
	})
	return index, found, false
}

func (pd *partData) trimLeft() {
	for len(pd.batches) > 0 {
		b0 := pd.batches[0]
		finRec := b0.FirstOffset + int64(b0.LastOffsetDelta)
		if finRec >= pd.logStartOffset {
			return
		}
		pd.batches = pd.batches[1:]
		pd.nbytes -= int64(b0.nbytes)
	}
}

/////////////
// CONFIGS //
/////////////

// TODO support modifying config values changing cluster behavior

// brokerConfigs calls fn for all:
//   - static broker configs (read only)
//   - default configs
//   - dynamic broker configs
func (c *Cluster) brokerConfigs(node int32, fn func(k string, v *string, src kmsg.ConfigSource, sensitive bool)) {
	if node >= 0 {
		for _, b := range c.bs {
			if b.node == node {
				id := strconv.Itoa(int(node))
				fn("broker.id", &id, kmsg.ConfigSourceStaticBrokerConfig, false)
				break
			}
		}
	}
	for _, c := range []struct {
		k    string
		v    string
		sens bool
	}{
		{k: "broker.rack", v: "krack"},
		{k: "sasl.enabled.mechanisms", v: "PLAIN,SCRAM-SHA-256,SCRAM-SHA-512"},
		{k: "super.users", sens: true},
	} {
		v := c.v
		fn(c.k, &v, kmsg.ConfigSourceStaticBrokerConfig, c.sens)
	}

	for k, v := range configDefaults {
		if _, ok := validBrokerConfigs[k]; ok {
			v := v
			fn(k, &v, kmsg.ConfigSourceDefaultConfig, false)
		}
	}

	for k, v := range c.bcfgs {
		fn(k, v, kmsg.ConfigSourceDynamicBrokerConfig, false)
	}
}

// configs calls fn for all
//   - static broker configs (read only)
//   - default configs
//   - dynamic broker configs
//   - dynamic topic configs
//
// This differs from brokerConfigs by also including dynamic topic configs.
func (d *data) configs(t string, fn func(k string, v *string, src kmsg.ConfigSource, sensitive bool)) {
	for k, v := range configDefaults {
		if _, ok := validTopicConfigs[k]; ok {
			v := v
			fn(k, &v, kmsg.ConfigSourceDefaultConfig, false)
		}
	}
	for k, v := range d.c.bcfgs {
		if topicEquiv, ok := validBrokerConfigs[k]; ok && topicEquiv != "" {
			fn(k, v, kmsg.ConfigSourceDynamicBrokerConfig, false)
		}
	}
	for k, v := range d.tcfgs[t] {
		fn(k, v, kmsg.ConfigSourceDynamicTopicConfig, false)
	}
}

// Unlike Kafka, we validate the value before allowing it to be set.
func (c *Cluster) setBrokerConfig(k string, v *string, dry bool) bool {
	if dry {
		return true
	}
	c.bcfgs[k] = v
	return true
}

func (d *data) setTopicConfig(t string, k string, v *string, dry bool) bool {
	if dry {
		return true
	}
	if _, ok := d.tcfgs[t]; !ok {
		d.tcfgs[t] = make(map[string]*string)
	}
	d.tcfgs[t][k] = v
	return true
}

// All valid topic configs we support, as well as the equivalent broker
// config if there is one.
var validTopicConfigs = map[string]string{
	"cleanup.policy":         "",
	"compression.type":       "compression.type",
	"max.message.bytes":      "log.message.max.bytes",
	"message.timestamp.type": "log.message.timestamp.type",
	"min.insync.replicas":    "min.insync.replicas",
	"retention.bytes":        "log.retention.bytes",
	"retention.ms":           "log.retention.ms",
}

// All valid broker configs we support, as well as their equivalent
// topic config if there is one.
var validBrokerConfigs = map[string]string{
	"broker.id":                  "",
	"broker.rack":                "",
	"compression.type":           "compression.type",
	"default.replication.factor": "",
	"fetch.max.bytes":            "",
	"log.dir":                    "",
	"log.message.timestamp.type": "message.timestamp.type",
	"log.retention.bytes":        "retention.bytes",
	"log.retention.ms":           "retention.ms",
	"message.max.bytes":          "max.message.bytes",
	"min.insync.replicas":        "min.insync.replicas",
	"sasl.enabled.mechanisms":    "",
	"super.users":                "",
}

// Default topic and broker configs.
var configDefaults = map[string]string{
	"cleanup.policy":         "delete",
	"compression.type":       "producer",
	"max.message.bytes":      "1048588",
	"message.timestamp.type": "CreateTime",
	"min.insync.replicas":    "1",
	"retention.bytes":        "-1",
	"retention.ms":           "604800000",

	"default.replication.factor": "3",
	"fetch.max.bytes":            "57671680",
	"log.dir":                    defLogDir,
	"log.message.timestamp.type": "CreateTime",
	"log.retention.bytes":        "-1",
	"log.retention.ms":           "604800000",
	"message.max.bytes":          "1048588",
}

const defLogDir = "/mem/kfake"

func staticConfig(s ...string) func(*string) bool {
	return func(v *string) bool {
		if v == nil {
			return false
		}
		for _, ok := range s {
			if *v == ok {
				return true
			}
		}
		return false
	}
}

func numberConfig(min int, hasMin bool, max int, hasMax bool) func(*string) bool {
	return func(v *string) bool {
		if v == nil {
			return false
		}
		i, err := strconv.Atoi(*v)
		if err != nil {
			return false
		}
		if hasMin && i < min || hasMax && i > max {
			return false
		}
		return true
	}
}
