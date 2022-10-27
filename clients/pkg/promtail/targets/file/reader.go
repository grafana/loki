package file

// Reader contains the set of expected calls the file target manager relies on.
type Reader interface {
	Start() error
	Stop()
	IsRunning() bool
	Path() string
	MarkPositionAndSize() error
}
