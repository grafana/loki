package kadm

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"sort"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// TopicID is the 16 byte underlying topic ID.
type TopicID [16]byte

// String returns the topic ID encoded as base64.
func (t TopicID) String() string { return base64.StdEncoding.EncodeToString(t[:]) }

// MarshalJSON returns the topic ID encoded as quoted base64.
func (t TopicID) MarshalJSON() ([]byte, error) { return []byte(`"` + t.String() + `"`), nil }

// Less returns if this ID is less than the other, byte by byte.
func (t TopicID) Less(other TopicID) bool {
	return bytes.Compare(t[:], other[:]) == -1
}

// PartitionDetail is the detail of a partition as returned by a metadata
// response. If the partition fails to load / has an error, then only the
// partition number itself and the Err fields will be set.
type PartitionDetail struct {
	Topic     string // Topic is the topic this partition belongs to.
	Partition int32  // Partition is the partition number these details are for.

	Leader          int32   // Leader is the broker leader, if there is one, otherwise -1.
	LeaderEpoch     int32   // LeaderEpoch is the leader's current epoch.
	Replicas        []int32 // Replicas is the list of replicas.
	ISR             []int32 // ISR is the list of in sync replicas.
	OfflineReplicas []int32 // OfflineReplicas is the list of offline replicas.

	Err error // Err is non-nil if the partition currently has a load error.
}

// PartitionDetails contains details for partitions as returned by a metadata
// response.
type PartitionDetails map[int32]PartitionDetail

// Sorted returns the partitions in sorted order.
func (ds PartitionDetails) Sorted() []PartitionDetail {
	s := make([]PartitionDetail, 0, len(ds))
	for _, d := range ds {
		s = append(s, d)
	}
	sort.Slice(s, func(i, j int) bool { return s[i].Partition < s[j].Partition })
	return s
}

// Numbers returns a sorted list of all partition numbers.
func (ds PartitionDetails) Numbers() []int32 {
	all := make([]int32, 0, len(ds))
	for p := range ds {
		all = append(all, p)
	}
	return int32s(all)
}

// NumReplicas returns the number of replicas for these partitions
//
// It is assumed that all partitions have the same number of replicas, so this
// simply returns the number of replicas in the first encountered partition.
func (ds PartitionDetails) NumReplicas() int {
	for _, p := range ds {
		return len(p.Replicas)
	}
	return 0
}

// TopicDetail is the detail of a topic as returned by a metadata response. If
// the topic fails to load / has an error, then there will be no partitions.
type TopicDetail struct {
	Topic string // Topic is the topic these details are for.

	ID         TopicID          // TopicID is the topic's ID, or all 0 if the broker does not support IDs.
	IsInternal bool             // IsInternal is whether the topic is an internal topic.
	Partitions PartitionDetails // Partitions contains details about the topic's partitions.

	Err error // Err is non-nil if the topic could not be loaded.
}

// TopicDetails contains details for topics as returned by a metadata response.
type TopicDetails map[string]TopicDetail

// Topics returns a sorted list of all topic names.
func (ds TopicDetails) Names() []string {
	all := make([]string, 0, len(ds))
	for t := range ds {
		all = append(all, t)
	}
	sort.Strings(all)
	return all
}

// Sorted returns all topics in sorted order.
func (ds TopicDetails) Sorted() []TopicDetail {
	s := make([]TopicDetail, 0, len(ds))
	for _, d := range ds {
		s = append(s, d)
	}
	sort.Slice(s, func(i, j int) bool {
		if s[i].Topic == "" {
			if s[j].Topic == "" {
				return bytes.Compare(s[i].ID[:], s[j].ID[:]) == -1
			}
			return true
		}
		if s[j].Topic == "" {
			return false
		}
		return s[i].Topic < s[j].Topic
	})
	return s
}

// Has returns whether the topic details has the given topic and, if so, that
// the topic's load error is not an unknown topic error.
func (ds TopicDetails) Has(topic string) bool {
	d, ok := ds[topic]
	return ok && d.Err != kerr.UnknownTopicOrPartition
}

// FilterInternal deletes any internal topics from this set of topic details.
func (ds TopicDetails) FilterInternal() {
	for t, d := range ds {
		if d.IsInternal {
			delete(ds, t)
		}
	}
}

