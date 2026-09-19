package wire

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/oklog/ulid/v2"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/proto/physicalpb"
	protoUlid "github.com/grafana/loki/v3/pkg/engine/internal/proto/ulid"
	"github.com/grafana/loki/v3/pkg/engine/internal/proto/wirepb"
	arrowcodec "github.com/grafana/loki/v3/pkg/engine/internal/scheduler/wire/arrow"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
	"github.com/grafana/loki/v3/pkg/xcap"
)

var DefaultFrameCodec = &protobufCodec{
	ArrowCodec: arrowcodec.DefaultArrowCodec,
}

// protobufCodec implements a protobuf-based codec for frames.
// Messages are length-prefixed: [uvarint length][protobuf payload]
type protobufCodec struct {
	*arrowcodec.ArrowCodec
}

// encode encodes a frame as protobuf and returns the on-wire bytes.
// Format: [uvarint length][protobuf payload].
//
// scratch is a reusable buffer whose backing array encode grows and reuses to
// avoid allocating a fresh length-prefix+payload buffer per frame. Callers pass
// the previous return value back in; because a connection serializes its sends
// under its write lock, the buffer is safe to reuse across that connection's
// sends. The returned slice aliases scratch's array and stays valid only until
// the next encode call on the same buffer.
func (c *protobufCodec) encode(frame Frame, m *Metrics, scratch []byte) []byte {
	mc := &metricCodec{protobufCodec: c, m: m}

	// Convert wire.Frame to protobuf.
	pbFrame, err := mc.frameToPbFrame(frame)
	if err != nil {
		panic(fmt.Errorf("failed to convert frame to protobuf: %w", err))
	}

	// Marshal to bytes.
	marshalStart := time.Now()
	payload, err := proto.Marshal(pbFrame)
	if err != nil {
		panic(fmt.Errorf("failed to marshal protobuf: %w", err))
	}
	mc.observeCodecStage(codecOperationEncode, codecStageProtobufMarshal, frame, time.Since(marshalStart))

	// Write the length prefix (uvarint) then the payload into the reusable
	// buffer.
	var prefix [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(prefix[:], uint64(len(payload)))
	buf := append(scratch[:0], prefix[:n]...)
	buf = append(buf, payload...)
	return buf
}

func readUvarint(r io.Reader) (length uint64, prefixBytes int, err error) {
	byteReader, ok := r.(io.ByteReader)
	if !ok {
		byteReader = &byteReaderAdapter{r: r}
	}

	countingReader := &countingByteReader{r: byteReader}
	length, err = binary.ReadUvarint(countingReader)
	return length, countingReader.n, err
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

type countingByteReader struct {
	r io.ByteReader
	n int
}

func (br *countingByteReader) ReadByte() (byte, error) {
	b, err := br.r.ReadByte()
	if err == nil {
		br.n++
	}
	return b, err
}

// metricCodec binds a [protobufCodec] to the [*Metrics] used for one encode or
// decode call. It is instantiated per call so the conversion helpers and
// codec-stage timing can read Metrics from a field instead of threading it
// through every signature. A nil m records nothing.
type metricCodec struct {
	*protobufCodec
	m *Metrics
}

func (c *metricCodec) observeCodecStage(operation codecOperation, stage codecStage, frame Frame, d time.Duration) {
	if c.m == nil || d < 0 {
		return
	}
	labels := labelsForFrame(frame, "")
	c.m.frameCodecStageSeconds.WithLabelValues(operation.String(), stage.String(), labels.frameType, labels.messageType).Observe(d.Seconds())
}

func (c *metricCodec) observeMessageCodecStage(operation codecOperation, stage codecStage, kind MessageKind, d time.Duration) {
	if c.m == nil || d < 0 {
		return
	}
	c.m.frameCodecStageSeconds.WithLabelValues(operation.String(), stage.String(), FrameKindMessage.String(), kind.String()).Observe(d.Seconds())
}

// decode reads and decodes the next frame from r, returning the frame and the
// number of on-wire bytes consumed (including the length prefix). It emits its
// own frame_receive_seconds read and deserialize phases; the receive path has
// no outcome dimension. decode is only reached over HTTP/2.
func (c *protobufCodec) decode(r io.Reader, m *Metrics) (Frame, int, error) {
	mc := &metricCodec{protobufCodec: c, m: m}

	readStart := time.Now()

	// Read length prefix (uvarint).
	length, prefixBytes, err := readUvarint(r)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read length prefix: %w", err)
	}

	// Read payload
	data := make([]byte, length)
	n, err := io.ReadFull(r, data)
	readDuration := time.Since(readStart)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read payload: %w", err)
	}
	if uint64(n) != length {
		return nil, 0, fmt.Errorf("incomplete read: read %d bytes, expected %d", n, length)
	}

	deserializeStart := time.Now()

	// Unmarshal protobuf.
	unmarshalStart := time.Now()
	pbFrame := &wirepb.Frame{}
	if err := proto.Unmarshal(data, pbFrame); err != nil {
		return nil, 0, fmt.Errorf("failed to unmarshal protobuf: %w", err)
	}
	protobufUnmarshalDuration := time.Since(unmarshalStart)

	// Convert protobuf to wire.Frame.
	frame, err := mc.frameFromPbFrame(pbFrame)
	deserializeDuration := time.Since(deserializeStart)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to convert protobuf to frame: %w", err)
	}
	mc.observeCodecStage(codecOperationDecode, codecStageProtobufUnmarshal, frame, protobufUnmarshalDuration)

	// Emit our own receive-side read and deserialize phases now that the frame
	// (and thus its labels) is known.
	receive := m.startFrameReceive(transportHTTP2, frame, sendModeInternal)
	receive.Observe(phaseReadBytes, readDuration)
	receive.Observe(phaseDeserialize, deserializeDuration)
	receive.Done()

	return frame, prefixBytes + n, nil
}

