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
