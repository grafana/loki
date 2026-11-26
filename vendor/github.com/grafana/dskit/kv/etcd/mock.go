package etcd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/go-kit/log"
	"go.etcd.io/etcd/api/v3/etcdserverpb"
	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
	"google.golang.org/protobuf/proto"

	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/kv/codec"
)

// channelBufferSize is the size of the channels used to send events from Put, Delete,
// and transactions as well as the channel used to send filtered events to watchers.
const channelBufferSize = 10

// NewInMemoryClient creates an Etcd Client implementation that uses an in-memory
// version of the underlying Etcd client.
func NewInMemoryClient(codec codec.Codec, logger log.Logger) (*Client, io.Closer) {
	// Make sure to set default values for the config including number of retries,
	// otherwise the client won't even attempt a CAS operation
	cfg := Config{}
	flagext.DefaultValues(&cfg)

	kv := newMockKV()
	client := &Client{
		cfg:    cfg,
		codec:  codec,
		cli:    kv,
		logger: logger,
	}

	return client, kv
}

// newMockKV creates an in-memory implementation of an etcd client
func newMockKV() *mockKV {
	kv := &mockKV{
		values:    make(map[string]*mvccpb.KeyValue),
		valuesMtx: sync.Mutex{},
		close:     make(chan struct{}),
		events:    make(map[chan *clientv3.Event]struct{}),
		eventsMtx: sync.Mutex{},
	}

	return kv
}

// mockKV is an in-memory implementation of an Etcd client.
//
// This implementation has many limitations compared to the real client since it
// only exists to be used by the Etcd kv.Client implementation during unit tests. As
// such some behavior may be missing or incorrect compared to the real client. This
// is determined to be an acceptable tradeoff to avoid needing to depend on an entire
// Etcd server for unit tests.
//
// Known limitations:
//
//   - Compact is not implemented and will panic
//   - RequestProgress is not implemented and will panic
//   - Only exact and prefix matching is supported for Get, Put, and Delete
//   - There may be inconsistencies with how various version numbers are adjusted
//     but none that are exposed by kv.Client unit tests
type mockKV struct {
	// Key-value pairs owned by the mock. Clone values before exposing them.
	values    map[string]*mvccpb.KeyValue
	valuesMtx sync.Mutex

	// Channel for stopping all running watch goroutines and closing
	// and cleaning up all channels used for sending events to watchers
	close chan struct{}

	// Channels that should receive events in response to Put or Delete
	// calls. These channels are in turn read by goroutines that apply
	// filtering before sending watch responses to their callers.
	events    map[chan *clientv3.Event]struct{}
	eventsMtx sync.Mutex
}

var _ Clientv3Facade = (*mockKV)(nil)

// Watch implements the Clientv3Facade interface
func (m *mockKV) Watch(ctx context.Context, key string, opts ...clientv3.OpOption) clientv3.WatchChan {
	watcher := make(chan clientv3.WatchResponse, channelBufferSize)
	consumer := m.createEventConsumer(channelBufferSize)

	go func() {
		defer func() {
			// When this goroutine ends, remove and close the channel written to by the
			// Put and Delete methods as well as closing the channel read by the caller
			// of the Watch method
			m.destroyEventConsumer(consumer)

			// non-blocking send
			select {
			case watcher <- clientv3.WatchResponse{Canceled: true}:
			default:
			}

			close(watcher)
		}()

		for {
			select {
			case <-ctx.Done():
				// Context cancelled for this watcher, run cleanup logic and exit
				return
			case <-m.close:
				// Close method called for all watchers, run cleanup logic and exit
				return
			case e := <-consumer:
				op := clientv3.OpGet(key, opts...)
				match := m.isMatch(op, e.Kv)

				if match {
					// non-blocking send
					select {
					case watcher <- clientv3.WatchResponse{Events: []*clientv3.Event{e}}:
					default:
					}
				}
			}
		}
	}()

	return watcher
}