// EachPartition calls fn for every partition in all topics.
func (ds TopicDetails) EachPartition(fn func(PartitionDetail)) {
	for _, td := range ds {
		for _, d := range td.Partitions {
			fn(d)
		}
	}
}

// EachError calls fn for each topic that could not be loaded.
func (ds TopicDetails) EachError(fn func(TopicDetail)) {
	for _, td := range ds {
		if td.Err != nil {
			fn(td)
		}
	}
}

// Error iterates over all topic details and returns the first error
// encountered, if any.
func (ds TopicDetails) Error() error {
	for _, t := range ds {
		if t.Err != nil {
			return t.Err
		}
	}
	return nil
}

// TopicsSet returns the topics and partitions as a set.
func (ds TopicDetails) TopicsSet() TopicsSet {
	var s TopicsSet
	ds.EachPartition(func(d PartitionDetail) {
		s.Add(d.Topic, d.Partition)
	})
	return s
}

// TopicsList returns the topics and partitions as a list.
func (ds TopicDetails) TopicsList() TopicsList {
	return ds.TopicsSet().Sorted()
}

// Metadata is the data from a metadata response.
type Metadata struct {
	Cluster    string        // Cluster is the cluster name, if any.
	Controller int32         // Controller is the node ID of the controller broker, if available, otherwise -1.
	Brokers    BrokerDetails // Brokers contains broker details, sorted by default.
	Topics     TopicDetails  // Topics contains topic details.
}

func int32s(is []int32) []int32 {
	sort.Slice(is, func(i, j int) bool { return is[i] < is[j] })
	return is
}

// ListBrokers issues a metadata request and returns BrokerDetails. This
// returns an error if the request fails to be issued, or an *AuthError.
func (cl *Client) ListBrokers(ctx context.Context) (BrokerDetails, error) {
	m, err := cl.Metadata(ctx)
	if err != nil {
		return nil, err
	}
	return m.Brokers, nil
}

// BrokerMetadata issues a metadata request and returns it, and does not ask
// for any topics.
//
// This returns an error if the request fails to be issued, or an *AuthErr.
func (cl *Client) BrokerMetadata(ctx context.Context) (Metadata, error) {
	return cl.metadata(ctx, true, nil)
}

// Metadata issues a metadata request and returns it. Specific topics to
// describe can be passed as additional arguments. If no topics are specified,
// all topics are requested.
//
// This returns an error if the request fails to be issued, or an *AuthErr.
func (cl *Client) Metadata(
	ctx context.Context,
	topics ...string,
) (Metadata, error) {
	return cl.metadata(ctx, false, topics)
}

func (cl *Client) metadata(ctx context.Context, noTopics bool, topics []string) (Metadata, error) {
	req := kmsg.NewPtrMetadataRequest()
	for _, t := range topics {
		rt := kmsg.NewMetadataRequestTopic()
		rt.Topic = kmsg.StringPtr(t)
		req.Topics = append(req.Topics, rt)
	}
	if noTopics {
		req.Topics = []kmsg.MetadataRequestTopic{}
	}
	resp, err := req.RequestWith(ctx, cl.cl)
	if err != nil {
		return Metadata{}, err
	}

	tds := make(map[string]TopicDetail, len(resp.Topics))
	for _, t := range resp.Topics {
		if err := maybeAuthErr(t.ErrorCode); err != nil {
			return Metadata{}, err
		}
		td := TopicDetail{
			ID:         t.TopicID,
			Partitions: make(map[int32]PartitionDetail),
			IsInternal: t.IsInternal,
			Err:        kerr.ErrorForCode(t.ErrorCode),
		}
		if t.Topic != nil {
			td.Topic = *t.Topic
		}
		for _, p := range t.Partitions {
			td.Partitions[p.Partition] = PartitionDetail{
				Topic:     td.Topic,
				Partition: p.Partition,

				Leader:          p.Leader,
				LeaderEpoch:     p.LeaderEpoch,
				Replicas:        p.Replicas,
				ISR:             p.ISR,
				OfflineReplicas: p.OfflineReplicas,

				Err: kerr.ErrorForCode(p.ErrorCode),
			}
		}
		tds[*t.Topic] = td
	}

	m := Metadata{
		Controller: resp.ControllerID,
		Topics:     tds,
	}
	if resp.ClusterID != nil {
		m.Cluster = *resp.ClusterID
	}

	for _, b := range resp.Brokers {
		m.Brokers = append(m.Brokers, kgo.BrokerMetadata{
			NodeID: b.NodeID,
			Host:   b.Host,
			Port:   b.Port,
			Rack:   b.Rack,
		})
	}
	sort.Slice(m.Brokers, func(i, j int) bool { return m.Brokers[i].NodeID < m.Brokers[j].NodeID })

	if len(topics) > 0 && len(m.Topics) != len(topics) {
		return Metadata{}, fmt.Errorf("metadata returned only %d topics of %d requested", len(m.Topics), len(topics))
	}

	return m, nil
}

