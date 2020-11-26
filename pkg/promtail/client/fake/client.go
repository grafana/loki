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
	OnStop   func()
}

func New(stop func()) *Client {
	c := &Client{
		OnStop:  stop,
		entries: make(chan api.Entry),
	}
	go func() {
		for e := range c.entries {
			c.received = append(c.received, e)
		}
	}()
	return c
}

// Stop implements client.Client
func (c *Client) Stop() {
	c.once.Do(func() { close(c.entries) })
	c.OnStop()
}

func (c *Client) Chan() chan<- api.Entry {
	return c.entries
}

func (c *Client) Received() []api.Entry {
	return c.received
}