func (c *metricCodec) frameFromPbFrame(f *wirepb.Frame) (Frame, error) {

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

func (c *metricCodec) messageFromPbMessage(mf *wirepb.MessageFrame) (Message, error) {

	if mf == nil {
		return nil, errors.New("nil message frame")
	}

	switch k := mf.Kind.(type) {
	case *wirepb.MessageFrame_WorkerHello:
		return WorkerHelloMessage{}, nil

	case *wirepb.MessageFrame_WorkerReady:
		return WorkerReadyMessage{}, nil

	case *wirepb.MessageFrame_TaskAssign:
		task, err := c.taskFromPbTask(k.TaskAssign.Task)
		if err != nil {
			return nil, err
		}

		closedSourceIDs := make([]ulid.ULID, 0, len(k.TaskAssign.ClosedSourceIds))
		for _, idStr := range k.TaskAssign.ClosedSourceIds {
			id, err := ulid.Parse(idStr)
			if err != nil {
				return nil, fmt.Errorf("invalid closed source ID %q: %w", idStr, err)
			}
			closedSourceIDs = append(closedSourceIDs, id)
		}

		var metadata http.Header
		if len(k.TaskAssign.Metadata) > 0 {
			metadata = make(http.Header)
			httpgrpc.ToHeader(k.TaskAssign.Metadata, metadata)
		}

		return TaskAssignMessage{
			Task:            task,
			ClosedSourceIDs: closedSourceIDs,
			Metadata:        metadata,
		}, nil

	case *wirepb.MessageFrame_TaskCancel:
		return TaskCancelMessage{
			ID: ulid.ULID(k.TaskCancel.Id),
		}, nil

	case *wirepb.MessageFrame_TaskResult:
		result, err := c.taskResultFromPbTaskResult(&k.TaskResult.Result)
		if err != nil {
			return nil, err
		}

		return TaskResultMessage{
			ID:     ulid.ULID(k.TaskResult.Id),
			Result: result,
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
		arrowStart := time.Now()
		record, err := c.DeserializeArrowRecord(k.StreamData.Data)
		c.observeMessageCodecStage(codecOperationDecode, codecStageArrowDecode, MessageKindStreamData, time.Since(arrowStart))
		if err != nil {
			return nil, fmt.Errorf("failed to deserialize arrow record: %w", err)
		}
		return StreamDataMessage{
			StreamID: ulid.ULID(k.StreamData.StreamId),
			Data:     record,
		}, nil

	case *wirepb.MessageFrame_StreamClosed:
		return StreamClosedMessage{
			StreamID: ulid.ULID(k.StreamClosed.StreamId),
		}, nil

	default:
		return nil, fmt.Errorf("unknown message kind: %T", k)
	}
}

func (c *metricCodec) taskFromPbTask(t *wirepb.Task) (*workflow.Task, error) {

	if t == nil {
		return nil, fmt.Errorf("nil task")
	}

	planStart := time.Now()
	fragment, err := t.Fragment.MarshalPhysical()
	c.observeMessageCodecStage(codecOperationDecode, codecStageTaskAssignDecode, MessageKindTaskAssign, time.Since(planStart))
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

	cachedSources, err := c.cachedSourcesFromPb(t.CachedSources, fragment)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal cached sources: %w", err)
	}

	return &workflow.Task{
		ULID:          ulid.ULID(t.Ulid),
		TenantID:      t.TenantId,
		Fragment:      fragment,
		Sources:       sources,
		Sinks:         sinks,
		CachedSources: cachedSources,
	}, nil
}

func (c *protobufCodec) taskResultFromPbTaskResult(tr *wirepb.TaskResult) (workflow.TaskResult, error) {
	if tr == nil {
		return workflow.TaskResult{}, fmt.Errorf("nil task result")
	}

	outcome, err := c.taskOutcomeFromPbTaskOutcome(tr.Outcome)
	if err != nil {
		return workflow.TaskResult{}, err
	}

	result := workflow.TaskResult{Outcome: outcome}
	pbErr := tr.GetError()
	if pbErr != nil {
		result.Error = errors.New(pbErr.Description)
	}

	if captureData := tr.GetCapture(); len(captureData) > 0 {
		capture := &xcap.Capture{}
		if err := capture.UnmarshalBinary(captureData); err != nil {
			return workflow.TaskResult{}, fmt.Errorf("failed to unmarshal capture: %w", err)
		}

		result.Capture = capture
	}

	return result, nil
}

func (c *protobufCodec) taskOutcomeFromPbTaskOutcome(outcome wirepb.TaskOutcome) (workflow.TaskOutcome, error) {
	switch outcome {
	case wirepb.TASK_OUTCOME_COMPLETED:
		return workflow.TaskOutcomeCompleted, nil
	case wirepb.TASK_OUTCOME_CANCELLED:
		return workflow.TaskOutcomeCancelled, nil
	case wirepb.TASK_OUTCOME_FAILED:
		return workflow.TaskOutcomeFailed, nil
	default:
		return 0, fmt.Errorf("task outcome %v is unknown", outcome)
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
				ULID: ulid.ULID(s.Ulid),
			}
		}

		result[node] = streams
	}

	return result, nil
}