// ListedOffset contains record offset information.
type ListedOffset struct {
	Topic     string // Topic is the topic this offset is for.
	Partition int32  // Partition is the partition this offset is for.

	Timestamp   int64 // Timestamp is the millisecond of the offset if listing after a time, otherwise -1.
	Offset      int64 // Offset is the record offset, or -1 if one could not be found.
	LeaderEpoch int32 // LeaderEpoch is the leader epoch at this offset, if any, otherwise -1.

	Err error // Err is non-nil if the partition has a load error.
}

// ListedOffsets contains per-partition record offset information that is
// returned from any of the List.*Offsets functions.
type ListedOffsets map[string]map[int32]ListedOffset

// Lookup returns the offset at t and p and whether it exists.
func (l ListedOffsets) Lookup(t string, p int32) (ListedOffset, bool) {
	if len(l) == 0 {
		return ListedOffset{}, false
	}
	ps := l[t]
	if len(ps) == 0 {
		return ListedOffset{}, false
	}
	o, exists := ps[p]
	return o, exists
}

// Each calls fn for each listed offset.
func (l ListedOffsets) Each(fn func(ListedOffset)) {
	for _, ps := range l {
		for _, o := range ps {
			fn(o)
		}
	}
}

// Error iterates over all offsets and returns the first error encountered, if
// any. This can be to check if a listing was entirely successful or not.
//
// Note that offset listing can be partially successful. For example, some
// offsets could succeed to be listed, while other could fail (maybe one
// partition is offline). If this is something you need to worry about, you may
// need to check all offsets manually.
func (l ListedOffsets) Error() error {
	for _, ps := range l {
		for _, o := range ps {
			if o.Err != nil {
				return o.Err
			}
		}
	}
	return nil
}

// Offsets returns these listed offsets as offsets.
func (l ListedOffsets) Offsets() Offsets {
	o := make(Offsets)
	l.Each(func(l ListedOffset) {
		o.Add(Offset{
			Topic:       l.Topic,
			Partition:   l.Partition,
			At:          l.Offset,
			LeaderEpoch: l.LeaderEpoch,
		})
	})
	return o
}

// KOffsets returns these listed offsets as a kgo offset map.
func (l ListedOffsets) KOffsets() map[string]map[int32]kgo.Offset {
	return l.Offsets().KOffsets()
}

// ListStartOffsets returns the start (oldest) offsets for each partition in
// each requested topic. In Kafka terms, this returns the log start offset. If
// no topics are specified, all topics are listed. If a requested topic does
// not exist, no offsets for it are listed and it is not present in the
// response.
//
// If any topics being listed do not exist, a special -1 partition is added
// to the response with the expected error code kerr.UnknownTopicOrPartition.
//
// This may return *ShardErrors.
func (cl *Client) ListStartOffsets(ctx context.Context, topics ...string) (ListedOffsets, error) {
	return cl.listOffsets(ctx, 0, -2, topics)
}

// ListEndOffsets returns the end (newest) offsets for each partition in each
// requested topic. In Kafka terms, this returns high watermarks. If no topics
// are specified, all topics are listed. If a requested topic does not exist,
// no offsets for it are listed and it is not present in the response.
//
// If any topics being listed do not exist, a special -1 partition is added
// to the response with the expected error code kerr.UnknownTopicOrPartition.
//
// This may return *ShardErrors.
func (cl *Client) ListEndOffsets(ctx context.Context, topics ...string) (ListedOffsets, error) {
	return cl.listOffsets(ctx, 0, -1, topics)
}

