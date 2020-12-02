package fake

import (
	"sync"

	"github.com/grafana/loki/pkg/promtail/api"
)

// Client is a fake client used for testing.
type Client struct {
	entries  chan api.Entry
	received []api.Entry
	once     sync.Once
	mtx      sync.Mutex
	wg       sync.WaitGroup
	OnStop   func()
}

func New(stop func()) *Client {
	c := &Client{
		OnStop:  stop,
		entries: make(chan api.Entry),
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
func (c *Client) Stop() {
	c.once.Do(func() { close(c.entries) })
	c.wg.Wait()
	c.OnStop()
}

func (c *Client) Chan() chan<- api.Entry {
	return c.entries
}

func (c *Client) Received() []api.Entry {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	cpy := make([]api.Entry, len(c.received))
	copy(cpy, c.received)
	return cpy
}

// StopNow implements client.Client
func (c *Client) StopNow() {
	c.Stop()
}
