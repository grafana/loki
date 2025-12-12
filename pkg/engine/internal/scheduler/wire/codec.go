package wire

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/gogo/protobuf/proto"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/oklog/ulid/v2"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/proto/physicalpb"
	protoUlid "github.com/grafana/loki/v3/pkg/engine/internal/proto/ulid"
	"github.com/grafana/loki/v3/pkg/engine/internal/proto/wirepb"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
	"github.com/grafana/loki/v3/pkg/xcap"
)

var defaultFrameCodec = &protobufCodec{
	allocator: memory.DefaultAllocator,
}

// protobufCodec implements a protobuf-based codec for frames.
// Messages are length-prefixed: [uvarint length][protobuf payload]
type protobufCodec struct {
	allocator memory.Allocator
}

// byteReaderAdapter adapts an io.Reader to io.ByteReader without buffering.
// This is used to read uvarint length prefixes byte-by-byte without
// consuming extra data that might be needed for subsequent reads.
type byteReaderAdapter struct {
	r io.Reader
}

func (br *byteReaderAdapter) ReadByte() (byte, error) {
	var b [1]byte
	_, err := io.ReadFull(br.r, b[:])
	return b[0], err
}

// EncodeTo encodes a frame as protobuf and writes it to the writer.
// Format: [uvarint length][protobuf payload]
func (c *protobufCodec) EncodeTo(w io.Writer, frame Frame) error {
	// Convert wire.Frame to protobuf
	pbFrame, err := c.frameToPbFrame(frame)
	if err != nil {
		return fmt.Errorf("failed to convert frame to protobuf: %w", err)
	}

	// Marshal to bytes
	data, err := proto.Marshal(pbFrame)
	if err != nil {
		return fmt.Errorf("failed to marshal protobuf: %w", err)
	}

	// Write length prefix (uvarint)
	length := uint64(len(data))
	buf := make([]byte, binary.MaxVarintLen64)
	n := binary.PutUvarint(buf, length)
	if _, err := w.Write(buf[:n]); err != nil {
		return fmt.Errorf("failed to write length prefix: %w", err)
	}

	// Write payload
	written, err := w.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write payload: %w", err)
	}
	if written != len(data) {
		return fmt.Errorf("incomplete write: wrote %d bytes, expected %d", written, len(data))
	}

	return nil
}

// DecodeFrom reads and decodes a frame from the bound reader.
// Format: [uvarint length][protobuf payload]
func (c *protobufCodec) DecodeFrom(r io.Reader) (Frame, error) {
	// Read length prefix (uvarint)
	// binary.ReadUvarint requires a ByteReader, so we wrap if needed
	byteReader, ok := r.(io.ByteReader)
	if !ok {
		byteReader = &byteReaderAdapter{r: r}
	}

	length, err := binary.ReadUvarint(byteReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read length prefix: %w", err)
	}

	// Read payload
	data := make([]byte, length)
	n, err := io.ReadFull(r, data)
	if err != nil {
		return nil, fmt.Errorf("failed to read payload: %w", err)
	}
	if uint64(n) != length {
		return nil, fmt.Errorf("incomplete read: read %d bytes, expected %d", n, length)
	}

	// Unmarshal protobuf
	pbFrame := &wirepb.Frame{}
	if err := proto.Unmarshal(data, pbFrame); err != nil {
		return nil, fmt.Errorf("failed to unmarshal protobuf: %w", err)
	}

	// Convert protobuf to wire.Frame
	frame, err := c.frameFromPbFrame(pbFrame)
	if err != nil {
		return nil, fmt.Errorf("failed to convert protobuf to frame: %w", err)
	}

	return frame, nil
}

