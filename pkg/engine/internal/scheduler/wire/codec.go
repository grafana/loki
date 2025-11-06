package wire

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/gogo/protobuf/proto"
	"github.com/oklog/ulid/v2"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/proto/physicalpb"
	ulidpb "github.com/grafana/loki/v3/pkg/engine/internal/proto/ulid"
	"github.com/grafana/loki/v3/pkg/engine/internal/proto/wirepb"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
)

type FrameCodec interface {
	// EncodeTo writes a frame to the [io.Writer]
	EncodeTo(w io.Writer, frame Frame) error

	// DecodeFrom reads a frame from the [io.Reader]
	DecodeFrom(r io.Reader) (Frame, error)
}

// ProtoFrameCodec implements a protobuf-based protocol for frames.
// Messages are length-prefixed: [4-byte length][protobuf payload]
type ProtoFrameCodec struct {
	*Codec
	maxFrameSizeBytes uint32
}

var _ FrameCodec = (*ProtoFrameCodec)(nil)

const DefaultMaxFrameSizeBytes = 100 * 1024 * 1024 // 100MB
var DefaultProtoFrameCodec = NewProtoFrameCodec(memory.NewGoAllocator(), DefaultMaxFrameSizeBytes)

func NewProtoFrameCodec(allocator memory.Allocator, maxFrameSizeBytes uint32) *ProtoFrameCodec {
	return &ProtoFrameCodec{
		Codec:             &Codec{allocator},
		maxFrameSizeBytes: maxFrameSizeBytes,
	}
}

// WriteFrame encodes a frame as protobuf and writes it to the writer.
// Format: [4-byte length (big-endian)][protobuf payload]
func (p *ProtoFrameCodec) EncodeTo(w io.Writer, frame Frame) error {
	// Convert wire.Frame to protobuf
	pbFrame, err := p.Codec.FrameToProto(frame)
	if err != nil {
		return fmt.Errorf("failed to convert frame to protobuf: %w", err)
	}

	// Marshal to bytes
	data, err := proto.Marshal(pbFrame)
	if err != nil {
		return fmt.Errorf("failed to marshal protobuf: %w", err)
	}

	// Write length prefix (4 bytes, big-endian)
	length := uint32(len(data))
	if err := binary.Write(w, binary.BigEndian, length); err != nil {
		return fmt.Errorf("failed to write length prefix: %w", err)
	}

	// Write payload
	n, err := w.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write payload: %w", err)
	}
	if n != len(data) {
		return fmt.Errorf("incomplete write: wrote %d bytes, expected %d", n, len(data))
	}

	return nil
}

// ReadFrame reads and decodes a frame from the bound reader.
// Format: [4-byte length (big-endian)][protobuf payload]
func (p *ProtoFrameCodec) DecodeFrom(r io.Reader) (Frame, error) {
	// Read length prefix (4 bytes, big-endian)
	var length uint32
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return nil, fmt.Errorf("failed to read length prefix: %w", err)
	}

	// Sanity check: prevent excessive allocations
	if length > p.maxFrameSizeBytes {
		return nil, fmt.Errorf("frame size %d exceeds maximum %d", length, p.maxFrameSizeBytes)
	}

	// Read payload
	data := make([]byte, length)
	n, err := io.ReadFull(r, data)
	if err != nil {
		return nil, fmt.Errorf("failed to read payload: %w", err)
	}
	if n != int(length) {
		return nil, fmt.Errorf("incomplete read: read %d bytes, expected %d", n, length)
	}

	// Unmarshal protobuf
	pbFrame := &wirepb.Frame{}
	if err := proto.Unmarshal(data, pbFrame); err != nil {
		return nil, fmt.Errorf("failed to unmarshal protobuf: %w", err)
	}

	// Convert protobuf to wire.Frame
	frame, err := p.Codec.FrameFromProto(pbFrame)
	if err != nil {
		return nil, fmt.Errorf("failed to convert protobuf to frame: %w", err)
	}

	return frame, nil
}

