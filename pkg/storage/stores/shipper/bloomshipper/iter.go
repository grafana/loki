package bloomshipper

type ChannelIter[T any] struct {
	ch  <-chan T
	cur T
}

func NewChannelIter[T any](ch <-chan T) *ChannelIter[T] {
	return &ChannelIter[T]{
		ch: ch,
	}
}

func (it *ChannelIter[T]) Next() bool {
	el, ok := <-it.ch
	if ok {
		it.cur = el
		return true
	}
	return false
}

func (it *ChannelIter[T]) At() T {
	return it.cur
}

func (it *ChannelIter[T]) Err() error {
	return nil
}
