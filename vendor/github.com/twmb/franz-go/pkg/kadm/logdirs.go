package kadm

import (
	"context"
	"sort"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// AlterReplicaLogDirsReq is the input for a request to alter replica log
// directories. The key is the directory that all topics and partitions in
// the topic set will move to.
type AlterReplicaLogDirsReq map[string]TopicsSet

// Add merges the input topic set into the given directory.
func (r *AlterReplicaLogDirsReq) Add(d string, s TopicsSet) {
	if *r == nil {
		*r = make(map[string]TopicsSet)
	}
	existing := (*r)[d]
	if existing == nil {
		existing = make(TopicsSet)
		(*r)[d] = existing
	}
	existing.Merge(s)
}

func (r AlterReplicaLogDirsReq) req() *kmsg.AlterReplicaLogDirsRequest {
	req := kmsg.NewPtrAlterReplicaLogDirsRequest()
	for dir, ts := range r {
		rd := kmsg.NewAlterReplicaLogDirsRequestDir()
		rd.Dir = dir
		for t, ps := range ts {
			rt := kmsg.NewAlterReplicaLogDirsRequestDirTopic()
			rt.Topic = t
			for p := range ps {
				rt.Partitions = append(rt.Partitions, p)
			}
			rd.Topics = append(rd.Topics, rt)
		}
		req.Dirs = append(req.Dirs, rd)
	}
	return req
}

func (r AlterReplicaLogDirsReq) dirfor(t string, p int32) string {
	for d, dts := range r {
		if dts == nil {
			continue
		}
		dtps, ok := dts[t] // does this dir contain this topic?
		if !ok {
			continue
		}
		if _, ok = dtps[p]; !ok { // does this topic in this dir contain this partition?
			continue
		}
		return d // yes
	}
	return ""
}

// AlterAllReplicaLogDirsResponses contains per-broker responses to altered
// partition directories.
type AlterAllReplicaLogDirsResponses map[int32]AlterReplicaLogDirsResponses

// Sorted returns the responses sorted by broker, topic, and partition.
func (rs AlterAllReplicaLogDirsResponses) Sorted() []AlterReplicaLogDirsResponse {
	var all []AlterReplicaLogDirsResponse
	rs.Each(func(r AlterReplicaLogDirsResponse) {
		all = append(all, r)
	})
	sort.Slice(all, func(i, j int) bool { return all[i].Less(all[j]) })
	return all
}

// Each calls fn for every response.
func (rs AlterAllReplicaLogDirsResponses) Each(fn func(AlterReplicaLogDirsResponse)) {
	for _, ts := range rs {
		ts.Each(fn)
	}
}

// AlterReplicaLogDirsResponses contains responses to altered partition
// directories for a single broker.
type AlterReplicaLogDirsResponses map[string]map[int32]AlterReplicaLogDirsResponse

// Sorted returns the responses sorted by topic and partition.
func (rs AlterReplicaLogDirsResponses) Sorted() []AlterReplicaLogDirsResponse {
	var all []AlterReplicaLogDirsResponse
	rs.Each(func(r AlterReplicaLogDirsResponse) {
		all = append(all, r)
	})
	sort.Slice(all, func(i, j int) bool { return all[i].Less(all[j]) })
	return all
}

// Each calls fn for every response.
func (rs AlterReplicaLogDirsResponses) Each(fn func(AlterReplicaLogDirsResponse)) {
	for _, ps := range rs {
		for _, r := range ps {
			fn(r)
		}
	}
}

// AlterReplicaLogDirsResponse contains a the response for an individual
// altered partition directory.
type AlterReplicaLogDirsResponse struct {
	Broker    int32  // Broker is the broker this response came from.
	Dir       string // Dir is the directory this partition was requested to be moved to.
	Topic     string // Topic is the topic for this partition.
	Partition int32  // Partition is the partition that was moved.
	Err       error  // Err is non-nil if this move had an error.
}

// Less returns if the response is less than the other by broker, dir, topic,
// and partition.
func (a AlterReplicaLogDirsResponse) Less(other AlterReplicaLogDirsResponse) bool {
	if a.Broker < other.Broker {
		return true
	}
	if a.Broker > other.Broker {
		return false
	}
	if a.Dir < other.Dir {
		return true
	}
	if a.Dir > other.Dir {
		return false
	}
	if a.Topic < other.Topic {
		return true
	}
	if a.Topic > other.Topic {
		return false
	}
	return a.Partition < other.Partition
}

func newAlterLogDirsResp(node int32, req AlterReplicaLogDirsReq, resp *kmsg.AlterReplicaLogDirsResponse) AlterReplicaLogDirsResponses {
	a := make(AlterReplicaLogDirsResponses)
	for _, kt := range resp.Topics {
		ps := make(map[int32]AlterReplicaLogDirsResponse)
		a[kt.Topic] = ps
		for _, kp := range kt.Partitions {
			ps[kp.Partition] = AlterReplicaLogDirsResponse{
				Broker:    node,
				Dir:       req.dirfor(kt.Topic, kp.Partition),
				Topic:     kt.Topic,
				Partition: kp.Partition,
				Err:       kerr.ErrorForCode(kp.ErrorCode),
			}
		}
	}
	return a
}

// AlterAllReplicaLogDirs alters the log directories for the input topic
// partitions, moving each partition to the requested directory. This function
// moves all replicas on any broker.
//
// This may return *ShardErrors.
func (cl *Client) AlterAllReplicaLogDirs(ctx context.Context, alter AlterReplicaLogDirsReq) (AlterAllReplicaLogDirsResponses, error) {
	if len(alter) == 0 {
		return make(AlterAllReplicaLogDirsResponses), nil
	}
	req := alter.req()
	shards := cl.cl.RequestSharded(ctx, req)
	resps := make(AlterAllReplicaLogDirsResponses)
	return resps, shardErrEachBroker(req, shards, func(b BrokerDetail, kr kmsg.Response) error {
		resp := kr.(*kmsg.AlterReplicaLogDirsResponse)
		resps[b.NodeID] = newAlterLogDirsResp(b.NodeID, alter, resp) // one node ID, no need to unique-check
		return nil
	})
}

// AlterBrokerReplicaLogDirs alters the log directories for the input topic on the
// given broker, moving each partition to the requested directory.
func (cl *Client) AlterBrokerReplicaLogDirs(ctx context.Context, broker int32, alter AlterReplicaLogDirsReq) (AlterReplicaLogDirsResponses, error) {
	if len(alter) == 0 {
		return make(AlterReplicaLogDirsResponses), nil
	}
	b := cl.cl.Broker(int(broker))
	kresp, err := b.RetriableRequest(ctx, alter.req())
	if err != nil {
		return nil, err
	}
	resp := kresp.(*kmsg.AlterReplicaLogDirsResponse)
	return newAlterLogDirsResp(broker, alter, resp), nil
}

func describeLogDirsReq(s TopicsSet) *kmsg.DescribeLogDirsRequest {
	req := kmsg.NewPtrDescribeLogDirsRequest()
	for t, ps := range s {
		rt := kmsg.NewDescribeLogDirsRequestTopic()
		rt.Topic = t
		for p := range ps {
			rt.Partitions = append(rt.Partitions, p)
		}
		req.Topics = append(req.Topics, rt)
	}
	return req
}

// DescribedAllLogDirs contains per-broker responses to described log
// directories.
type DescribedAllLogDirs map[int32]DescribedLogDirs

// Sorted returns each log directory sorted by broker, then by directory.
func (ds DescribedAllLogDirs) Sorted() []DescribedLogDir {
	var all []DescribedLogDir
	ds.Each(func(d DescribedLogDir) {
		all = append(all, d)
	})
	sort.Slice(all, func(i, j int) bool {
		l, r := all[i], all[j]
		return l.Broker < r.Broker || l.Broker == r.Broker && l.Dir < r.Dir
	})
	return all
}

// Each calls fn for every described log dir in all responses.
func (ds DescribedAllLogDirs) Each(fn func(DescribedLogDir)) {
	for _, bds := range ds {
		bds.Each(fn)
	}
}

// DescribedLogDirs contains per-directory responses to described log
// directories for a single broker.
type DescribedLogDirs map[string]DescribedLogDir

// Lookup returns the described partition if it exists.
func (ds DescribedLogDirs) Lookup(d, t string, p int32) (DescribedLogDirPartition, bool) {
	dir, exists := ds[d]
	if !exists {
		return DescribedLogDirPartition{}, false
	}
	ps, exists := dir.Topics[t]
	if !exists {
		return DescribedLogDirPartition{}, false
	}
	dp, exists := ps[p]
	if !exists {
		return DescribedLogDirPartition{}, false
	}
	return dp, true
}

// LookupPartition returns the described partition if it exists in any
// directory. Brokers should only have one replica of a partition, so this
// should always find at most one partition.
func (ds DescribedLogDirs) LookupPartition(t string, p int32) (DescribedLogDirPartition, bool) {
	for _, dir := range ds {
		ps, exists := dir.Topics[t]
		if !exists {
			continue
		}
		dp, exists := ps[p]
		if !exists {
			continue
		}
		return dp, true
	}
	return DescribedLogDirPartition{}, false
}

// Size returns the total size of all directories.
func (ds DescribedLogDirs) Size() int64 {
	var tot int64
	ds.EachPartition(func(d DescribedLogDirPartition) {
		tot += d.Size
	})
	return tot
}

// Error iterates over all directories and returns the first error encounted,
// if any. This can be used to check if describing was entirely successful or
// not.
func (ds DescribedLogDirs) Error() error {
	for _, d := range ds {
		if d.Err != nil {
			return d.Err
		}
	}
	return nil
}

// Ok returns true if there are no errors. This is a shortcut for ds.Error() ==
// nil.
func (ds DescribedLogDirs) Ok() bool {
	return ds.Error() == nil
}

// Sorted returns all directories sorted by dir.
func (ds DescribedLogDirs) Sorted() []DescribedLogDir {
	var all []DescribedLogDir
	ds.Each(func(d DescribedLogDir) {
		all = append(all, d)
	})
	sort.Slice(all, func(i, j int) bool {
		l, r := all[i], all[j]
		return l.Broker < r.Broker || l.Broker == r.Broker && l.Dir < r.Dir
	})
	return all
}

// SortedPartitions returns all partitions sorted by dir, then topic, then
// partition.
func (ds DescribedLogDirs) SortedPartitions() []DescribedLogDirPartition {
	var all []DescribedLogDirPartition
	ds.EachPartition(func(d DescribedLogDirPartition) {
		all = append(all, d)
	})
	sort.Slice(all, func(i, j int) bool { return all[i].Less(all[j]) })
	return all
}

// SortedBySize returns all directories sorted from smallest total directory
// size to largest.
func (ds DescribedLogDirs) SortedBySize() []DescribedLogDir {
	var all []DescribedLogDir
	ds.Each(func(d DescribedLogDir) {
		all = append(all, d)
	})
	sort.Slice(all, func(i, j int) bool {
		l, r := all[i], all[j]
		ls, rs := l.Size(), r.Size()
		return ls < rs || ls == rs &&
			(l.Broker < r.Broker || l.Broker == r.Broker &&
				l.Dir < r.Dir)
	})
	return all
}

// SortedPartitionsBySize returns all partitions across all directories sorted
// by smallest to largest, falling back to by broker, dir, topic, and
// partition.
func (ds DescribedLogDirs) SortedPartitionsBySize() []DescribedLogDirPartition {
	var all []DescribedLogDirPartition
	ds.EachPartition(func(d DescribedLogDirPartition) {
		all = append(all, d)
	})
	sort.Slice(all, func(i, j int) bool { return all[i].LessBySize(all[j]) })
	return all
}

// SmallestPartitionBySize returns the smallest partition by directory size, or
// no partition if there are no partitions.
func (ds DescribedLogDirs) SmallestPartitionBySize() (DescribedLogDirPartition, bool) {
	sorted := ds.SortedPartitionsBySize()
	if len(sorted) == 0 {
		return DescribedLogDirPartition{}, false
	}
	return sorted[0], true
}

// LargestPartitionBySize returns the largest partition by directory size, or
// no partition if there are no partitions.
func (ds DescribedLogDirs) LargestPartitionBySize() (DescribedLogDirPartition, bool) {
	sorted := ds.SortedPartitionsBySize()
	if len(sorted) == 0 {
		return DescribedLogDirPartition{}, false
	}
	return sorted[len(sorted)-1], true
}

// Each calls fn for each log directory.
func (ds DescribedLogDirs) Each(fn func(DescribedLogDir)) {
	for _, d := range ds {
		fn(d)
	}
}

// Each calls fn for each partition in any directory.
func (ds DescribedLogDirs) EachPartition(fn func(d DescribedLogDirPartition)) {
	for _, d := range ds {
		d.Topics.Each(fn)
	}
}

// EachError calls fn for every directory that has a non-nil error.
func (ds DescribedLogDirs) EachError(fn func(DescribedLogDir)) {
	for _, d := range ds {
		if d.Err != nil {
			fn(d)
		}
	}
}

// DescribedLogDir is a described log directory.
type DescribedLogDir struct {
	Broker int32                 // Broker is the broker being described.
	Dir    string                // Dir is the described directory.
	Topics DescribedLogDirTopics // Partitions are the partitions in this directory.
	Err    error                 // Err is non-nil if this directory could not be described.
}

// Size returns the total size of all partitions in this directory. This is
// a shortcut for .Topics.Size().
func (ds DescribedLogDir) Size() int64 {
	return ds.Topics.Size()
}

// DescribedLogDirTopics contains per-partition described log directories.
type DescribedLogDirTopics map[string]map[int32]DescribedLogDirPartition

// Lookup returns the described partition if it exists.
func (ds DescribedLogDirTopics) Lookup(t string, p int32) (DescribedLogDirPartition, bool) {
	ps, exists := ds[t]
	if !exists {
		return DescribedLogDirPartition{}, false
	}
	d, exists := ps[p]
	return d, exists
}

// Size returns the total size of all partitions in this directory.
func (ds DescribedLogDirTopics) Size() int64 {
	var tot int64
	ds.Each(func(d DescribedLogDirPartition) {
		tot += d.Size
	})
	return tot
}

// Sorted returns all partitions sorted by topic then partition.
func (ds DescribedLogDirTopics) Sorted() []DescribedLogDirPartition {
	var all []DescribedLogDirPartition
	ds.Each(func(d DescribedLogDirPartition) {
		all = append(all, d)
	})
	sort.Slice(all, func(i, j int) bool { return all[i].Less(all[j]) })
	return all
}

// SortedBySize returns all partitions sorted by smallest size to largest. If
// partitions are of equal size, the sorting is topic then partition.
func (ds DescribedLogDirTopics) SortedBySize() []DescribedLogDirPartition {
	var all []DescribedLogDirPartition
	ds.Each(func(d DescribedLogDirPartition) {
		all = append(all, d)
	})
	sort.Slice(all, func(i, j int) bool { return all[i].LessBySize(all[j]) })
	return all
}

// Each calls fn for every partition.
func (ds DescribedLogDirTopics) Each(fn func(p DescribedLogDirPartition)) {
	for _, ps := range ds {
		for _, d := range ps {
			fn(d)
		}
	}
}

// DescribedLogDirPartition is the information for a single partitions described
// log directory.
type DescribedLogDirPartition struct {
	Broker    int32  // Broker is the broker this partition is on.
	Dir       string // Dir is the directory this partition lives in.
	Topic     string // Topic is the topic for this partition.
	Partition int32  // Partition is this partition.
	Size      int64  // Size is the total size of the log segments of this partition, in bytes.

	// OffsetLag is how far behind the log end offset this partition is.
	// The math is:
	//
	//     if IsFuture {
	//         logEndOffset - futureLogEndOffset
	//     } else {
	//         max(highWaterMark - logEndOffset)
	//     }
	//
	OffsetLag int64
	// IsFuture is true if this replica was created by an
	// AlterReplicaLogDirsRequest and will replace the current log of the
	// replica in the future.
	IsFuture bool
}

// Less returns if one dir partition is less than the other, by dir, topic,
// partition, and size.
func (p DescribedLogDirPartition) Less(other DescribedLogDirPartition) bool {
	if p.Broker < other.Broker {
		return true
	}
	if p.Broker > other.Broker {
		return false
	}
	if p.Dir < other.Dir {
		return true
	}
	if p.Dir > other.Dir {
		return false
	}
	if p.Topic < other.Topic {
		return true
	}
	if p.Topic > other.Topic {
		return false
	}
	if p.Partition < other.Partition {
		return true
	}
	if p.Partition > other.Partition {
		return false
	}
	return p.Size < other.Size
}

// LessBySize returns if one dir partition is less than the other by size,
// otherwise by normal Less semantics.
func (p DescribedLogDirPartition) LessBySize(other DescribedLogDirPartition) bool {
	if p.Size < other.Size {
		return true
	}
	return p.Less(other)
}

func newDescribeLogDirsResp(node int32, resp *kmsg.DescribeLogDirsResponse) DescribedLogDirs {
	ds := make(DescribedLogDirs)
	for _, rd := range resp.Dirs {
		d := DescribedLogDir{
			Broker: node,
			Dir:    rd.Dir,
			Topics: make(DescribedLogDirTopics),
			Err:    kerr.ErrorForCode(rd.ErrorCode),
		}
		for _, rt := range rd.Topics {
			t := make(map[int32]DescribedLogDirPartition)
			d.Topics[rt.Topic] = t
			for _, rp := range rt.Partitions {
				t[rp.Partition] = DescribedLogDirPartition{
					Broker:    node,
					Dir:       rd.Dir,
					Topic:     rt.Topic,
					Partition: rp.Partition,
					Size:      rp.Size,
					OffsetLag: rp.OffsetLag,
					IsFuture:  rp.IsFuture,
				}
			}
		}
		ds[rd.Dir] = d
	}
	return ds
}

// DescribeAllLogDirs describes the log directores for every input topic
// partition on every broker. If the input set is nil, this describes all log
// directories.
//
// This may return *ShardErrors.
func (cl *Client) DescribeAllLogDirs(ctx context.Context, s TopicsSet) (DescribedAllLogDirs, error) {
	req := describeLogDirsReq(s)
	shards := cl.cl.RequestSharded(ctx, req)
	resps := make(DescribedAllLogDirs)
	return resps, shardErrEachBroker(req, shards, func(b BrokerDetail, kr kmsg.Response) error {
		resp := kr.(*kmsg.DescribeLogDirsResponse)
		if err := maybeAuthErr(resp.ErrorCode); err != nil {
			return err
		}
		if err := kerr.ErrorForCode(resp.ErrorCode); err != nil {
			return err
		}
		resps[b.NodeID] = newDescribeLogDirsResp(b.NodeID, resp) // one node ID, no need to unique-check
		return nil
	})
}

// DescribeBrokerLogDirs describes the log directories for the input topic
// partitions on the given broker. If the input set is nil, this describes all
// log directories.
func (cl *Client) DescribeBrokerLogDirs(ctx context.Context, broker int32, s TopicsSet) (DescribedLogDirs, error) {
	req := describeLogDirsReq(s)
	b := cl.cl.Broker(int(broker))
	kresp, err := b.RetriableRequest(ctx, req)
	if err != nil {
		return nil, err
	}
	resp := kresp.(*kmsg.DescribeLogDirsResponse)
	if err := maybeAuthErr(resp.ErrorCode); err != nil {
		return nil, err
	}
	if err := kerr.ErrorForCode(resp.ErrorCode); err != nil {
		return nil, err
	}
	return newDescribeLogDirsResp(broker, resp), nil
}
