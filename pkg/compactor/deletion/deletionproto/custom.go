package deletionproto

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

func (c *Chunk) GetSize() uint32 {
	return c.KB
}

func (c *Chunk) GetEntriesCount() uint32 {
	return c.Entries
}

// IsZero reports whether the chunk carries no data. A zero-value entry in
// StorageUpdates.RebuiltChunks means "remove the source chunk without a
// rebuilt replacement" — the role a nil map value played when the map was
// pointer-valued under gogoproto (wiresmith map values are value-typed and
// have no pointer option). Wire format is unchanged: both encode as a map
// entry whose value field is absent.
func (c Chunk) IsZero() bool {
	return c == Chunk{}
}