func (c *protobufCodec) frameFromPbFrame(f *wirepb.Frame) (Frame, error) {
	if f == nil {
		return nil, errors.New("nil frame")
	}

	switch k := f.Kind.(type) {
	case *wirepb.Frame_Ack:
		return AckFrame{ID: k.Ack.Id}, nil

	case *wirepb.Frame_Nack:
		return NackFrame{
			ID:    k.Nack.Id,
			Error: c.errorFromPb(k.Nack.Error),
		}, nil

	case *wirepb.Frame_Discard:
		return DiscardFrame{ID: k.Discard.Id}, nil

	case *wirepb.Frame_Message:
		msg, err := c.messageFromPbMessage(k.Message)
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

func (c *protobufCodec) errorFromPb(errPb *wirepb.Error) *Error {
	if errPb == nil {
		return nil
	}

	return &Error{
		Code:    errPb.Code,
		Message: errPb.Message,
	}
}

func (c *protobufCodec) messageFromPbMessage(mf *wirepb.MessageFrame) (Message, error) {
	if mf == nil {
		return nil, errors.New("nil message frame")
	}

	switch k := mf.Kind.(type) {
	case *wirepb.MessageFrame_WorkerHello:
		return WorkerHelloMessage{Threads: int(k.WorkerHello.Threads)}, nil

	case *wirepb.MessageFrame_WorkerSubscribe:
		return WorkerSubscribeMessage{}, nil

	case *wirepb.MessageFrame_WorkerReady:
		return WorkerReadyMessage{}, nil

	case *wirepb.MessageFrame_TaskAssign:
		task, err := c.taskFromPbTask(k.TaskAssign.Task)
		if err != nil {
			return nil, err
		}

		streamStates := make(map[ulid.ULID]workflow.StreamState)
		for idStr, statePb := range k.TaskAssign.StreamStates {
			id, err := ulid.Parse(idStr)
			if err != nil {
				return nil, fmt.Errorf("invalid stream ID %q: %w", idStr, err)
			}
			state, err := c.streamStateFromPbStreamState(statePb)
			if err != nil {
				return nil, fmt.Errorf("stream state from pb stream state (%s): %w", idStr, err)
			}
			streamStates[id] = state
		}

		var metadata http.Header
		if len(k.TaskAssign.Metadata) > 0 {
			metadata = make(http.Header)
			httpgrpc.ToHeader(k.TaskAssign.Metadata, metadata)
		}

		return TaskAssignMessage{
			Task:         task,
			StreamStates: streamStates,
			Metadata:     metadata,
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
		status, err := c.taskStatusFromPbTaskStatus(&k.TaskStatus.Status)
		if err != nil {
			return nil, err
		}

		return TaskStatusMessage{
			ID:     ulid.ULID(k.TaskStatus.Id),
			Status: status,
		}, nil

	case *wirepb.MessageFrame_StreamBind:
		addr, err := addrPortStrToAddr(k.StreamBind.Receiver)
		if err != nil {
			return nil, fmt.Errorf("invalid receiver address %s: %w", k.StreamBind.Receiver, err)
		}
		return StreamBindMessage{
			StreamID: ulid.ULID(k.StreamBind.StreamId),
			Receiver: addr,
		}, nil

	case *wirepb.MessageFrame_StreamData:
		record, err := c.deserializeArrowRecord(k.StreamData.Data)
		if err != nil {
			return nil, fmt.Errorf("failed to deserialize arrow record: %w", err)
		}
		return StreamDataMessage{
			StreamID: ulid.ULID(k.StreamData.StreamId),
			Data:     record,
		}, nil

	case *wirepb.MessageFrame_StreamStatus:
		streamState, err := c.streamStateFromPbStreamState(k.StreamStatus.State)
		if err != nil {
			return nil, fmt.Errorf("stream state from pb stream state: %w", err)
		}
		return StreamStatusMessage{
			StreamID: ulid.ULID(k.StreamStatus.StreamId),
			State:    streamState,
		}, nil

	default:
		return nil, fmt.Errorf("unknown message kind: %T", k)
	}
}

func (c *protobufCodec) taskFromPbTask(t *wirepb.Task) (*workflow.Task, error) {
	if t == nil {
		return nil, fmt.Errorf("nil task")
	}

	fragment, err := t.Fragment.MarshalPhysical()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal fragment: %w", err)
	}

	sources, err := c.nodeStreamMapFromPbNodeStreamList(t.Sources, fragment)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal sources: %w", err)
	}

	sinks, err := c.nodeStreamMapFromPbNodeStreamList(t.Sinks, fragment)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal sinks: %w", err)
	}

	return &workflow.Task{
		ULID:     ulid.ULID(t.Ulid),
		TenantID: t.TenantId,
		Fragment: fragment,
		Sources:  sources,
		Sinks:    sinks,
		MaxTimeRange: physical.TimeRange{
			Start: t.MaxTimeRange.Start,
			End:   t.MaxTimeRange.End,
		},
	}, nil
}