// createEventConsumer creates and returns a new channel that is registered to receive
// events for Puts and Deletes.
func (m *mockKV) createEventConsumer(bufSz int) chan *clientv3.Event {
	ch := make(chan *clientv3.Event, bufSz)
	m.eventsMtx.Lock()
	m.events[ch] = struct{}{}
	m.eventsMtx.Unlock()
	return ch
}

// destroyEventConsumer removes the given channel from the list of channels that events
// should be sent to and closes it.
func (m *mockKV) destroyEventConsumer(ch chan *clientv3.Event) {
	m.eventsMtx.Lock()
	delete(m.events, ch)
	m.eventsMtx.Unlock()
	close(ch)
}

// sendEvent writes an event to all currently registered events. The consumer
// channels are each read by a goroutine that filters the event and sends it to
// the caller of the Watch method.
func (m *mockKV) sendEvent(e *clientv3.Event) {
	m.eventsMtx.Lock()
	for ch := range m.events {
		// Give each watcher its own protobuf message to prevent shared mutations.
		event := proto.Clone(e).(*clientv3.Event)

		// non-blocking send
		select {
		case ch <- event:
		default:
		}
	}
	m.eventsMtx.Unlock()
}

// RequestProgress implements the Clientv3Facade interface
func (m *mockKV) RequestProgress(context.Context) error {
	panic("RequestProgress unimplemented")
}

// Close implements the Clientv3Facade interface
func (m *mockKV) Close() error {
	close(m.close)
	return nil
}

// Get implements the Clientv3Facade interface
func (m *mockKV) Get(ctx context.Context, key string, opts ...clientv3.OpOption) (*clientv3.GetResponse, error) {
	op := clientv3.OpGet(key, opts...)
	res, err := m.Do(ctx, op)

	if err != nil {
		return nil, err
	}

	return res.Get(), nil
}

// Delete implements the Clientv3Facade interface
func (m *mockKV) Delete(ctx context.Context, key string, opts ...clientv3.OpOption) (*clientv3.DeleteResponse, error) {
	op := clientv3.OpDelete(key, opts...)
	res, err := m.Do(ctx, op)

	if err != nil {
		return nil, err
	}

	return res.Del(), nil
}

// Put implements the Clientv3Facade interface
func (m *mockKV) Put(ctx context.Context, key, val string, opts ...clientv3.OpOption) (*clientv3.PutResponse, error) {
	op := clientv3.OpPut(key, val, opts...)
	res, err := m.Do(ctx, op)

	if err != nil {
		return nil, err
	}

	return res.Put(), nil
}

// Txn implements the Clientv3Facade interface
func (m *mockKV) Txn(ctx context.Context) clientv3.Txn {
	return &mockTxn{
		ctx: ctx,
		kv:  m,
	}
}

// Compact implements the Clientv3Facade interface
func (m *mockKV) Compact(context.Context, int64, ...clientv3.CompactOption) (*clientv3.CompactResponse, error) {
	panic("Compact unimplemented")
}

// Do implements the Clientv3Facade interface
func (m *mockKV) Do(_ context.Context, op clientv3.Op) (clientv3.OpResponse, error) {
	m.valuesMtx.Lock()
	defer m.valuesMtx.Unlock()
	return m.doInternal(op)
}

func (m *mockKV) doInternal(op clientv3.Op) (clientv3.OpResponse, error) {
	if op.IsGet() {
		return m.doGet(op)
	}
	if op.IsPut() {
		return m.doPut(op)
	}
	if op.IsDelete() {
		return m.doDelete(op)
	}
	if op.IsTxn() {
		return m.doTxn(op)
	}
	panic(fmt.Sprintf("unsupported operation: %+v", op))
}

func (m *mockKV) doGet(op clientv3.Op) (clientv3.OpResponse, error) {
	matching := m.matchingKeys(op, m.values)
	kvs := make([]*mvccpb.KeyValue, 0, len(matching))

	for _, k := range matching {
		kv := m.values[k]
		kvs = append(kvs, proto.Clone(kv).(*mvccpb.KeyValue))
	}

	res := clientv3.GetResponse{
		Kvs:   kvs,
		Count: int64(len(kvs)),
	}

	return res.OpResponse(), nil
}

