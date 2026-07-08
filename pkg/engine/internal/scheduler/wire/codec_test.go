package wire

import (
	"bytes"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
)

func TestProtobufCodec_Frames(t *testing.T) {
	tests := map[string]struct {
		frame Frame
	}{
		"AckFrame": {
			frame: AckFrame{ID: 42},
		},
		"NackFrame without error": {
			frame: NackFrame{ID: 43},
		},
		"NackFrame with error": {
			frame: NackFrame{
				ID:    44,
				Error: Errorf(http.StatusInternalServerError, "test error"),
			},
		},
		"DiscardFrame": {
			frame: DiscardFrame{ID: 45},
		},
	}

	codec := DefaultFrameCodec
	mc := &metricCodec{protobufCodec: codec}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			pbFrame, err := mc.frameToPbFrame(tt.frame)
			require.NoError(t, err)

			actualFrame, err := mc.frameFromPbFrame(pbFrame)
			require.NoError(t, err)

			assert.Equal(t, tt.frame, actualFrame)
		})
	}
}

func TestProtobufCodec_Messages(t *testing.T) {
	taskULID := ulid.Make()
	streamULID := ulid.Make()
	addrPort, err := netip.ParseAddrPort("192.168.0.1:12345")
	require.NoError(t, err)
	addr := net.TCPAddrFromAddrPort(addrPort)

	tests := map[string]struct {
		message Message
	}{
		"WorkerHelloMessage": {
			message: WorkerHelloMessage{Threads: 4},
		},
		"WorkerSubscribeMessage": {
			message: WorkerSubscribeMessage{},
		},
		"WorkerReadyMessage": {
			message: WorkerReadyMessage{},
		},
		"TaskAssignMessage without StreamStates": {
			message: TaskAssignMessage{
				Task: &workflow.Task{
					ULID:     taskULID,
					TenantID: "test-tenant",
					Fragment: &physical.Plan{},
					Sources:  map[physical.Node][]*workflow.Stream{},
					Sinks:    map[physical.Node][]*workflow.Stream{},
				},
				StreamStates: map[ulid.ULID]workflow.StreamState{},
			},
		},
		"TaskAssignMessage with StreamStates": {
			message: TaskAssignMessage{
				Task: &workflow.Task{
					ULID:     taskULID,
					TenantID: "test-tenant",
					Fragment: &physical.Plan{},
					Sources:  map[physical.Node][]*workflow.Stream{},
					Sinks:    map[physical.Node][]*workflow.Stream{},
				},
				StreamStates: map[ulid.ULID]workflow.StreamState{
					streamULID: workflow.StreamStateOpen,
				},
			},
		},
		"TaskCancelMessage": {
			message: TaskCancelMessage{ID: taskULID},
		},
		"TaskFlagMessage not interruptible": {
			message: TaskFlagMessage{
				ID:            taskULID,
				Interruptible: false,
			},
		},
		"TaskFlagMessage interruptible": {
			message: TaskFlagMessage{
				ID:            taskULID,
				Interruptible: true,
			},
		},
		"TaskStatusMessage with Created state": {
			message: TaskStatusMessage{
				ID: taskULID,
				Status: workflow.TaskStatus{
					State: workflow.TaskStateCreated,
				},
			},
		},
		"TaskStatusMessage with Running state": {
			message: TaskStatusMessage{
				ID: taskULID,
				Status: workflow.TaskStatus{
					State: workflow.TaskStateRunning,
				},
			},
		},
		"TaskStatusMessage with Running state and ContributingTimeRange": {
			message: TaskStatusMessage{
				ID: taskULID,
				Status: workflow.TaskStatus{
					State: workflow.TaskStateRunning,
					ContributingTimeRange: workflow.ContributingTimeRange{
						Timestamp: time.Now().Add(-time.Minute),
						LessThan:  true,
					},
				},
			},
		},
		"TaskStatusMessage with Completed state": {
			message: TaskStatusMessage{
				ID: taskULID,
				Status: workflow.TaskStatus{
					State: workflow.TaskStateCompleted,
				},
			},
		},
		"TaskStatusMessage with Failed state and error": {
			message: TaskStatusMessage{
				ID: taskULID,
				Status: workflow.TaskStatus{
					State: workflow.TaskStateFailed,
					Error: errors.New("task failed"),
				},
			},
		},
		"StreamBindMessage": {
			message: StreamBindMessage{
				StreamID: streamULID,
				Receiver: addr,
			},
		},
		"StreamStatusMessage with Idle state": {
			message: StreamStatusMessage{
				StreamID: streamULID,
				State:    workflow.StreamStateIdle,
			},
		},
		"StreamStatusMessage with Open state": {
			message: StreamStatusMessage{
				StreamID: streamULID,
				State:    workflow.StreamStateOpen,
			},
		},
		"StreamStatusMessage with Blocked state": {
			message: StreamStatusMessage{
				StreamID: streamULID,
				State:    workflow.StreamStateBlocked,
			},
		},
		"StreamStatusMessage with Closed state": {
			message: StreamStatusMessage{
				StreamID: streamULID,
				State:    workflow.StreamStateClosed,
			},
		},
	}

	codec := DefaultFrameCodec
	mc := &metricCodec{protobufCodec: codec}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			frame := MessageFrame{
				ID:      100,
				Message: tt.message,
			}

			pbFrame, err := mc.frameToPbFrame(frame)
			require.NoError(t, err)

			actualFrame, err := mc.frameFromPbFrame(pbFrame)
			require.NoError(t, err)

			assert.Equal(t, frame, actualFrame)
		})
	}
}

