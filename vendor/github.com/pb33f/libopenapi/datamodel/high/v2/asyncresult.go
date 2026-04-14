package v2

type asyncResult[T any] struct {
	key    string
	result T
}
