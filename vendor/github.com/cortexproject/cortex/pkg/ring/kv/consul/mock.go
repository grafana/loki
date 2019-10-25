package consul

import (
	"fmt"
	"sync"
	"time"

	"github.com/go-kit/kit/log/level"
	consul "github.com/hashicorp/consul/api"

	"github.com/cortexproject/cortex/pkg/ring/kv/codec"
	"github.com/cortexproject/cortex/pkg/util"
)

type mockKV struct {
	mtx     sync.Mutex
	cond    *sync.Cond
	kvps    map[string]*consul.KVPair
	current uint64 // the current 'index in the log'
}

// NewInMemoryClient makes a new mock consul client.
func NewInMemoryClient(codec codec.Codec) *Client {
	return NewInMemoryClientWithConfig(codec, Config{})
}

// NewInMemoryClientWithConfig makes a new mock consul client with supplied Config.
func NewInMemoryClientWithConfig(codec codec.Codec, cfg Config) *Client {
	m := mockKV{
		kvps: map[string]*consul.KVPair{},
	}
	m.cond = sync.NewCond(&m.mtx)
	go m.loop()
	return &Client{
		kv:    &m,
		codec: codec,
		cfg:   cfg,
	}
}

func copyKVPair(in *consul.KVPair) *consul.KVPair {
	out := *in
	out.Value = make([]byte, len(in.Value))
	copy(out.Value, in.Value)
	return &out
}

// periodic loop to wake people up, so they can honour timeouts
func (m *mockKV) loop() {
	for range time.Tick(1 * time.Second) {
		m.mtx.Lock()
		m.cond.Broadcast()
		m.mtx.Unlock()
	}
}

func (m *mockKV) Put(p *consul.KVPair, q *consul.WriteOptions) (*consul.WriteMeta, error) {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	m.current++
	existing, ok := m.kvps[p.Key]
	if ok {
		existing.Value = p.Value
		existing.ModifyIndex = m.current
	} else {
		m.kvps[p.Key] = &consul.KVPair{
			Key:         p.Key,
			Value:       p.Value,
			CreateIndex: m.current,
			ModifyIndex: m.current,
		}
	}

	m.cond.Broadcast()

	level.Debug(util.Logger).Log("msg", "Put", "key", p.Key, "value", fmt.Sprintf("%.40q", p.Value), "modify_index", m.current)
	return nil, nil
}

func (m *mockKV) CAS(p *consul.KVPair, q *consul.WriteOptions) (bool, *consul.WriteMeta, error) {
	level.Debug(util.Logger).Log("msg", "CAS", "key", p.Key, "modify_index", p.ModifyIndex, "value", fmt.Sprintf("%.40q", p.Value))

	m.mtx.Lock()
	defer m.mtx.Unlock()
	existing, ok := m.kvps[p.Key]
	if ok && existing.ModifyIndex != p.ModifyIndex {
		return false, nil, nil
	}

	m.current++
	if ok {
		existing.Value = p.Value
		existing.ModifyIndex = m.current
	} else {
		m.kvps[p.Key] = &consul.KVPair{
			Key:         p.Key,
			Value:       p.Value,
			CreateIndex: m.current,
			ModifyIndex: m.current,
		}
	}

	m.cond.Broadcast()
	return true, nil, nil
}

func (m *mockKV) Get(key string, q *consul.QueryOptions) (*consul.KVPair, *consul.QueryMeta, error) {
	level.Debug(util.Logger).Log("msg", "Get", "key", key, "wait_index", q.WaitIndex)

	m.mtx.Lock()
	defer m.mtx.Unlock()

	value, ok := m.kvps[key]
	if !ok {
		level.Debug(util.Logger).Log("msg", "Get - not found", "key", key)
		return nil, &consul.QueryMeta{LastIndex: m.current}, nil
	}

	if q.WaitIndex >= value.ModifyIndex && q.WaitTime > 0 {
		deadline := time.Now().Add(q.WaitTime)
		if ctxDeadline, ok := q.Context().Deadline(); ok && ctxDeadline.Before(deadline) {
			// respect deadline from context, if set.
			deadline = ctxDeadline
		}

		// simply wait until value.ModifyIndex changes. This allows us to test reporting old index values by resetting them.
		startModify := value.ModifyIndex
		for startModify == value.ModifyIndex && time.Now().Before(deadline) {
			m.cond.Wait()
		}
		if time.Now().After(deadline) {
			level.Debug(util.Logger).Log("msg", "Get - deadline exceeded", "key", key)
			return nil, &consul.QueryMeta{LastIndex: q.WaitIndex}, nil
		}
	}

	level.Debug(util.Logger).Log("msg", "Get", "key", key, "modify_index", value.ModifyIndex, "value", fmt.Sprintf("%.40q", value.Value))
	return copyKVPair(value), &consul.QueryMeta{LastIndex: value.ModifyIndex}, nil
}

func (m *mockKV) List(prefix string, q *consul.QueryOptions) (consul.KVPairs, *consul.QueryMeta, error) {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	deadline := time.Now().Add(q.WaitTime)
	for q.WaitIndex >= m.current && time.Now().Before(deadline) {
		m.cond.Wait()
	}
	if time.Now().After(deadline) {
		return nil, &consul.QueryMeta{LastIndex: q.WaitIndex}, nil
	}

	result := consul.KVPairs{}
	for _, kvp := range m.kvps {
		if kvp.ModifyIndex >= q.WaitIndex {
			result = append(result, copyKVPair(kvp))
		}
	}
	return result, &consul.QueryMeta{LastIndex: m.current}, nil
}

func (m *mockKV) ResetIndex() {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	m.current = 0
	m.cond.Broadcast()

	level.Debug(util.Logger).Log("msg", "Reset")
}

func (m *mockKV) ResetIndexForKey(key string) {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	if value, ok := m.kvps[key]; ok {
		value.ModifyIndex = 0
	}

	m.cond.Broadcast()
	level.Debug(util.Logger).Log("msg", "ResetIndexForKey", "key", key)
}