func (m *mockKV) doDelete(op clientv3.Op) (clientv3.OpResponse, error) {
	matching := m.matchingKeys(op, m.values)

	for _, k := range matching {
		kv := proto.Clone(m.values[k]).(*mvccpb.KeyValue)
		kv.ModRevision = kv.Version

		m.sendEvent(&clientv3.Event{
			Type: mvccpb.DELETE,
			Kv:   kv,
		})

		delete(m.values, k)
	}

	res := clientv3.DeleteResponse{Deleted: int64(len(matching))}
	return res.OpResponse(), nil
}

func (m *mockKV) doPut(op clientv3.Op) (clientv3.OpResponse, error) {
	keyBytes := op.KeyBytes()
	valBytes := op.ValueBytes()
	key := string(keyBytes)

	var newVal *mvccpb.KeyValue
	oldVal, ok := m.values[key]

	if ok {
		newVal = proto.Clone(oldVal).(*mvccpb.KeyValue)
		newVal.Version = newVal.Version + 1
		newVal.ModRevision = newVal.ModRevision + 1
		newVal.Value = valBytes
	} else {
		newVal = &mvccpb.KeyValue{
			Key:            keyBytes,
			Value:          valBytes,
			Version:        1,
			CreateRevision: 1,
			ModRevision:    1,
		}
	}

	m.values[key] = newVal
	m.sendEvent(&clientv3.Event{
		Type: mvccpb.PUT,
		Kv:   newVal,
	})

	res := clientv3.PutResponse{}
	return res.OpResponse(), nil
}

func (m *mockKV) doTxn(op clientv3.Op) (clientv3.OpResponse, error) {
	cmps, thens, elses := op.Txn()
	succeeded := m.evalCmps(cmps)

	var toRun []clientv3.Op
	if succeeded {
		toRun = thens
	} else {
		toRun = elses
	}

	responses := make([]*etcdserverpb.ResponseOp, 0, len(toRun))
	for _, o := range toRun {
		_, err := m.doInternal(o)
		if err != nil {
			panic(fmt.Sprintf("unexpected error running transaction: %s", err))
		}

		responses = append(responses, &etcdserverpb.ResponseOp{Response: nil})
	}

	res := clientv3.TxnResponse{
		Succeeded: succeeded,
		Responses: responses,
	}
	return res.OpResponse(), nil
}

// matchingKeys returns the keys of elements that match the given Op
func (m *mockKV) matchingKeys(op clientv3.Op, kvps map[string]*mvccpb.KeyValue) []string {
	// NOTE that even when Op is a prefix match, the key bytes will be the same
	// as they would be for an exact match. We use the fact that RangeBytes will
	// be non-nil for prefix matches to understand how to select keys.
	keyBytes := op.KeyBytes()
	rangeBytes := op.RangeBytes()
	keys := make([]string, 0)

	if keyBytes != nil && rangeBytes == nil {
		// Exact match
		k := string(keyBytes)

		if _, ok := kvps[k]; ok {
			keys = append(keys, k)
		}
	} else if keyBytes != nil {
		// Prefix match
		emptyPrefix := len(keyBytes) == 1 && keyBytes[0] == byte(0)
		for k := range kvps {
			if emptyPrefix || bytes.HasPrefix([]byte(k), keyBytes) {
				keys = append(keys, k)
			}
		}
	}

	return keys
}

// isMatch returns true if the provided key-value pair matches the given Op
func (m *mockKV) isMatch(op clientv3.Op, kvp *mvccpb.KeyValue) bool {
	keys := m.matchingKeys(op, map[string]*mvccpb.KeyValue{string(kvp.Key): kvp})
	return len(keys) != 0
}

// evalCmps executes the given comparisons, stopping as soon as the first one
// evaluates to false.
func (m *mockKV) evalCmps(cmps []clientv3.Cmp) bool {
	for _, c := range cmps {
		if !m.evalCmp(c) {
			return false
		}
	}

	return true
}