func TestProtobufCodec_StreamDataMessage(t *testing.T) {
	streamULID := ulid.Make()
	codec := DefaultFrameCodec
	mc := &metricCodec{protobufCodec: codec}

	originalRecord := createTestArrowRecord()

	message := StreamDataMessage{
		StreamID: streamULID,
		Data:     originalRecord,
	}

	frame := MessageFrame{
		ID:      100,
		Message: message,
	}

	pbFrame, err := mc.frameToPbFrame(frame)
	require.NoError(t, err)

	actualFrame, err := mc.frameFromPbFrame(pbFrame)
	require.NoError(t, err)

	actualMessage := actualFrame.(MessageFrame).Message.(StreamDataMessage)

	assert.Equal(t, frame.ID, actualFrame.(MessageFrame).ID)
	assert.Equal(t, streamULID, actualMessage.StreamID)

	assert.NotNil(t, actualMessage.Data)
	assert.True(t, originalRecord.Schema().Equal(actualMessage.Data.Schema()))
	assert.Equal(t, originalRecord.NumRows(), actualMessage.Data.NumRows())
	assert.Equal(t, originalRecord.NumCols(), actualMessage.Data.NumCols())
}

func TestProtobufCodec_Metrics(t *testing.T) {
	codec := DefaultFrameCodec
	metrics := NewMetrics()

	streamData := MessageFrame{
		ID: 1,
		Message: StreamDataMessage{
			StreamID: ulid.Make(),
			Data:     createTestArrowRecord(),
		},
	}
	streamDataBytes := codec.encode(streamData, metrics, nil)
	decodedStreamData, size, err := codec.decode(bytes.NewReader(streamDataBytes), metrics)
	require.NoError(t, err)
	require.Equal(t, len(streamDataBytes), size)
	metrics.observeFrameBytes(directionIncoming, decodedStreamData, sendModeInternal, size)

	taskAssign := MessageFrame{
		ID: 2,
		Message: TaskAssignMessage{
			Task: &workflow.Task{
				ULID:     ulid.Make(),
				TenantID: "test-tenant",
				Fragment: &physical.Plan{},
				Sources:  map[physical.Node][]*workflow.Stream{},
				Sinks:    map[physical.Node][]*workflow.Stream{},
			},
			StreamStates: map[ulid.ULID]workflow.StreamState{},
		},
	}
	taskAssignBytes := codec.encode(taskAssign, metrics, nil)
	decodedTaskAssign, size, err := codec.decode(bytes.NewReader(taskAssignBytes), metrics)
	require.NoError(t, err)
	require.Equal(t, len(taskAssignBytes), size)
	metrics.observeFrameBytes(directionOutgoing, decodedTaskAssign, sendModeSync, size)

	for _, tc := range []struct {
		operation   codecOperation
		stage       codecStage
		messageType string
	}{
		{codecOperationEncode, codecStageArrowEncode, MessageKindStreamData.String()},
		{codecOperationDecode, codecStageArrowDecode, MessageKindStreamData.String()},
		{codecOperationEncode, codecStageProtobufMarshal, MessageKindStreamData.String()},
		{codecOperationDecode, codecStageProtobufUnmarshal, MessageKindStreamData.String()},
		{codecOperationEncode, codecStageTaskAssignEncode, MessageKindTaskAssign.String()},
		{codecOperationDecode, codecStageTaskAssignDecode, MessageKindTaskAssign.String()},
	} {
		require.Equal(t, uint64(1), histogramCount(t, metrics.reg,
			"loki_engine_scheduler_wire_frame_codec_stage_seconds",
			map[string]string{
				"operation":    tc.operation.String(),
				"stage":        tc.stage.String(),
				"frame_type":   FrameKindMessage.String(),
				"message_type": tc.messageType,
			}))
	}

	require.Equal(t, uint64(1), histogramCount(t, metrics.reg,
		"loki_engine_scheduler_wire_frame_size_bytes",
		map[string]string{
			"direction":    directionIncoming.String(),
			"frame_type":   FrameKindMessage.String(),
			"message_type": MessageKindStreamData.String(),
			"mode":         sendModeInternal.String(),
		}))
	require.Equal(t, uint64(1), histogramCount(t, metrics.reg,
		"loki_engine_scheduler_wire_frame_size_bytes",
		map[string]string{
			"direction":    directionOutgoing.String(),
			"frame_type":   FrameKindMessage.String(),
			"message_type": MessageKindTaskAssign.String(),
			"mode":         sendModeSync.String(),
		}))
}

