package wire

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// FrameProtocol handles reading and writing frames over a connection.
// FrameProtocol is stateful and bound to specific reader/writer during initialization.
// It is not safe for concurrent use.
type FrameProtocol interface {
	// BindWriter binds the protocol to a writer. Must be called before WriteFrame.
	BindWriter(w io.Writer)

	// BindReader binds the protocol to a reader. Must be called before ReadFrame.
	BindReader(r io.Reader)

	// WriteFrame writes a frame to the bound writer.
	WriteFrame(frame Frame) error

	// ReadFrame reads a frame from the bound reader.
	ReadFrame() (Frame, error)
}

// JSONProtocol implements a simple JSON-based protocol for frames.
type JSONProtocol struct {
	enc *json.Encoder
	dec *json.Decoder
}

// NewJSONProtocol creates a new JSON protocol.
func NewJSONProtocol() FrameProtocol {
	return &JSONProtocol{}
}

// BindWriter binds the protocol to a writer.
func (p *JSONProtocol) BindWriter(w io.Writer) {
	p.enc = json.NewEncoder(w)
}

// BindReader binds the protocol to a reader.
func (p *JSONProtocol) BindReader(r io.Reader) {
	p.dec = json.NewDecoder(r)
}

// frameWrapper wraps a frame with its kind for JSON serialization.
type frameWrapper struct {
	Kind  FrameKind       `json:"kind"`
	Frame json.RawMessage `json:"frame"`
}

// messageFrameJSON is the JSON representation of MessageFrame with message kind.
type messageFrameJSON struct {
	ID          uint64          `json:"id"`
	MessageKind MessageKind     `json:"messageKind"`
	Message     json.RawMessage `json:"message"`
}

// nackFrameJSON is the JSON representation of NackFrame with error as string.
type nackFrameJSON struct {
	ID    uint64 `json:"id"`
	Error string `json:"error,omitempty"`
}

// WriteFrame encodes a frame as JSON and writes it to the bound writer.
func (p *JSONProtocol) WriteFrame(frame Frame) error {

	var data []byte
	var err error

	// Handle MessageFrame specially because it contains an interface
	if mf, ok := frame.(MessageFrame); ok {
		msgData, err := json.Marshal(mf.Message)
		if err != nil {
			return err
		}
		data, err = json.Marshal(messageFrameJSON{
			ID:          mf.ID,
			MessageKind: mf.Message.Kind(),
			Message:     msgData,
		})
		if err != nil {
			return err
		}
	} else if nf, ok := frame.(NackFrame); ok {
		// Handle NackFrame specially to convert error to string
		var errStr string
		if nf.Error != nil {
			errStr = nf.Error.Error()
		}
		data, err = json.Marshal(nackFrameJSON{
			ID:    nf.ID,
			Error: errStr,
		})
		if err != nil {
			return err
		}
	} else {
		data, err = json.Marshal(frame)
		if err != nil {
			return err
		}
	}

	wrapper := frameWrapper{
		Kind:  frame.FrameKind(),
		Frame: data,
	}

	return p.enc.Encode(wrapper)
}

// ReadFrame reads and decodes a frame from the bound reader.
func (p *JSONProtocol) ReadFrame() (Frame, error) {
	var wrapper frameWrapper
	if err := p.dec.Decode(&wrapper); err != nil {
		return nil, err
	}

	switch wrapper.Kind {
	case FrameKindMessage:
		var mfj messageFrameJSON
		if err := json.Unmarshal(wrapper.Frame, &mfj); err != nil {
			return nil, err
		}

		msg, err := decodeMessage(mfj.MessageKind, mfj.Message)
		if err != nil {
			return nil, err
		}

		return MessageFrame{
			ID:      mfj.ID,
			Message: msg,
		}, nil

	case FrameKindAck:
		var f AckFrame
		if err := json.Unmarshal(wrapper.Frame, &f); err != nil {
			return nil, err
		}
		return f, nil

	case FrameKindNack:
		var nfj nackFrameJSON
		if err := json.Unmarshal(wrapper.Frame, &nfj); err != nil {
			return nil, err
		}
		nf := NackFrame{
			ID: nfj.ID,
		}
		if nfj.Error != "" {
			nf.Error = errors.New(nfj.Error)
		}
		return nf, nil

	case FrameKindDiscard:
		var f DiscardFrame
		if err := json.Unmarshal(wrapper.Frame, &f); err != nil {
			return nil, err
		}
		return f, nil

	default:
		return nil, fmt.Errorf("unknown frame kind: %v", wrapper.Kind)
	}
}

// decodeMessage decodes a message based on its kind.
func decodeMessage(kind MessageKind, data []byte) (Message, error) {
	switch kind {
	case MessageKindWorkerReady:
		var msg WorkerReadyMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, err
		}
		return msg, nil

	case MessageKindTaskAssign:
		var msg TaskAssignMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, err
		}
		return msg, nil

	case MessageKindTaskCancel:
		var msg TaskCancelMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, err
		}
		return msg, nil

	case MessageKindTaskFlag:
		var msg TaskFlagMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, err
		}
		return msg, nil

	case MessageKindTaskStatus:
		var msg TaskStatusMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, err
		}
		return msg, nil

	case MessageKindStreamBind:
		var msg StreamBindMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, err
		}
		return msg, nil

	case MessageKindStreamData:
		var msg StreamDataMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, err
		}
		return msg, nil

	case MessageKindStreamStatus:
		var msg StreamStatusMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, err
		}
		return msg, nil

	default:
		return nil, fmt.Errorf("unknown message kind: %v", kind)
	}
}
