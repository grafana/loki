package ragel

// ReadingError represents errors occurred in the readers.
type ReadingError struct {
	message string
}

// NewReadingError creates a ReadingError with the given message.
func NewReadingError(message string) *ReadingError {
	return &ReadingError{
		message: message,
	}
}

func (e *ReadingError) Error() string {
	return e.message
}

var (
	// ErrNotFound is the message representing a situation in which the needle we were looking for has not been found.
	ErrNotFound = "needle not found"
)