func (c *protobufCodec) taskStatusFromPbTaskStatus(ts *wirepb.TaskStatus) (workflow.TaskStatus, error) {
	if ts == nil {
		return workflow.TaskStatus{}, fmt.Errorf("nil task status")
	}

	state, err := c.taskStateFromPbTaskState(ts.State)
	if err != nil {
		return workflow.TaskStatus{}, err
	}

	status := workflow.TaskStatus{State: state}
	pbErr := ts.GetError()
	if pbErr != nil {
		status.Error = errors.New(pbErr.Description)
	}

	if captureData := ts.GetCapture(); len(captureData) > 0 {
		capture := &xcap.Capture{}
		if err := capture.UnmarshalBinary(captureData); err != nil {
			return workflow.TaskStatus{}, fmt.Errorf("failed to unmarshal capture: %w", err)
		}

		status.Capture = capture
	}

	if ts.ContributingTimeRange != nil {
		status.ContributingTimeRange = workflow.ContributingTimeRange{
			Timestamp: ts.ContributingTimeRange.Timestamp,
			LessThan:  ts.ContributingTimeRange.LessThan,
		}
	}

	return status, nil
}

func (c *protobufCodec) taskStateFromPbTaskState(state wirepb.TaskState) (workflow.TaskState, error) {
	switch state {
	case wirepb.TASK_STATE_CREATED:
		return workflow.TaskStateCreated, nil
	case wirepb.TASK_STATE_PENDING:
		return workflow.TaskStatePending, nil
	case wirepb.TASK_STATE_RUNNING:
		return workflow.TaskStateRunning, nil
	case wirepb.TASK_STATE_COMPLETED:
		return workflow.TaskStateCompleted, nil
	case wirepb.TASK_STATE_CANCELLED:
		return workflow.TaskStateCancelled, nil
	case wirepb.TASK_STATE_FAILED:
		return workflow.TaskStateFailed, nil
	default:
		return workflow.TaskStateCancelled, fmt.Errorf("task state %v is unknown", state)
	}
}

func (c *protobufCodec) streamStateFromPbStreamState(state wirepb.StreamState) (workflow.StreamState, error) {
	switch state {
	case wirepb.STREAM_STATE_IDLE:
		return workflow.StreamStateIdle, nil
	case wirepb.STREAM_STATE_OPEN:
		return workflow.StreamStateOpen, nil
	case wirepb.STREAM_STATE_BLOCKED:
		return workflow.StreamStateBlocked, nil
	case wirepb.STREAM_STATE_CLOSED:
		return workflow.StreamStateClosed, nil
	default:
		return workflow.StreamStateIdle, fmt.Errorf("stream state %v is unknown", state)
	}
}