type Codec struct {
	allocator memory.Allocator
}

func (m *Codec) FrameFromProto(f *wirepb.Frame) (Frame, error) {
	if f == nil {
		return nil, errors.New("nil frame")
	}

	switch k := f.Kind.(type) {
	case *wirepb.Frame_Ack:
		return AckFrame{ID: k.Ack.Id}, nil

	case *wirepb.Frame_Nack:
		var err error
		if k.Nack.Error != "" {
			err = errors.New(k.Nack.Error)
		}
		return NackFrame{
			ID:    k.Nack.Id,
			Error: err,
		}, nil

	case *wirepb.Frame_Discard:
		return DiscardFrame{ID: k.Discard.Id}, nil

	case *wirepb.Frame_Message:
		msg, err := m.MessageFromProto(k.Message)
		if err != nil {
			return nil, err
		}
		return MessageFrame{
			ID:      k.Message.Id,
			Message: msg,
		}, nil

	default:
		return nil, fmt.Errorf("unknown frame kind: %T", k)
	}
}

func (m *Codec) MessageFromProto(mf *wirepb.MessageFrame) (Message, error) {
	if mf == nil {
		return nil, errors.New("nil message frame")
	}

	switch k := mf.Kind.(type) {
	case *wirepb.MessageFrame_WorkerReady:
		return WorkerReadyMessage{}, nil

	case *wirepb.MessageFrame_TaskAssign:
		task, err := m.TaskFromProto(k.TaskAssign.Task)
		if err != nil {
			return nil, err
		}

		streamStates := make(map[ulid.ULID]workflow.StreamState)
		for idStr, state := range k.TaskAssign.StreamStates {
			id, err := ulid.Parse(idStr)
			if err != nil {
				return nil, fmt.Errorf("invalid stream ID %q: %w", idStr, err)
			}
			streamStates[id] = m.streamStateFromProto(state)
		}

		return TaskAssignMessage{
			Task:         task,
			StreamStates: streamStates,
		}, nil

	case *wirepb.MessageFrame_TaskCancel:
		return TaskCancelMessage{
			ID: ulid.ULID(k.TaskCancel.Id),
		}, nil

	case *wirepb.MessageFrame_TaskFlag:
		return TaskFlagMessage{
			ID:            ulid.ULID(k.TaskFlag.Id),
			Interruptible: k.TaskFlag.Interruptible,
		}, nil

	case *wirepb.MessageFrame_TaskStatus:
		status, err := m.taskStatusFromProto(&k.TaskStatus.Status)
		if err != nil {
			return nil, err
		}
		return TaskStatusMessage{
			ID:     ulid.ULID(k.TaskStatus.Id),
			Status: status,
		}, nil

	case *wirepb.MessageFrame_StreamBind:
		addr, err := newTCPAddrFromString(k.StreamBind.Receiver)
		if err != nil {
			return nil, fmt.Errorf("invalid receiver address: %w", err)
		}
		return StreamBindMessage{
			StreamID: ulid.ULID(k.StreamBind.StreamId),
			Receiver: addr,
		}, nil

	case *wirepb.MessageFrame_StreamData:
		// Deserialize Arrow record from bytes
		record, err := m.deserializeArrowRecord(k.StreamData.Data)
		if err != nil {
			return nil, fmt.Errorf("failed to deserialize arrow record: %w", err)
		}
		return StreamDataMessage{
			StreamID: ulid.ULID(k.StreamData.StreamId),
			Data:     record,
		}, nil

	case *wirepb.MessageFrame_StreamStatus:
		return StreamStatusMessage{
			StreamID: ulid.ULID(k.StreamStatus.StreamId),
			State:    m.streamStateFromProto(k.StreamStatus.State),
		}, nil

	default:
		return nil, fmt.Errorf("unknown message kind: %T", k)
	}
}

