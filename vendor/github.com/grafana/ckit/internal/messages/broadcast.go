package messages

import "github.com/hashicorp/memberlist"

// Broadcast converts m into a memberlist Broadcast. m should not change once
// being converted into a Broadcast.
//
// onDone will be called once the message has been broadcasted or invalidated.
//
// Queueing the resulting Broadcast will invalidate all previous broadcasts
// for messages with the same name; this means that callers should only queue
// newer messages.
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

// We want to implement [memberlist.NamedBroadcast] because it goes through a
// fast path when queueing messages for broadcasting, where the Name is
// stored in a map to immediately invalidate older messages with the same
// Name.
//
// Without this, each time we queue a message, we call Invalidates on all
// previously queued messages, for a total of N(N + 1) / 2 calls.
var (
	_ memberlist.Broadcast      = (*broadcastWrapper)(nil)
	_ memberlist.NamedBroadcast = (*broadcastWrapper)(nil)
)

func (bw *broadcastWrapper) Invalidates(b memberlist.Broadcast) bool {
	other, ok := b.(*broadcastWrapper)
	if !ok {
		return false
	}
	return bw.inner.Name() == other.inner.Name()
}

func (bw *broadcastWrapper) Message() []byte {
	return bw.data
}

func (bw *broadcastWrapper) Name() string {
	return bw.inner.Name()
}

func (bw *broadcastWrapper) Finished() {
	if bw.onDone != nil {
		bw.onDone()
	}
}