func (c *protobufCodec) cachedSourcesFromPb(pbMap map[string]*wirepb.CachedSources, fragment *physical.Plan) (map[physical.Node]workflow.CachedSources, error) {
	if len(pbMap) == 0 {
		return nil, nil
	}

	nodeByID := make(map[ulid.ULID]physical.Node)
	for node := range fragment.Graph().Nodes() {
		nodeByID[node.ID()] = node
	}

	result := make(map[physical.Node]workflow.CachedSources, len(pbMap))
	for nodeIDStr, cs := range pbMap {
		nodeID, err := ulid.Parse(nodeIDStr)
		if err != nil {
			return nil, fmt.Errorf("invalid cached-source node ID %q: %w", nodeIDStr, err)
		}
		node, ok := nodeByID[nodeID]
		if !ok {
			return nil, fmt.Errorf("cached-source node ID %q not found in fragment", nodeIDStr)
		}
		if cs == nil {
			return nil, fmt.Errorf("cached-source entry for node ID %q is nil", nodeIDStr)
		}
		result[node] = cs.CachedSource
	}
	return result, nil
}

func (c *metricCodec) frameToPbFrame(from Frame) (*wirepb.Frame, error) {

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
		panic(fmt.Errorf("unknown frame type: %T", v))
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

func (c *metricCodec) messageToPbMessage(from Message) (*wirepb.MessageFrame, error) {

	if from == nil {
		return nil, errors.New("nil message")
	}

	mf := &wirepb.MessageFrame{}

	switch v := from.(type) {
	case WorkerHelloMessage:
		mf.Kind = &wirepb.MessageFrame_WorkerHello{
			WorkerHello: &wirepb.WorkerHelloMessage{},
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

		closedSourceIDs := make([]string, len(v.ClosedSourceIDs))
		for i, id := range v.ClosedSourceIDs {
			closedSourceIDs[i] = id.String()
		}

		mf.Kind = &wirepb.MessageFrame_TaskAssign{
			TaskAssign: &wirepb.TaskAssignMessage{
				Task:            task,
				ClosedSourceIds: closedSourceIDs,
				Metadata:        httpgrpc.FromHeader(v.Metadata),
			},
		}

	case TaskCancelMessage:
		mf.Kind = &wirepb.MessageFrame_TaskCancel{
			TaskCancel: &wirepb.TaskCancelMessage{
				Id: protoUlid.ULID(v.ID),
			},
		}

	case TaskResultMessage:
		result, err := c.taskResultToPbTaskResult(v.Result)
		if err != nil {
			return nil, err
		}

		mf.Kind = &wirepb.MessageFrame_TaskResult{
			TaskResult: &wirepb.TaskResultMessage{
				Id:     protoUlid.ULID(v.ID),
				Result: *result,
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
		// Serialize Arrow record to bytes.
		arrowStart := time.Now()
		data, err := c.SerializeArrowRecord(v.Data)
		c.observeMessageCodecStage(codecOperationEncode, codecStageArrowEncode, MessageKindStreamData, time.Since(arrowStart))
		if err != nil {
			return nil, fmt.Errorf("failed to serialize arrow record: %w", err)
		}
		mf.Kind = &wirepb.MessageFrame_StreamData{
			StreamData: &wirepb.StreamDataMessage{
				StreamId: protoUlid.ULID(v.StreamID),
				Data:     data,
			},
		}

	case StreamClosedMessage:
		mf.Kind = &wirepb.MessageFrame_StreamClosed{
			StreamClosed: &wirepb.StreamClosedMessage{
				StreamId: protoUlid.ULID(v.StreamID),
			},
		}

	default:
		panic(fmt.Errorf("unknown message type: %T", v))
	}

	return mf, nil
}

func (c *metricCodec) taskToPbTask(from *workflow.Task) (*wirepb.Task, error) {

	if from == nil {
		return nil, errors.New("nil task")
	}

	fragment := &physicalpb.Plan{}
	planStart := time.Now()
	if err := fragment.UnmarshalPhysical(from.Fragment); err != nil {
		c.observeMessageCodecStage(codecOperationEncode, codecStageTaskAssignEncode, MessageKindTaskAssign, time.Since(planStart))
		return nil, fmt.Errorf("failed to unmarshal fragment: %w", err)
	}
	c.observeMessageCodecStage(codecOperationEncode, codecStageTaskAssignEncode, MessageKindTaskAssign, time.Since(planStart))

	sources, err := c.nodeStreamMapToPbNodeStreamList(from.Sources)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal sources: %w", err)
	}

	sinks, err := c.nodeStreamMapToPbNodeStreamList(from.Sinks)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal sinks: %w", err)
	}

	cachedSources := c.cachedSourcesToPb(from.CachedSources)

	return &wirepb.Task{
		Ulid:          protoUlid.ULID(from.ULID),
		TenantId:      from.TenantID,
		Fragment:      fragment,
		Sources:       sources,
		Sinks:         sinks,
		CachedSources: cachedSources,
	}, nil
}

func (c *protobufCodec) taskResultToPbTaskResult(from workflow.TaskResult) (*wirepb.TaskResult, error) {
	tr := &wirepb.TaskResult{
		Outcome: c.taskOutcomeToPbTaskOutcome(from.Outcome),
	}

	if from.Error != nil {
		tr.Error = &wirepb.TaskError{Description: from.Error.Error()}
	}

	if from.Capture != nil {
		captureData, err := from.Capture.MarshalBinary()
		if err != nil {
			return nil, fmt.Errorf("failed to marshal capture: %w", err)
		}

		tr.Capture = captureData
	}

	return tr, nil
}

func (c *protobufCodec) taskOutcomeToPbTaskOutcome(outcome workflow.TaskOutcome) wirepb.TaskOutcome {
	switch outcome {
	case workflow.TaskOutcomeCompleted:
		return wirepb.TASK_OUTCOME_COMPLETED
	case workflow.TaskOutcomeCancelled:
		return wirepb.TASK_OUTCOME_CANCELLED
	case workflow.TaskOutcomeFailed:
		return wirepb.TASK_OUTCOME_FAILED
	default:
		return wirepb.TASK_OUTCOME_UNSPECIFIED
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
				Ulid: protoUlid.ULID(s.ULID),
			}
		}

		result[nodeIDStr] = &wirepb.StreamList{
			Streams: pbStreams,
		}
	}

	return result, nil
}

func (c *protobufCodec) cachedSourcesToPb(srcs map[physical.Node]workflow.CachedSources) map[string]*wirepb.CachedSources {
	if len(srcs) == 0 {
		return nil
	}
	result := make(map[string]*wirepb.CachedSources, len(srcs))
	for node, cs := range srcs {
		result[node.ID().String()] = &wirepb.CachedSources{CachedSource: cs}
	}
	return result
}
