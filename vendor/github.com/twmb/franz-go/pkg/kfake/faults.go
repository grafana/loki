package kfake

import (
	"context"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// We check faults wherever we check authorization, and per partition in
// every request that carries partitions.

// Fault fails matching requests with Err in place of the real answer, with
// every field being an AND filter of what the fault should apply to. Faults
// are an easier way to inject targeted failures than Control.
//
// Selectors choose what a fault applies to. kfake checks faults while
// handling a request, once for each thing the request names.
//
//   - Topic and TopicID both work whether a request carries topic names
//     or topic IDs.
//   - A fault fires only on the broker that would handle the request:
//     the coordinator, the controller, or the partition leader (unless
//     you set Nodes, then it always fires on those specific brokers).
//   - A fault fires only after the request passes ACL checks. It can
//     fail a topic or group that does not exist.
//   - Requests that list everything, like Metadata for all topics or
//     ListGroups, leave a faulted entry out of the response, the same way
//     they leave out entries you are not authorized to see.
//   - Fetch and ShareFetch also check the partitions in the fetch session
//     that the request does not name.
//
// A selector that a request does not carry never matches: a fault with a
// Topic does not fail a heartbeat.
//
// TopLevel sets the request's top-level ErrorCode instead, before the
// request is handled. It never fires on a request version whose response
// has no top-level ErrorCode.
//
// A faulted entity is rejected before the broker acts on it, except with
// REQUEST_TIMED_OUT, on the requests a real broker can answer that way after
// applying them, or NOT_ENOUGH_REPLICAS_AFTER_APPEND on produce: those are
// answered and the work still happens. Faults are checked in the order added
// and the first match answers; a ControlKey control that answers a request
// bypasses them.
type Fault struct {
	Keys  []kmsg.Key // which requests this should apply to; nil means all
	Nodes []int32    // which nodes this should apply to; nil means all; see above

	Topic      string   // "" means all
	TopicID    [16]byte // zero means all
	Partitions []int32  // which partitions this should apply to; nil means all
	Group      string   // "" means all; a FindCoordinator group key matches this
	TxnID      string   // "" means all; a FindCoordinator transaction key matches this
	TopLevel   bool     // fail the request's top-level ErrorCode rather than its entities; see above

	// Resource names an entity no selector above reaches: a config
	// resource, a client quota entity, a SCRAM user, a log dir, a feature,
	// a member in a LeaveGroup, or the resource name of an ACL creation or
	// filter. "" means all.
	Resource string

	Err   *kerr.Error // nil defaults to UNKNOWN_SERVER_ERROR
	Count int         // requests to fault; 0 means one, -1 means until Remove is called

	// When further filters by the request itself, for what the selectors
	// cannot reach: a field inside the request body, such as the member
	// epoch on a heartbeat or the topics a heartbeat subscribes to. nil
	// means every request the selectors match. Returning false means the
	// request is not faulted.
	//
	// When can be called several times for one request, once per entity
	// checked. You can call Cluster methods inside When, minus Wait
	// methods that wait for the cluster itself to do things.
	When func(kmsg.Request) bool

	// Observe counts matching requests rather than faulting them: the
	// request proceeds as it would have while the fault is counted for
	// Hits and Wait.
	//
	// Note that a control that answers a request bypasses faults, so an
	// observing fault does not see it, nor does an observing fault
	// without TopLevel see a request a TopLevel fault answers. An
	// observing fault also counts a request that reached the wrong
	// broker.
	Observe bool
}

// FaultHandle refers to the faults installed by one Fault call.
type FaultHandle struct {
	c  *Cluster
	fs []*fault
}

type fault struct {
	keys       []kmsg.Key
	nodes      []int32
	topic      string
	topicID    uuid
	partitions []int32
	group      string
	txnID      string
	resource   string
	topLevel   bool
	when       func(kmsg.Request) bool
	observe    bool
	err        *kerr.Error
	hits       atomic.Int64

	// left is the requests remaining, -1 for unlimited, and removed is
	// set once we drop the fault. Both are guarded by c.faultsMu.
	left    int
	removed bool
}

// Fault installs faults and returns a handle to them. Fault may be called
// from a control function.
func (c *Cluster) Fault(faults ...Fault) *FaultHandle {
	h := &FaultHandle{c: c}
	c.faultsMu.Lock()
	defer c.faultsMu.Unlock()
	if c.faultCond == nil {
		c.faultCond = sync.NewCond(&c.faultsMu)
	}
	for _, in := range faults {
		if in.TopLevel && (in.Topic != "" || in.TopicID != noID || in.Partitions != nil || in.Resource != "") {
			panic("kfake: a TopLevel fault can select only on Group and TxnID")
		}
		f := &fault{
			keys:       slices.Clone(in.Keys),
			nodes:      slices.Clone(in.Nodes),
			topic:      in.Topic,
			topicID:    in.TopicID,
			partitions: slices.Clone(in.Partitions),
			group:      in.Group,
			txnID:      in.TxnID,
			resource:   in.Resource,
			topLevel:   in.TopLevel,
			when:       in.When,
			observe:    in.Observe,
			err:        in.Err,
			left:       in.Count,
		}
		if f.err == nil {
			f.err = kerr.UnknownServerError
		}
		switch {
		case in.Count == 0:
			f.left = 1
		case in.Count < 0:
			f.left = -1
		}
		h.fs = append(h.fs, f)
		c.faults = append(c.faults, f)
	}
	return h
}

// Wait blocks until the faults have answered at least n requests in total,
// or until ctx is done, returning ctx's error.
func (h *FaultHandle) Wait(ctx context.Context, n int) error {
	quit := false
	done := make(chan struct{})
	go func() {
		h.c.faultsMu.Lock()
		defer h.c.faultsMu.Unlock()
		defer close(done)

		for !quit && h.Hits() < n {
			h.c.faultCond.Wait()
		}
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		h.c.faultsMu.Lock()
		quit = true
		h.c.faultsMu.Unlock()
		h.c.faultCond.Broadcast()
		return ctx.Err()
	}
}

// Hits is the count of requests the faults have answered so far.
func (h *FaultHandle) Hits() int {
	var n int64
	for _, f := range h.fs {
		n += f.hits.Load()
	}
	return int(n)
}

// Remove uninstalls faults associated with this handle.
func (h *FaultHandle) Remove() {
	h.c.faultsMu.Lock()
	defer h.c.faultsMu.Unlock()
	for _, f := range h.fs {
		h.c.rmFaultLocked(f)
	}
}

func (c *Cluster) rmFaultLocked(f *fault) {
	if f.removed {
		return
	}
	f.removed = true
	c.faults = slices.DeleteFunc(c.faults, func(o *fault) bool { return o == f })
}

// faultKey is what a site knows about the entity it is answering for. A fault
// that selects on something the key does not name never matches there.
type faultKey struct {
	topic     string
	topicID   uuid
	partition int32
	hasPart   bool
	group     string
	txnID     string
	resource  string

	// misrouted is set when this broker would not handle the entity:
	// it is not the coordinator, controller, or partition leader. We
	// answer that ourselves (NOT_COORDINATOR, ...), so faults that
	// answer skip it unless they name this node; observing faults
	// still count it.
	misrouted bool
}

// part returns k naming a partition.
func (k faultKey) part(p int32) faultKey {
	k.partition, k.hasPart = p, true
	return k
}

func (f *fault) matches(k faultKey) bool {
	if f.topic != "" && f.topic != k.topic {
		return false
	}
	if f.topicID != noID && f.topicID != k.topicID {
		return false
	}
	if len(f.partitions) > 0 && (!k.hasPart || !slices.Contains(f.partitions, k.partition)) {
		return false
	}
	if f.group != "" && f.group != k.group {
		return false
	}
	if f.txnID != "" && f.txnID != k.txnID {
		return false
	}
	if f.resource != "" && f.resource != k.resource {
		return false
	}
	return true
}

// faultCheck is the faults that can match one request; hits records the ones
// this request has already hit.
type faultCheck struct {
	c    *Cluster
	key  int16
	kreq kmsg.Request
	fs   []*fault
	hits []*fault
}

// faultsFor returns the faults to consider for creq, or nil if none can match.
func (c *Cluster) faultsFor(creq *clientReq) *faultCheck {
	c.faultsMu.Lock()
	defer c.faultsMu.Unlock()
	if len(c.faults) == 0 {
		return nil
	}
	key := creq.kreq.Key()
	kkey := kmsg.Key(key)
	var fs []*fault
	for _, f := range c.faults {
		if len(f.nodes) > 0 && !slices.Contains(f.nodes, creq.cc.b.node) {
			continue
		}
		if len(f.keys) > 0 && !slices.Contains(f.keys, kkey) {
			continue
		}
		fs = append(fs, f)
	}
	if len(fs) == 0 {
		return nil
	}
	return &faultCheck{c: c, key: key, kreq: creq.kreq, fs: fs}
}

// check returns the error to answer the entity named by k with, if a fault
// matches it.
func (fc *faultCheck) check(k faultKey) *kerr.Error {
	if fc == nil {
		return nil
	}
	fc.c.resolveTopic(&k)
	for _, f := range fc.fs {
		// A TopLevel fault answers only in topLevel. A misrouted
		// entity is seen only by an observing fault or one that names
		// this node.
		if f.topLevel || k.misrouted && !f.observe && len(f.nodes) == 0 || !f.matches(k) || !fc.when(f) {
			continue
		}
		if !fc.hit(f) {
			continue
		}
		if f.observe {
			continue // counted, but the entity is not faulted
		}
		return f.err
	}
	return nil
}

// resolveTopic fills in whichever of the topic name and ID the site did not
// know, so Topic and TopicID both match whatever the request carries.
func (c *Cluster) resolveTopic(k *faultKey) {
	switch {
	case k.topic != "" && k.topicID == noID:
		k.topicID = c.data.t2id[k.topic]
	case k.topic == "" && k.topicID != noID:
		k.topic = c.data.id2t[k.topicID]
	}
}

// when reports whether f's When accepts this request. A fault with no When
// accepts every request its selectors matched. While When runs, admin
// serves Cluster methods inline; see inWhen.
func (fc *faultCheck) when(f *fault) bool {
	if f.when == nil {
		return true
	}
	c := fc.c
	c.whenMu.Lock()
	c.inWhen = true
	c.whenMu.Unlock()
	defer func() {
		c.whenMu.Lock()
		c.inWhen = false
		c.whenMu.Unlock()
	}()
	return f.when(fc.kreq)
}

// afterApply is the requests a broker can answer REQUEST_TIMED_OUT after
// applying them, and the codes it answers with.
var afterApply = map[int16][]*kerr.Error{
	int16(kmsg.Produce):                   {kerr.RequestTimedOut, kerr.NotEnoughReplicasAfterAppend},
	int16(kmsg.WriteTxnMarkers):           {kerr.RequestTimedOut, kerr.NotEnoughReplicasAfterAppend},
	int16(kmsg.DeleteRecords):             {kerr.RequestTimedOut},
	int16(kmsg.OffsetCommit):              {kerr.RequestTimedOut},
	int16(kmsg.TxnOffsetCommit):           {kerr.RequestTimedOut},
	int16(kmsg.DeleteGroups):              {kerr.RequestTimedOut},
	int16(kmsg.AlterShareGroupOffsets):    {kerr.RequestTimedOut},
	int16(kmsg.DeleteShareGroupOffsets):   {kerr.RequestTimedOut},
	int16(kmsg.CreateTopics):              {kerr.RequestTimedOut},
	int16(kmsg.DeleteTopics):              {kerr.RequestTimedOut},
	int16(kmsg.CreatePartitions):          {kerr.RequestTimedOut},
	int16(kmsg.ElectLeaders):              {kerr.RequestTimedOut},
	int16(kmsg.AlterPartitionAssignments): {kerr.RequestTimedOut},
	int16(kmsg.AlterConfigs):              {kerr.RequestTimedOut},
	int16(kmsg.IncrementalAlterConfigs):   {kerr.RequestTimedOut},
	int16(kmsg.CreateACLs):                {kerr.RequestTimedOut},
	int16(kmsg.DeleteACLs):                {kerr.RequestTimedOut},
	int16(kmsg.AlterClientQuotas):         {kerr.RequestTimedOut},
	int16(kmsg.AlterUserSCRAMCredentials): {kerr.RequestTimedOut},
	int16(kmsg.UpdateFeatures):            {kerr.RequestTimedOut},
}

// skipsWork reports whether answering e skips the entity's work; the
// afterApply codes do not.
func (creq *clientReq) skipsWork(e *kerr.Error) bool {
	return !slices.Contains(afterApply[creq.kreq.Key()], e)
}

// entityless is the requests that carry nothing a fault can select on.
var entityless = map[int16]bool{
	int16(kmsg.ApiVersions):               true,
	int16(kmsg.SASLHandshake):             true,
	int16(kmsg.SASLAuthenticate):          true,
	int16(kmsg.GetTelemetrySubscriptions): true,
	int16(kmsg.PushTelemetry):             true,
	int16(kmsg.DescribeCluster):           true,
	int16(kmsg.ListGroups):                true,
	int16(kmsg.ListTransactions):          true,
}

// topLevel answers a request before its handler runs: a TopLevel fault sets
// the top-level error code of any request that has one, and on a request with
// nothing to select on (entityless) any fault with no entity selectors does
// the same.
func (fc *faultCheck) topLevel(kreq kmsg.Request) kmsg.Response {
	if fc == nil {
		return nil
	}
	for _, f := range fc.fs {
		answers := f.topLevel && f.matches(requestKey(kreq)) || entityless[fc.key] && f.matches(faultKey{})
		if !answers || !fc.when(f) {
			continue
		}
		resp, code, ok := topLevelCode(kreq)
		if !ok {
			continue // this request version has no top-level code
		}
		if !fc.hit(f) {
			continue
		}
		if f.observe {
			continue // counted, but the request is not answered
		}
		code.SetInt(int64(f.err.Code))
		return resp
	}
	return nil
}

// topLevelCode returns a response for kreq and its top-level ErrorCode field,
// if the response has one at kreq's version. Many responses gained their
// ErrorCode in a later version; we tell by encoding a code and decoding it
// back, since a code the version does not serialize never reaches the client.
func topLevelCode(kreq kmsg.Request) (kmsg.Response, reflect.Value, bool) {
	resp := kreq.ResponseKind()
	code, ok := errorCodeField(resp)
	if !ok {
		return nil, reflect.Value{}, false
	}
	code.SetInt(-1)
	back := kreq.ResponseKind()
	if err := back.ReadFrom(resp.AppendTo(nil)); err != nil {
		return nil, reflect.Value{}, false
	}
	code.SetInt(0)
	if backCode, _ := errorCodeField(back); backCode.Int() != -1 {
		return nil, reflect.Value{}, false
	}
	return resp, code, true
}

func errorCodeField(kresp kmsg.Response) (reflect.Value, bool) {
	v := reflect.ValueOf(kresp)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return reflect.Value{}, false
	}
	code := v.Elem().FieldByName("ErrorCode")
	if !code.IsValid() || code.Kind() != reflect.Int16 {
		return reflect.Value{}, false
	}
	return code, true
}