// ListCommittedOffsets returns newest committed offsets for each partition in
// each requested topic. A committed offset may be slightly less than the
// latest offset. In Kafka terms, committed means the last stable offset, and
// newest means the high watermark. Record offsets in active, uncommitted
// transactions will not be returned. If no topics are specified, all topics
// are listed. If a requested topic does not exist, no offsets for it are
// listed and it is not present in the response.
//
// If any topics being listed do not exist, a special -1 partition is added
// to the response with the expected error code kerr.UnknownTopicOrPartition.
//
// This may return *ShardErrors.
func (cl *Client) ListCommittedOffsets(ctx context.Context, topics ...string) (ListedOffsets, error) {
	return cl.listOffsets(ctx, 1, -1, topics)
}

// ListOffsetsAfterMilli returns the first offsets after the requested
// millisecond timestamp. Unlike listing start/end/committed offsets, offsets
// returned from this function also include the timestamp of the offset. If no
// topics are specified, all topics are listed. If a partition has no offsets
// after the requested millisecond, the offset will be the current end offset.
// If a requested topic does not exist, no offsets for it are listed and it is
// not present in the response.
//
// If any topics being listed do not exist, a special -1 partition is added
// to the response with the expected error code kerr.UnknownTopicOrPartition.
//
// This may return *ShardErrors.
func (cl *Client) ListOffsetsAfterMilli(ctx context.Context, millisecond int64, topics ...string) (ListedOffsets, error) {
	return cl.listOffsets(ctx, 0, millisecond, topics)
}

func (cl *Client) listOffsets(ctx context.Context, isolation int8, timestamp int64, topics []string) (ListedOffsets, error) {
	tds, err := cl.ListTopics(ctx, topics...)
	if err != nil {
		return nil, err
	}

	// If we request with timestamps, we may request twice: once for after
	// timestamps, and once for any -1 (and no error) offsets where the
	// timestamp is in the future.
	list := make(ListedOffsets)

	for _, td := range tds {
		if td.Err != nil {
			list[td.Topic] = map[int32]ListedOffset{
				-1: {
					Topic:     td.Topic,
					Partition: -1,
					Err:       td.Err,
				},
			}
		}
	}
	rerequest := make(map[string][]int32)
	shardfn := func(kr kmsg.Response) error {
		resp := kr.(*kmsg.ListOffsetsResponse)
		for _, t := range resp.Topics {
			lt, ok := list[t.Topic]
			if !ok {
				lt = make(map[int32]ListedOffset)
				list[t.Topic] = lt
			}
			for _, p := range t.Partitions {
				if err := maybeAuthErr(p.ErrorCode); err != nil {
					return err
				}
				lt[p.Partition] = ListedOffset{
					Topic:       t.Topic,
					Partition:   p.Partition,
					Timestamp:   p.Timestamp,
					Offset:      p.Offset,
					LeaderEpoch: p.LeaderEpoch,
					Err:         kerr.ErrorForCode(p.ErrorCode),
				}
				if timestamp != -1 && p.Offset == -1 && p.ErrorCode == 0 {
					rerequest[t.Topic] = append(rerequest[t.Topic], p.Partition)
				}
			}
		}
		return nil
	}

	req := kmsg.NewPtrListOffsetsRequest()
	req.IsolationLevel = isolation
	for t, td := range tds {
		rt := kmsg.NewListOffsetsRequestTopic()
		if td.Err != nil {
			continue
		}
		rt.Topic = t
		for p := range td.Partitions {
			rp := kmsg.NewListOffsetsRequestTopicPartition()
			rp.Partition = p
			rp.Timestamp = timestamp
			rt.Partitions = append(rt.Partitions, rp)
		}
		req.Topics = append(req.Topics, rt)
	}
	shards := cl.cl.RequestSharded(ctx, req)
	err = shardErrEach(req, shards, shardfn)
	if len(rerequest) > 0 {
		req.Topics = req.Topics[:0]
		for t, ps := range rerequest {
			rt := kmsg.NewListOffsetsRequestTopic()
			rt.Topic = t
			for _, p := range ps {
				rp := kmsg.NewListOffsetsRequestTopicPartition()
				rp.Partition = p
				rp.Timestamp = -1 // we always list end offsets when rerequesting
				rt.Partitions = append(rt.Partitions, rp)
			}
			req.Topics = append(req.Topics, rt)
		}
		shards = cl.cl.RequestSharded(ctx, req)
		err = mergeShardErrs(err, shardErrEach(req, shards, shardfn))
	}
	return list, err
}