func (c *protobufCodec) nodeStreamMapFromPbNodeStreamList(pbMap map[string]*wirepb.StreamList, fragment *physical.Plan) (map[physical.Node][]*workflow.Stream, error) {
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

func (c *protobufCodec) frameToPbFrame(from Frame) (*wirepb.Frame, error) {
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
		f.Kind = &wirepb.Frame_Nack{
			Nack: &wirepb.NackFrame{
				Id:    v.ID,
				Error: c.errorToPb(v.Error),
			},
		}

	case DiscardFrame:
		f.Kind = &wirepb.Frame_Discard{
			Discard: &wirepb.DiscardFrame{Id: v.ID},
		}

	case MessageFrame:
		mf, err := c.messageToPbMessage(v.Message)
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

func (c *protobufCodec) errorToPb(e *Error) *wirepb.Error {
	if e == nil {
		return nil
	}

	return &wirepb.Error{
		Code:    e.Code,
		Message: e.Message,
	}
}

func (c *protobufCodec) messageToPbMessage(from Message) (*wirepb.MessageFrame, error) {
	if from == nil {
		return nil, errors.New("nil message")
	}

	mf := &wirepb.MessageFrame{}

	switch v := from.(type) {
	case WorkerHelloMessage:
		mf.Kind = &wirepb.MessageFrame_WorkerHello{
			WorkerHello: &wirepb.WorkerHelloMessage{Threads: uint64(v.Threads)},
		}

	case WorkerSubscribeMessage:
		mf.Kind = &wirepb.MessageFrame_WorkerSubscribe{
			WorkerSubscribe: &wirepb.WorkerSubscribeMessage{},
		}

	case WorkerReadyMessage:
		mf.Kind = &wirepb.MessageFrame_WorkerReady{
			WorkerReady: &wirepb.WorkerReadyMessage{},
		}

	case TaskAssignMessage:
		task, err := c.taskToPbTask(v.Task)
		if err != nil {
			return nil, err
		}

		streamStates := make(map[string]wirepb.StreamState)
		for id, state := range v.StreamStates {
			streamStates[id.String()] = c.streamStateToPbStreamState(state)
		}

		mf.Kind = &wirepb.MessageFrame_TaskAssign{
			TaskAssign: &wirepb.TaskAssignMessage{
				Task:         task,
				StreamStates: streamStates,
				Metadata:     httpgrpc.FromHeader(v.Metadata),
			},
		}

	case TaskCancelMessage:
		mf.Kind = &wirepb.MessageFrame_TaskCancel{
			TaskCancel: &wirepb.TaskCancelMessage{
				Id: protoUlid.ULID(v.ID),
			},
		}

	case TaskFlagMessage:
		mf.Kind = &wirepb.MessageFrame_TaskFlag{
			TaskFlag: &wirepb.TaskFlagMessage{
				Id:            protoUlid.ULID(v.ID),
				Interruptible: v.Interruptible,
			},
		}

	case TaskStatusMessage:
		status, err := c.taskStatusToPbTaskStatus(v.Status)
		if err != nil {
			return nil, err
		}

		mf.Kind = &wirepb.MessageFrame_TaskStatus{
			TaskStatus: &wirepb.TaskStatusMessage{
				Id:     protoUlid.ULID(v.ID),
				Status: *status,
			},
		}

	case StreamBindMessage:
		mf.Kind = &wirepb.MessageFrame_StreamBind{
			StreamBind: &wirepb.StreamBindMessage{
				StreamId: protoUlid.ULID(v.StreamID),
				Receiver: v.Receiver.String(),
			},
		}

	case StreamDataMessage:
		// Serialize Arrow record to bytes
		data, err := c.serializeArrowRecord(v.Data)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize arrow record: %w", err)
		}
		mf.Kind = &wirepb.MessageFrame_StreamData{
			StreamData: &wirepb.StreamDataMessage{
				StreamId: protoUlid.ULID(v.StreamID),
				Data:     data,
			},
		}

	case StreamStatusMessage:
		mf.Kind = &wirepb.MessageFrame_StreamStatus{
			StreamStatus: &wirepb.StreamStatusMessage{
				StreamId: protoUlid.ULID(v.StreamID),
				State:    c.streamStateToPbStreamState(v.State),
			},
		}

	default:
		return nil, fmt.Errorf("unknown message type: %T", v)
	}

	return mf, nil
}

func (c *protobufCodec) taskToPbTask(from *workflow.Task) (*wirepb.Task, error) {
	if from == nil {
		return nil, errors.New("nil task")
	}

	fragment := &physicalpb.Plan{}
	if err := fragment.UnmarshalPhysical(from.Fragment); err != nil {
		return nil, fmt.Errorf("failed to unmarshal fragment: %w", err)
	}

	sources, err := c.nodeStreamMapToPbNodeStreamList(from.Sources)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal sources: %w", err)
	}

	sinks, err := c.nodeStreamMapToPbNodeStreamList(from.Sinks)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal sinks: %w", err)
	}

	return &wirepb.Task{
		Ulid:     protoUlid.ULID(from.ULID),
		TenantId: from.TenantID,
		Fragment: fragment,
		Sources:  sources,
		Sinks:    sinks,
		MaxTimeRange: &physicalpb.TimeRange{
			Start: from.MaxTimeRange.Start,
			End:   from.MaxTimeRange.End,
		},
	}, nil
}

