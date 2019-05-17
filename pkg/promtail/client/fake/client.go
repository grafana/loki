package fake

import (
	"time"

	"github.com/grafana/loki/pkg/promtail/api"
	"github.com/prometheus/common/model"
)

// Client is a fake client used for testing.
type Client struct {
	OnHandleEntry api.EntryHandlerFunc
	OnStop        func()
}

// Stop implements client.Client
func (c *Client) Stop() {
	c.OnStop()
}

// Handle implements client.Client
func (c *Client) Handle(labels model.LabelSet, time time.Time, entry string) error {
	return c.OnHandleEntry.Handle(labels, time, entry)
}