// evalCmp executes the given comparison between some field of a key-value
// pair stored in mockKV and a provided value. The field may be the value
// stored or one of the various version numbers associated with it.
func (m *mockKV) evalCmp(cmp clientv3.Cmp) bool {
	pbCmp := cmp.GetCompare()

	// Etcd treats a missing key as having zero-valued comparison fields, allowing
	// version == 0 to implement create-if-absent transactions.
	entry := m.values[string(pbCmp.GetKey())]
	if entry == nil {
		entry = &mvccpb.KeyValue{}
	}

	switch pbCmp.GetTarget() {
	case etcdserverpb.Compare_VALUE:
		return m.evalEntryBytes(entry.Value, pbCmp.GetValue(), pbCmp)
	case etcdserverpb.Compare_CREATE:
		return m.evalEntryInt64(entry.CreateRevision, pbCmp.GetCreateRevision(), pbCmp)
	case etcdserverpb.Compare_LEASE:
		return m.evalEntryInt64(entry.Lease, pbCmp.GetLease(), pbCmp)
	case etcdserverpb.Compare_VERSION:
		return m.evalEntryInt64(entry.Version, pbCmp.GetVersion(), pbCmp)
	case etcdserverpb.Compare_MOD:
		return m.evalEntryInt64(entry.ModRevision, pbCmp.GetModRevision(), pbCmp)
	default:
		panic(fmt.Sprintf("unsupported target for cmp: %+v", pbCmp))
	}
}

// evalEntryBytes returns true if the provided comparison between two
// byte slices is true, false otherwise. This is used to compare values
// stored by the mockKV as part of transactions.
func (m *mockKV) evalEntryBytes(v1 []byte, v2 []byte, cmp *etcdserverpb.Compare) bool {
	res := bytes.Compare(v1, v2)

	switch cmp.GetResult() {
	case etcdserverpb.Compare_EQUAL:
		return res == 0
	case etcdserverpb.Compare_GREATER:
		return res > 0
	case etcdserverpb.Compare_LESS:
		return res < 0
	case etcdserverpb.Compare_NOT_EQUAL:
		return res != 0
	default:
		panic(fmt.Sprintf("unsupported result for cmp: %+v", cmp))
	}
}

// evalEntryInt64 returns true if the provided comparison between two ints
// is true, false otherwise. This is used to compared various version numbers
// of key-value pairs stored by the mockKV as part of a transaction.
func (m *mockKV) evalEntryInt64(v1 int64, v2 int64, cmp *etcdserverpb.Compare) bool {
	switch cmp.GetResult() {
	case etcdserverpb.Compare_EQUAL:
		return v1 == v2
	case etcdserverpb.Compare_GREATER:
		return v1 > v2
	case etcdserverpb.Compare_LESS:
		return v1 < v2
	case etcdserverpb.Compare_NOT_EQUAL:
		return v1 != v2
	default:
		panic(fmt.Sprintf("unsupported result for cmp: %+v", cmp))
	}
}

// mockTxn builds up comparisons, success actions and fallback actions to
// be run as part of a transaction. Once the transaction is ready to be
// executed, all the comparisons and actions are run by the mockKV instance
// that started this transaction.
type mockTxn struct {
	kv      *mockKV
	ctx     context.Context
	cmps    []clientv3.Cmp
	thenOps []clientv3.Op
	elseOps []clientv3.Op
}

func (m *mockTxn) If(cs ...clientv3.Cmp) clientv3.Txn {
	m.cmps = append(m.cmps, cs...)
	return m
}

func (m *mockTxn) Then(ops ...clientv3.Op) clientv3.Txn {
	m.thenOps = append(m.thenOps, ops...)
	return m
}

func (m *mockTxn) Else(ops ...clientv3.Op) clientv3.Txn {
	m.elseOps = append(m.elseOps, ops...)
	return m
}

func (m *mockTxn) Commit() (*clientv3.TxnResponse, error) {
	op := clientv3.OpTxn(m.cmps, m.thenOps, m.elseOps)
	// Note that we're using the Do method here instead of doInternal since we didn't
	// acquire the data lock when starting this transaction.
	res, err := m.kv.Do(m.ctx, op)

	if err != nil {
		return nil, err
	}

	return res.Txn(), nil
}