func (m *Codec) TaskFromProto(t *wirepb.Task) (*workflow.Task, error) {
	if t == nil {
		return nil, errors.New("nil task")
	}

	fragment, err := t.Fragment.MarshalPhysical()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal fragment: %w", err)
	}

	sources, err := m.nodeStreamMapFromPbNodeStreamList(t.Sources, fragment)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal sources: %w", err)
	}

	sinks, err := m.nodeStreamMapFromPbNodeStreamList(t.Sinks, fragment)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal sinks: %w", err)
	}

	return &workflow.Task{
		ULID:     ulid.ULID(t.Ulid),
		TenantID: t.TenantId,
		Fragment: fragment,
		Sources:  sources,
		Sinks:    sinks,
	}, nil
}

func (m *Codec) taskStatusFromProto(ts *wirepb.TaskStatus) (workflow.TaskStatus, error) {
	if ts == nil {
		return workflow.TaskStatus{}, errors.New("nil task status")
	}

	status := workflow.TaskStatus{
		State: m.taskStateFromProto(ts.State),
	}

	if ts.Error != "" {
		status.Error = errors.New(ts.Error)
	}

	return status, nil
}

func (m *Codec) taskStateFromProto(state wirepb.TaskState) workflow.TaskState {
	switch state {
	case wirepb.TASK_STATE_CREATED:
		return workflow.TaskStateCreated
	case wirepb.TASK_STATE_PENDING:
		return workflow.TaskStatePending
	case wirepb.TASK_STATE_RUNNING:
		return workflow.TaskStateRunning
	case wirepb.TASK_STATE_COMPLETED:
		return workflow.TaskStateCompleted
	case wirepb.TASK_STATE_CANCELLED:
		return workflow.TaskStateCancelled
	case wirepb.TASK_STATE_FAILED:
		return workflow.TaskStateFailed
	default:
		return workflow.TaskStateCreated
	}
}

func (m *Codec) streamStateFromProto(state wirepb.StreamState) workflow.StreamState {
	switch state {
	case wirepb.STREAM_STATE_IDLE:
		return workflow.StreamStateIdle
	case wirepb.STREAM_STATE_OPEN:
		return workflow.StreamStateOpen
	case wirepb.STREAM_STATE_BLOCKED:
		return workflow.StreamStateBlocked
	case wirepb.STREAM_STATE_CLOSED:
		return workflow.StreamStateClosed
	default:
		return workflow.StreamStateIdle
	}
}

func (m *Codec) nodeStreamMapFromPbNodeStreamList(pbMap map[string]*wirepb.StreamList, fragment *physical.Plan) (map[physical.Node][]*workflow.Stream, error) {
	result := make(map[physical.Node][]*workflow.Stream)

	// Build a map of node IDs to nodes from the fragment
	nodeByID := make(map[ulid.ULID]physical.Node)
	for node := range fragment.Graph().Nodes() {
		nodeByID[node.ID()] = node
	}

	for nodeIDStr, streamList := range pbMap {
		// Parse the node ID string back to ULID
		nodeID, err := ulid.Parse(nodeIDStr)
		if err != nil {
			return nil, fmt.Errorf("invalid node ID %q: %w", nodeIDStr, err)
		}

		// Look up the actual node
		node, ok := nodeByID[nodeID]
		if !ok {
			return nil, fmt.Errorf("node ID %q not found in fragment", nodeIDStr)
		}

		streams := make([]*workflow.Stream, len(streamList.Streams))
		for i, s := range streamList.Streams {
			streams[i] = &workflow.Stream{
				ULID:     ulid.ULID(s.Ulid),
				TenantID: s.TenantId,
			}
		}

		result[node] = streams
	}

	return result, nil
}

