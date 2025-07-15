package kadm

import (
	"context"
	"sort"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// AlterPartitionAssignmentsReq is the input for a request to alter partition
// assignments. The keys are topics and partitions, and the final slice
// corresponds to brokers that replicas will be assigneed to. If the brokers
// for a given partition are null, the request will *cancel* any active
// reassignment for that partition.
type AlterPartitionAssignmentsReq map[string]map[int32][]int32

// Assign specifies brokers that a partition should be placed on. Using null
// for the brokers cancels a pending reassignment of the parititon.
func (r *AlterPartitionAssignmentsReq) Assign(t string, p int32, brokers []int32) {
	if *r == nil {
		*r = make(map[string]map[int32][]int32)
	}
	ps := (*r)[t]
	if ps == nil {
		ps = make(map[int32][]int32)
		(*r)[t] = ps
	}
	ps[p] = brokers
}

// CancelAssign cancels a reassignment of the given partition.
func (r *AlterPartitionAssignmentsReq) CancelAssign(t string, p int32) {
	r.Assign(t, p, nil)
}

// AlterPartitionAssignmentsResponse contains a response for an individual
// partition that was assigned.
type AlterPartitionAssignmentsResponse struct {
	Topic      string // Topic is the topic that was assigned.
	Partition  int32  // Partition is the partition that was assigned.
	Err        error  // Err is non-nil if this assignment errored.
	ErrMessage string // ErrMessage is an optional additional message on error.
}

// AlterPartitionAssignmentsResponses contains responses to all partitions in an
// alter assignment request.
type AlterPartitionAssignmentsResponses map[string]map[int32]AlterPartitionAssignmentsResponse

// Sorted returns the responses sorted by topic and partition.
func (rs AlterPartitionAssignmentsResponses) Sorted() []AlterPartitionAssignmentsResponse {
	var all []AlterPartitionAssignmentsResponse
	rs.Each(func(r AlterPartitionAssignmentsResponse) {
		all = append(all, r)
	})
	sort.Slice(all, func(i, j int) bool {
		l, r := all[i], all[j]
		return l.Topic < r.Topic || l.Topic == r.Topic && l.Partition < r.Partition
	})
	return all
}

// Each calls fn for every response.
func (rs AlterPartitionAssignmentsResponses) Each(fn func(AlterPartitionAssignmentsResponse)) {
	for _, ps := range rs {
		for _, r := range ps {
			fn(r)
		}
	}
}

// Error returns the first error in the responses, if any.
func (rs AlterPartitionAssignmentsResponses) Error() error {
	for _, ps := range rs {
		for _, r := range ps {
			if r.Err != nil {
				return r.Err
			}
		}
	}
	return nil
}

// AlterPartitionAssignments alters partition assignments for the requested
// partitions, returning an error if the response could not be issued or if
// you do not have permissions.
func (cl *Client) AlterPartitionAssignments(ctx context.Context, req AlterPartitionAssignmentsReq) (AlterPartitionAssignmentsResponses, error) {
	if len(req) == 0 {
		return make(AlterPartitionAssignmentsResponses), nil
	}

	kreq := kmsg.NewPtrAlterPartitionAssignmentsRequest()
	kreq.TimeoutMillis = cl.timeoutMillis
	for t, ps := range req {
		rt := kmsg.NewAlterPartitionAssignmentsRequestTopic()
		rt.Topic = t
		for p, bs := range ps {
			rp := kmsg.NewAlterPartitionAssignmentsRequestTopicPartition()
			rp.Partition = p
			rp.Replicas = bs
			rt.Partitions = append(rt.Partitions, rp)
		}
		kreq.Topics = append(kreq.Topics, rt)
	}

	kresp, err := kreq.RequestWith(ctx, cl.cl)
	if err != nil {
		return nil, err
	}
	if err = kerr.ErrorForCode(kresp.ErrorCode); err != nil {
		return nil, &ErrAndMessage{err, unptrStr(kresp.ErrorMessage)}
	}

	a := make(AlterPartitionAssignmentsResponses)
	for _, kt := range kresp.Topics {
		ps := make(map[int32]AlterPartitionAssignmentsResponse)
		a[kt.Topic] = ps
		for _, kp := range kt.Partitions {
			ps[kp.Partition] = AlterPartitionAssignmentsResponse{
				Topic:      kt.Topic,
				Partition:  kp.Partition,
				Err:        kerr.ErrorForCode(kp.ErrorCode),
				ErrMessage: unptrStr(kp.ErrorMessage),
			}
		}
	}
	return a, nil
}

// ListPartitionReassignmentsResponse contains a response for an individual
// partition that was listed.
type ListPartitionReassignmentsResponse struct {
	Topic            string  // Topic is the topic that was listed.
	Partition        int32   // Partition is the partition that was listed.
	Replicas         []int32 // Replicas are the partition's current replicas.
	AddingReplicas   []int32 // AddingReplicas are replicas currently being added to the partition.
	RemovingReplicas []int32 // RemovingReplicas are replicas currently being removed from the partition.
}

// ListPartitionReassignmentsResponses contains responses to all partitions in
// a list reassignment request.
type ListPartitionReassignmentsResponses map[string]map[int32]ListPartitionReassignmentsResponse

// Sorted returns the responses sorted by topic and partition.
func (rs ListPartitionReassignmentsResponses) Sorted() []ListPartitionReassignmentsResponse {
	var all []ListPartitionReassignmentsResponse
	rs.Each(func(r ListPartitionReassignmentsResponse) {
		all = append(all, r)
	})
	sort.Slice(all, func(i, j int) bool {
		l, r := all[i], all[j]
		return l.Topic < r.Topic || l.Topic == r.Topic && l.Partition < r.Partition
	})
	return all
}

// Each calls fn for every response.
func (rs ListPartitionReassignmentsResponses) Each(fn func(ListPartitionReassignmentsResponse)) {
	for _, ps := range rs {
		for _, r := range ps {
			fn(r)
		}
	}
}

// ListPartitionReassignments lists the state of any active reassignments for
// all requested partitions, returning an error if the response could not be
// issued or if you do not have permissions.
func (cl *Client) ListPartitionReassignments(ctx context.Context, s TopicsSet) (ListPartitionReassignmentsResponses, error) {
	if len(s) == 0 {
		return make(ListPartitionReassignmentsResponses), nil
	}

	kreq := kmsg.NewPtrListPartitionReassignmentsRequest()
	kreq.TimeoutMillis = cl.timeoutMillis
	for t, ps := range s {
		rt := kmsg.NewListPartitionReassignmentsRequestTopic()
		rt.Topic = t
		for p := range ps {
			rt.Partitions = append(rt.Partitions, p)
		}
		kreq.Topics = append(kreq.Topics, rt)
	}

	kresp, err := kreq.RequestWith(ctx, cl.cl)
	if err != nil {
		return nil, err
	}
	if err = kerr.ErrorForCode(kresp.ErrorCode); err != nil {
		return nil, &ErrAndMessage{err, unptrStr(kresp.ErrorMessage)}
	}

	a := make(ListPartitionReassignmentsResponses)
	for _, kt := range kresp.Topics {
		ps := make(map[int32]ListPartitionReassignmentsResponse)
		a[kt.Topic] = ps
		for _, kp := range kt.Partitions {
			ps[kp.Partition] = ListPartitionReassignmentsResponse{
				Topic:            kt.Topic,
				Partition:        kp.Partition,
				Replicas:         kp.Replicas,
				AddingReplicas:   kp.AddingReplicas,
				RemovingReplicas: kp.RemovingReplicas,
			}
		}
	}
	return a, nil
}
