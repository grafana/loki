package kfake

import (
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// Behavior:
//
// * If topic does not exist, we hang
// * Topic created while waiting is not returned in final response
// * If any partition is on a different broker, we return immediately
// * Out of range fetch causes early return
// * Raw bytes of batch counts against wait bytes

func init() { regKey(1, 4, 16) }

func (c *Cluster) handleFetch(creq *clientReq, w *watchFetch) (kmsg.Response, error) {
	var (
		req  = creq.kreq.(*kmsg.FetchRequest)
		resp = req.ResponseKind().(*kmsg.FetchResponse)
	)

	if err := checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	var (
		nbytes      int
		returnEarly bool
		needp       tps[int]
	)
	if w == nil {
	out:
		for i, rt := range req.Topics {
			if req.Version >= 13 {
				rt.Topic = c.data.id2t[rt.TopicID]
				req.Topics[i].Topic = rt.Topic
			}
			t, ok := c.data.tps.gett(rt.Topic)
			if !ok {
				continue
			}
			for _, rp := range rt.Partitions {
				pd, ok := t[rp.Partition]
				if !ok || pd.createdAt.After(creq.at) {
					continue
				}
				if pd.leader != creq.cc.b {
					returnEarly = true // NotLeaderForPartition
					break out
				}
				i, ok, atEnd := pd.searchOffset(rp.FetchOffset)
				if atEnd {
					continue
				}
				if !ok {
					returnEarly = true // OffsetOutOfRange
					break out
				}
				pbytes := 0
				for _, b := range pd.batches[i:] {
					nbytes += b.nbytes
					pbytes += b.nbytes
					if pbytes >= int(rp.PartitionMaxBytes) {
						returnEarly = true
						break out
					}
				}
				needp.set(rt.Topic, rp.Partition, int(rp.PartitionMaxBytes)-pbytes)
			}
		}
	}

	wait := time.Duration(req.MaxWaitMillis) * time.Millisecond
	deadline := creq.at.Add(wait)
	if w == nil && !returnEarly && nbytes < int(req.MinBytes) && time.Now().Before(deadline) {
		w := &watchFetch{
			need:     int(req.MinBytes) - nbytes,
			needp:    needp,
			deadline: deadline,
			creq:     creq,
		}
		w.cb = func() {
			select {
			case c.watchFetchCh <- w:
			case <-c.die:
			}
		}
		for _, rt := range req.Topics {
			t, ok := c.data.tps.gett(rt.Topic)
			if !ok {
				continue
			}
			for _, rp := range rt.Partitions {
				pd, ok := t[rp.Partition]
				if !ok || pd.createdAt.After(creq.at) {
					continue
				}
				pd.watch[w] = struct{}{}
				w.in = append(w.in, pd)
			}
		}
		w.t = time.AfterFunc(wait, w.cb)
		return nil, nil
	}

	id2t := make(map[uuid]string)
	tidx := make(map[string]int)

	donet := func(t string, id uuid, errCode int16) *kmsg.FetchResponseTopic {
		if i, ok := tidx[t]; ok {
			return &resp.Topics[i]
		}
		id2t[id] = t
		tidx[t] = len(resp.Topics)
		st := kmsg.NewFetchResponseTopic()
		st.Topic = t
		st.TopicID = id
		resp.Topics = append(resp.Topics, st)
		return &resp.Topics[len(resp.Topics)-1]
	}
	donep := func(t string, id uuid, p int32, errCode int16) *kmsg.FetchResponseTopicPartition {
		sp := kmsg.NewFetchResponseTopicPartition()
		sp.Partition = p
		sp.ErrorCode = errCode
		st := donet(t, id, 0)
		st.Partitions = append(st.Partitions, sp)
		return &st.Partitions[len(st.Partitions)-1]
	}

	var includeBrokers bool
	defer func() {
		if includeBrokers {
			for _, b := range c.bs {
				sb := kmsg.NewFetchResponseBroker()
				h, p, _ := net.SplitHostPort(b.ln.Addr().String())
				p32, _ := strconv.Atoi(p)
				sb.NodeID = b.node
				sb.Host = h
				sb.Port = int32(p32)
				resp.Brokers = append(resp.Brokers, sb)
			}
		}
	}()

	var batchesAdded int
full:
	for _, rt := range req.Topics {
		for _, rp := range rt.Partitions {
			pd, ok := c.data.tps.getp(rt.Topic, rp.Partition)
			if !ok {
				if req.Version >= 13 {
					donep(rt.Topic, rt.TopicID, rp.Partition, kerr.UnknownTopicID.Code)
				} else {
					donep(rt.Topic, rt.TopicID, rp.Partition, kerr.UnknownTopicOrPartition.Code)
				}
				continue
			}
			if pd.leader != creq.cc.b {
				p := donep(rt.Topic, rt.TopicID, rp.Partition, kerr.NotLeaderForPartition.Code)
				p.CurrentLeader.LeaderID = pd.leader.node
				p.CurrentLeader.LeaderEpoch = pd.epoch
				includeBrokers = true
				continue
			}
			sp := donep(rt.Topic, rt.TopicID, rp.Partition, 0)
			sp.HighWatermark = pd.highWatermark
			sp.LastStableOffset = pd.lastStableOffset
			sp.LogStartOffset = pd.logStartOffset
			i, ok, atEnd := pd.searchOffset(rp.FetchOffset)
			if atEnd {
				continue
			}
			if !ok {
				sp.ErrorCode = kerr.OffsetOutOfRange.Code
				continue
			}
			var pbytes int
			for _, b := range pd.batches[i:] {
				if nbytes = nbytes + b.nbytes; nbytes > int(req.MaxBytes) && batchesAdded > 1 {
					break full
				}
				if pbytes = pbytes + b.nbytes; pbytes > int(rp.PartitionMaxBytes) && batchesAdded > 1 {
					break
				}
				batchesAdded++
				sp.RecordBatches = b.AppendTo(sp.RecordBatches)
			}
		}
	}

	return resp, nil
}

type watchFetch struct {
	need     int
	needp    tps[int]
	deadline time.Time
	creq     *clientReq

	in []*partData
	cb func()
	t  *time.Timer

	once    sync.Once
	cleaned bool
}

func (w *watchFetch) push(nbytes int) {
	w.need -= nbytes
	if w.need <= 0 {
		w.once.Do(func() {
			go w.cb()
		})
	}
}

func (w *watchFetch) deleted() {
	w.once.Do(func() {
		go w.cb()
	})
}

func (w *watchFetch) cleanup(c *Cluster) {
	w.cleaned = true
	for _, in := range w.in {
		delete(in.watch, w)
	}
	w.t.Stop()
}
