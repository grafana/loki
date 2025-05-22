package messages

import "github.com/hashicorp/memberlist"

// Broadcast converts m into a memberlist Broadcast. m should not change once
// being converted into a Broadcast.
//
// onDone will be called once the message has been broadcasted or invalidated.
func Broadcast(m Message, onDone func()) (memberlist.Broadcast, error) {
	bb, err := Encode(m)
	if err != nil {
		return nil, err
	}

	return &broadcastWrapper{inner: m, data: bb, onDone: onDone}, nil
}

type broadcastWrapper struct {
	inner  Message
	data   []byte
	onDone func()
}

func (bw *broadcastWrapper) Invalidates(b memberlist.Broadcast) bool {
	other, ok := b.(*broadcastWrapper)
	if !ok {
		return false
	}
	return bw.inner.Invalidates(other.inner)
}

func (bw *broadcastWrapper) Message() []byte {
	return bw.data
}

func (bw *broadcastWrapper) Finished() {
	if bw.onDone != nil {
		bw.onDone()
	}
}
