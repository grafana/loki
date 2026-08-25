package util

import (
	"sync"
)

// Client is a fake client used for testing.
type FakeClient struct {
	entries  chan Entry
	received []Entry
	once     sync.Once
	mtx      sync.Mutex
	wg       sync.WaitGroup
	OnStop   func()
}

func NewFakeClient(stop func()) *FakeClient {
	c := &FakeClient{
		OnStop:  stop,
		entries: make(chan Entry),
	}
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		for e := range c.entries {
			c.mtx.Lock()
			c.received = append(c.received, e)
			c.mtx.Unlock()
		}
	}()
	return c
}

// Stop implements client.Client
func (c *FakeClient) Stop() {
	c.once.Do(func() { close(c.entries) })
	c.wg.Wait()
	c.OnStop()
}

func (c *FakeClient) Chan() chan<- Entry {
	return c.entries
}

func (c *FakeClient) Received() []Entry {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	cpy := make([]Entry, len(c.received))
	copy(cpy, c.received)
	return cpy
}

// StopNow implements client.Client
func (c *FakeClient) StopNow() {
	c.Stop()
}

func (c *FakeClient) Name() string {
	return "fake"
}

// Clear is used to cleanup the buffered received entries, so the same client can be re-used between
// test cases.
func (c *FakeClient) Clear() {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.received = []Entry{}
}
