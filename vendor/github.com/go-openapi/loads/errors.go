package loads

type loaderError string

func (e loaderError) Error() string {
	return string(e)
}

const (
	// ErrLoads is an error returned by the loads package
	ErrLoads loaderError = "loaderrs error"

	// ErrNoLoader indicates that no configured loader matched the input
	ErrNoLoader loaderError = "no loader matched"
)