func (c *protobufCodec) taskStatusToPbTaskStatus(from workflow.TaskStatus) (*wirepb.TaskStatus, error) {
	ts := &wirepb.TaskStatus{
		State: c.taskStateToPbTaskState(from.State),
		ContributingTimeRange: &wirepb.ContributingTimeRange{
			Timestamp: from.ContributingTimeRange.Timestamp,
			LessThan:  from.ContributingTimeRange.LessThan,
		},
	}

	if from.Error != nil {
		ts.Error = &wirepb.TaskError{Description: from.Error.Error()}
	}

	if from.Capture != nil {
		captureData, err := from.Capture.MarshalBinary()
		if err != nil {
			return nil, fmt.Errorf("failed to marshal capture: %w", err)
		}

		ts.Capture = captureData
	}

	return ts, nil
}

func (c *protobufCodec) taskStateToPbTaskState(state workflow.TaskState) wirepb.TaskState {
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
		return wirepb.TASK_STATE_INVALID
	}
}

func (c *protobufCodec) streamStateToPbStreamState(state workflow.StreamState) wirepb.StreamState {
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
		return wirepb.STREAM_STATE_INVALID
	}
}

func (c *protobufCodec) nodeStreamMapToPbNodeStreamList(nodeMap map[physical.Node][]*workflow.Stream) (map[string]*wirepb.StreamList, error) {
	result := make(map[string]*wirepb.StreamList)

	for node, streams := range nodeMap {
		// Get the node ID
		nodeID := node.ID()
		nodeIDStr := nodeID.String()

		pbStreams := make([]*wirepb.Stream, len(streams))
		for i, s := range streams {
			pbStreams[i] = &wirepb.Stream{
				Ulid:     protoUlid.ULID(s.ULID),
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
func (c *protobufCodec) serializeArrowRecord(record arrow.RecordBatch) ([]byte, error) {
	if record == nil {
		return nil, errors.New("nil arrow record")
	}

	var buf bytes.Buffer
	writer := ipc.NewWriter(&buf,
		ipc.WithSchema(record.Schema()),
		ipc.WithAllocator(c.allocator),
	)
	defer writer.Close()

	if err := writer.Write(record); err != nil {
		return nil, err
	}

	if err := writer.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// deserializeArrowRecord deserializes an Arrow record from bytes using IPC format.
func (c *protobufCodec) deserializeArrowRecord(data []byte) (arrow.RecordBatch, error) {
	if len(data) == 0 {
		return nil, errors.New("empty arrow data")
	}

	reader, err := ipc.NewReader(
		bytes.NewReader(data),
		ipc.WithAllocator(c.allocator),
	)
	if err != nil {
		return nil, err
	}

	if !reader.Next() {
		if err := reader.Err(); err != nil {
			return nil, err
		}
		return nil, errors.New("no record in arrow data")
	}

	rec := reader.RecordBatch()
	return rec, nil
}
