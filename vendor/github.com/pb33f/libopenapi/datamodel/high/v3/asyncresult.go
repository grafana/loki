package v3

type asyncResult[T any] struct {
	key    string
	result T
}
