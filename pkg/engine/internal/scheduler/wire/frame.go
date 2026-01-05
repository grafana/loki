package wire

import (
	"fmt"
)

// FrameKind represents the type of a Frame.
type FrameKind int

const (
	// FrameKindInvalid represents an invalid frame.
	FrameKindInvalid FrameKind = iota

	FrameKindMessage // FrameKindMessage is used for [MessageFrame].
	FrameKindAck     // FrameKindAck is used for [AckFrame].
	FrameKindNack    // FrameKindNack is used for [NackFrame].
	FrameKindDiscard // FrameKindDiscard is used for [DiscardFrame].
)

var frameKindNames = [...]string{
	FrameKindInvalid: "FrameKindInvalid",
	FrameKindMessage: "FrameKindMessage",
	FrameKindAck:     "FrameKindAck",
	FrameKindNack:    "FrameKindNack",
	FrameKindDiscard: "FrameKindDiscard",
}

// String returns a string representation of k.
func (k FrameKind) String() string {
	if k < 0 || int(k) >= len(frameKindNames) {
		return fmt.Sprintf("FrameKind(%d)", k)
	}
	return frameKindNames[k]
}

// A Frame is the lowest level of communication between two peers. Frames are an
// envelope for messages between peers.
type Frame interface {
	isFrame()
	FrameKind() FrameKind
}

// MessageFrame is a Frame that sends a [Message] to the peer. MessageFrames are
// paired with an [AckFrame] to acknowledge that the message has been
// successfully processed, or [NackFrame] in case of failure.
type MessageFrame struct {
	// ID uniquely identifies the message. It is up to the sender to ensure that
	// IDs are unique within a stream.
	ID uint64

	// Message being sent to the peer.
	Message Message
}

// FrameKind returns [FrameKindMessage].
func (m MessageFrame) FrameKind() FrameKind { return FrameKindMessage }

// AckFrame is a Frame that acknowledges a [MessageFrame] was processed
// successfully.
type AckFrame struct {
	// ID of the [MessageFrame] being acknowledged.
	ID uint64
}

// FrameKind returns [FrameKindAck].
func (a AckFrame) FrameKind() FrameKind { return FrameKindAck }

// NackFrame is a Frame that notifies that a [MessageFrame] could not be
// processed.
type NackFrame struct {
	// ID of the [MessageFrame] being negatively acknowledged.
	ID uint64

	// Error is the error that occurred.
	Error *Error
}

// FrameKind returns [FrameKindNack].
func (n NackFrame) FrameKind() FrameKind { return FrameKindNack }

// DiscardFrame is a Frame that informs the peer that a [MessageFrame] has
// been discarded and an acknowledgement is no longer needed.
//
// A peer receiving a DiscardFrame should produce no acknowledgement.
type DiscardFrame struct {
	// ID of the [MessageFrame] being discarded.
	ID uint64
}

// FrameKind returns [FrameKindDiscard].
func (d DiscardFrame) FrameKind() FrameKind { return FrameKindDiscard }

// Marker implementations.

func (m MessageFrame) isFrame() {}
func (a AckFrame) isFrame()     {}
func (n NackFrame) isFrame()    {}
func (d DiscardFrame) isFrame() {}
