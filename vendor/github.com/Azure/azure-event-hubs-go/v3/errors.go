package eventhub

type (
	// ErrNoMessages is returned when an operation returned no messages. It is not indicative that there will not be
	// more messages in the future.
	ErrNoMessages struct{}
)

func (e ErrNoMessages) Error() string {
	return "no messages available"
}