// requestKey names the group and transactional ID a request carries at its
// top level, for TopLevel faults.
func requestKey(kreq kmsg.Request) faultKey {
	var k faultKey
	v := reflect.ValueOf(kreq)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return k
	}
	str := func(name string) string {
		f := v.Elem().FieldByName(name)
		switch {
		case !f.IsValid():
		case f.Kind() == reflect.String:
			return f.String()
		case f.Kind() == reflect.Pointer && !f.IsNil() && f.Elem().Kind() == reflect.String:
			return f.Elem().String()
		}
		return ""
	}
	k.group = str("Group")
	k.txnID = str("TransactionalID")
	return k
}

// hit takes a unit of f's budget for this request. A request with several
// matching entities hits the fault once: the first entity takes the unit,
// later ones are still answered. A fault with nothing left removes itself.
func (fc *faultCheck) hit(f *fault) bool {
	if slices.Contains(fc.hits, f) {
		return true
	}
	fc.c.faultsMu.Lock()
	defer fc.c.faultsMu.Unlock()
	if f.removed || f.left == 0 {
		return false
	}
	if f.left > 0 {
		f.left--
		if f.left == 0 {
			fc.c.rmFaultLocked(f)
		}
	}
	f.hits.Add(1)
	fc.hits = append(fc.hits, f)
	fc.c.faultCond.Broadcast()
	return true
}