func TestProtobufCodec_TaskStates(t *testing.T) {
	taskULID := ulid.Make()

	states := []workflow.TaskState{
		workflow.TaskStateCreated,
		workflow.TaskStatePending,
		workflow.TaskStateRunning,
		workflow.TaskStateCompleted,
		workflow.TaskStateCancelled,
		workflow.TaskStateFailed,
	}

	codec := DefaultFrameCodec
	mc := &metricCodec{protobufCodec: codec}

	for _, state := range states {
		t.Run(state.String(), func(t *testing.T) {
			message := TaskStatusMessage{
				ID: taskULID,
				Status: workflow.TaskStatus{
					State: state,
				},
			}

			frame := MessageFrame{
				ID:      1,
				Message: message,
			}

			pbFrame, err := mc.frameToPbFrame(frame)
			require.NoError(t, err)

			actualFrame, err := mc.frameFromPbFrame(pbFrame)
			require.NoError(t, err)

			actualMessage := actualFrame.(MessageFrame).Message.(TaskStatusMessage)
			assert.Equal(t, state, actualMessage.Status.State)
		})
	}
}

func TestProtobufCodec_StreamStates(t *testing.T) {
	streamULID := ulid.Make()

	states := []workflow.StreamState{
		workflow.StreamStateIdle,
		workflow.StreamStateOpen,
		workflow.StreamStateBlocked,
		workflow.StreamStateClosed,
	}

	codec := DefaultFrameCodec
	mc := &metricCodec{protobufCodec: codec}

	for _, state := range states {
		t.Run(state.String(), func(t *testing.T) {
			message := StreamStatusMessage{
				StreamID: streamULID,
				State:    state,
			}

			frame := MessageFrame{
				ID:      1,
				Message: message,
			}

			pbFrame, err := mc.frameToPbFrame(frame)
			require.NoError(t, err)

			actualFrame, err := mc.frameFromPbFrame(pbFrame)
			require.NoError(t, err)

			actualMessage := actualFrame.(MessageFrame).Message.(StreamStatusMessage)
			assert.Equal(t, state, actualMessage.State)
		})
	}
}

func TestProtobufCodec_ErrorCases(t *testing.T) {
	codec := DefaultFrameCodec
	mc := &metricCodec{protobufCodec: codec}

	t.Run("nil frame to protobuf", func(t *testing.T) {
		_, err := mc.frameToPbFrame(nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "nil frame")
	})

	t.Run("nil frame from protobuf", func(t *testing.T) {
		_, err := mc.frameFromPbFrame(nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "nil frame")
	})

	t.Run("nil message to protobuf", func(t *testing.T) {
		_, err := mc.messageToPbMessage(nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "nil message")
	})

	t.Run("nil task to protobuf", func(t *testing.T) {
		_, err := mc.taskToPbTask(nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "nil task")
	})

	t.Run("nil arrow record serialization", func(t *testing.T) {
		_, err := codec.SerializeArrowRecord(nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "nil arrow record")
	})

	t.Run("empty arrow data deserialization", func(t *testing.T) {
		_, err := codec.DeserializeArrowRecord([]byte{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "empty arrow data")
	})
}

func createTestArrowRecord() arrow.RecordBatch {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
	}, nil)

	builder := array.NewInt64Builder(memory.DefaultAllocator)

	builder.Append(1)
	builder.Append(2)
	builder.Append(3)

	data := builder.NewArray()

	return array.NewRecordBatch(schema, []arrow.Array{data}, 3)
}