func (m *Codec) FrameToProto(from Frame) (*wirepb.Frame, error) {
	if from == nil {
		return nil, errors.New("nil frame")
	}

	f := &wirepb.Frame{}

	switch v := from.(type) {
	case AckFrame:
		f.Kind = &wirepb.Frame_Ack{
			Ack: &wirepb.AckFrame{Id: v.ID},
		}

	case NackFrame:
		var errStr string
		if v.Error != nil {
			errStr = v.Error.Error()
		}
		f.Kind = &wirepb.Frame_Nack{
			Nack: &wirepb.NackFrame{
				Id:    v.ID,
				Error: errStr,
			},
		}

	case DiscardFrame:
		f.Kind = &wirepb.Frame_Discard{
			Discard: &wirepb.DiscardFrame{Id: v.ID},
		}

	case MessageFrame:
		mf, err := m.MessageToProto(v.Message)
		if err != nil {
			return nil, err
		}
		mf.Id = v.ID
		f.Kind = &wirepb.Frame_Message{Message: mf}

	default:
		return nil, fmt.Errorf("unknown frame type: %T", v)
	}

	return f, nil
}

func (m *Codec) MessageToProto(from Message) (*wirepb.MessageFrame, error) {
	if from == nil {
		return nil, errors.New("nil message")
	}

	mf := &wirepb.MessageFrame{}

	switch v := from.(type) {
	case WorkerReadyMessage:
		mf.Kind = &wirepb.MessageFrame_WorkerReady{
			WorkerReady: &wirepb.WorkerReadyMessage{},
		}

	case TaskAssignMessage:
		task, err := m.TaskToProto(v.Task)
		if err != nil {
			return nil, err
		}

		streamStates := make(map[string]wirepb.StreamState)
		for id, state := range v.StreamStates {
			streamStates[id.String()] = m.streamStateToProto(state)
		}

		mf.Kind = &wirepb.MessageFrame_TaskAssign{
			TaskAssign: &wirepb.TaskAssignMessage{
				Task:         task,
				StreamStates: streamStates,
			},
		}

	case TaskCancelMessage:
		mf.Kind = &wirepb.MessageFrame_TaskCancel{
			TaskCancel: &wirepb.TaskCancelMessage{
				Id: ulidpb.ULID(v.ID),
			},
		}

	case TaskFlagMessage:
		mf.Kind = &wirepb.MessageFrame_TaskFlag{
			TaskFlag: &wirepb.TaskFlagMessage{
				Id:            ulidpb.ULID(v.ID),
				Interruptible: v.Interruptible,
			},
		}

	case TaskStatusMessage:
		status, err := m.taskStatusToProto(v.Status)
		if err != nil {
			return nil, err
		}
		mf.Kind = &wirepb.MessageFrame_TaskStatus{
			TaskStatus: &wirepb.TaskStatusMessage{
				Id:     ulidpb.ULID(v.ID),
				Status: *status,
			},
		}

	case StreamBindMessage:
		mf.Kind = &wirepb.MessageFrame_StreamBind{
			StreamBind: &wirepb.StreamBindMessage{
				StreamId: ulidpb.ULID(v.StreamID),
				Receiver: v.Receiver.String(),
			},
		}

	case StreamDataMessage:
		// Serialize Arrow record to bytes
		data, err := m.serializeArrowRecord(v.Data)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize arrow record: %w", err)
		}
		mf.Kind = &wirepb.MessageFrame_StreamData{
			StreamData: &wirepb.StreamDataMessage{
				StreamId: ulidpb.ULID(v.StreamID),
				Data:     data,
			},
		}

	case StreamStatusMessage:
		mf.Kind = &wirepb.MessageFrame_StreamStatus{
			StreamStatus: &wirepb.StreamStatusMessage{
				StreamId: ulidpb.ULID(v.StreamID),
				State:    m.streamStateToProto(v.State),
			},
		}

	default:
		return nil, fmt.Errorf("unknown message type: %T", v)
	}

	return mf, nil
}

func (m *Codec) TaskToProto(from *workflow.Task) (*wirepb.Task, error) {
	if from == nil {
		return nil, errors.New("nil task")
	}

	fragment := &physicalpb.Plan{}
	if err := fragment.UnmarshalPhysical(from.Fragment); err != nil {
		return nil, fmt.Errorf("failed to unmarshal fragment: %w", err)
	}

	sources, err := m.nodeStreamMapToPbNodeStreamList(from.Sources)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal sources: %w", err)
	}

	sinks, err := m.nodeStreamMapToPbNodeStreamList(from.Sinks)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal sinks: %w", err)
	}

	return &wirepb.Task{
		Ulid:     ulidpb.ULID(from.ULID),
		TenantId: from.TenantID,
		Fragment: fragment,
		Sources:  sources,
		Sinks:    sinks,
	}, nil
}

