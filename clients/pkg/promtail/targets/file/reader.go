package file

type Reader interface {
	Stop()
	IsRunning() bool
	Path() string
	MarkPositionAndSize() error
}