// anyHit reports whether anything in this request was faulted. An observing
// fault takes a unit like any other, but it answers nothing, so a site that
// changes what it does once something was faulted must not see it.
func (fc *faultCheck) anyHit() bool {
	return fc != nil && slices.ContainsFunc(fc.hits, func(f *fault) bool { return !f.observe })
}

// deny answers the error to fail the entity named by k with, either because
// the user is not allowed the operation or because a fault matches.
func (c *Cluster) deny(creq *clientReq, resource string, rt kmsg.ACLResourceType, op kmsg.ACLOperation, k faultKey) *kerr.Error {
	if !c.allowedACL(creq, resource, rt, op) {
		return aclDenied(rt)
	}
	return creq.faults.check(k)
}

// denyCluster is deny for an operation on the cluster itself.
func (c *Cluster) denyCluster(creq *clientReq, op kmsg.ACLOperation, k faultKey) *kerr.Error {
	if !c.allowedClusterACL(creq, op) {
		return kerr.ClusterAuthorizationFailed
	}
	return creq.faults.check(k)
}

func aclDenied(rt kmsg.ACLResourceType) *kerr.Error {
	switch rt {
	case kmsg.ACLResourceTypeTopic:
		return kerr.TopicAuthorizationFailed
	case kmsg.ACLResourceTypeGroup:
		return kerr.GroupAuthorizationFailed
	case kmsg.ACLResourceTypeTransactionalId:
		return kerr.TransactionalIDAuthorizationFailed
	default:
		return kerr.ClusterAuthorizationFailed
	}
}
