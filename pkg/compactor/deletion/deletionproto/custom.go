package deletionproto

import "github.com/prometheus/common/model"

const (
	StatusReceived  DeleteRequestStatus = "received"
	StatusProcessed DeleteRequestStatus = "processed"
)

type (
	DeleteRequestStatus string
)

func (s DeleteRequestStatus) Equal(status DeleteRequestStatus) bool {
	return s == status
}

func (c *Chunk) GetFrom() model.Time {
	return c.From
}

func (c *Chunk) GetThrough() model.Time {
	return c.Through
}

func (c *Chunk) GetSize() uint32 {
	return c.KB
}

func (c *Chunk) GetEntriesCount() uint32 {
	return c.Entries
}