func (m *Codec) taskStatusToProto(from workflow.TaskStatus) (*wirepb.TaskStatus, error) {
	ts := &wirepb.TaskStatus{
		State: m.taskStateToProto(from.State),
	}

	if from.Error != nil {
		ts.Error = from.Error.Error()
	}

	return ts, nil
}

func (m *Codec) taskStateToProto(state workflow.TaskState) wirepb.TaskState {
	switch state {
	case workflow.TaskStateCreated:
		return wirepb.TASK_STATE_CREATED
	case workflow.TaskStatePending:
		return wirepb.TASK_STATE_PENDING
	case workflow.TaskStateRunning:
		return wirepb.TASK_STATE_RUNNING
	case workflow.TaskStateCompleted:
		return wirepb.TASK_STATE_COMPLETED
	case workflow.TaskStateCancelled:
		return wirepb.TASK_STATE_CANCELLED
	case workflow.TaskStateFailed:
		return wirepb.TASK_STATE_FAILED
	default:
		panic("failed to encode task state: invalid state")
	}
}

func (m *Codec) streamStateToProto(state workflow.StreamState) wirepb.StreamState {
	switch state {
	case workflow.StreamStateIdle:
		return wirepb.STREAM_STATE_IDLE
	case workflow.StreamStateOpen:
		return wirepb.STREAM_STATE_OPEN
	case workflow.StreamStateBlocked:
		return wirepb.STREAM_STATE_BLOCKED
	case workflow.StreamStateClosed:
		return wirepb.STREAM_STATE_CLOSED
	default:
		panic("failed to encode stream state: invalid state")
	}
}

func (m *Codec) nodeStreamMapToPbNodeStreamList(nodeMap map[physical.Node][]*workflow.Stream) (map[string]*wirepb.StreamList, error) {
	result := make(map[string]*wirepb.StreamList)

	for node, streams := range nodeMap {
		// Get the node ID
		nodeID := node.ID()
		nodeIDStr := nodeID.String()

		pbStreams := make([]*wirepb.Stream, len(streams))
		for i, s := range streams {
			pbStreams[i] = &wirepb.Stream{
				Ulid:     ulidpb.ULID(s.ULID),
				TenantId: s.TenantID,
			}
		}

		result[nodeIDStr] = &wirepb.StreamList{
			Streams: pbStreams,
		}
	}

	return result, nil
}

// serializeArrowRecord serializes an Arrow record to bytes using IPC format.
func (m *Codec) serializeArrowRecord(record arrow.Record) ([]byte, error) {
	if record == nil {
		return nil, errors.New("nil arrow record")
	}

	var buf bytes.Buffer
	writer := ipc.NewWriter(&buf,
		ipc.WithSchema(record.Schema()),
		ipc.WithAllocator(m.allocator),
	)
	defer writer.Close()

	if err := writer.Write(record); err != nil {
		return nil, err
	} else if err := writer.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// deserializeArrowRecord deserializes an Arrow record from bytes using IPC format.
func (m *Codec) deserializeArrowRecord(data []byte) (arrow.Record, error) {
	if len(data) == 0 {
		return nil, errors.New("empty arrow data")
	}

	reader, err := ipc.NewReader(
		bytes.NewReader(data),
		ipc.WithAllocator(m.allocator),
	)
	if err != nil {
		return nil, err
	}
	defer reader.Release()

	if !reader.Next() {
		if err := reader.Err(); err != nil {
			return nil, err
		}
		return nil, errors.New("no record in arrow data")
	}

	rec := reader.Record()
	rec.Retain()
	return rec, nil
}
