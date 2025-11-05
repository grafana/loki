package wire

import (
	"errors"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/workflow"
)

func TestProtoMapper_Frames(t *testing.T) {
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
				Error: errors.New("test error"),
			},
		},
		"DiscardFrame": {
			frame: DiscardFrame{ID: 45},
		},
	}

	mapper := &protoMapper{memory.DefaultAllocator}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			pbFrame, err := mapper.frameToPbFrame(tt.frame)
			require.NoError(t, err)

			actualFrame, err := mapper.frameFromPbFrame(pbFrame)
			require.NoError(t, err)

			assert.Equal(t, tt.frame, actualFrame)
		})
	}
}

func TestProtoMapper_Messages(t *testing.T) {
	taskULID := ulid.Make()
	streamULID := ulid.Make()

	tests := map[string]struct {
		message Message
	}{
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
				Receiver: &tcpAddr{Addr: "192.168.1.1:8080"},
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

	mapper := &protoMapper{memory.DefaultAllocator}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			frame := MessageFrame{
				ID:      100,
				Message: tt.message,
			}

			pbFrame, err := mapper.frameToPbFrame(frame)
			require.NoError(t, err)

			actualFrame, err := mapper.frameFromPbFrame(pbFrame)
			require.NoError(t, err)

			assert.Equal(t, frame, actualFrame)
		})
	}
}

func TestProtoMapper_StreamDataMessage(t *testing.T) {
	streamULID := ulid.Make()
	mapper := &protoMapper{memory.DefaultAllocator}

	originalRecord := createTestArrowRecord()

	message := StreamDataMessage{
		StreamID: streamULID,
		Data:     originalRecord,
	}

	frame := MessageFrame{
		ID:      100,
		Message: message,
	}

	pbFrame, err := mapper.frameToPbFrame(frame)
	require.NoError(t, err)

	actualFrame, err := mapper.frameFromPbFrame(pbFrame)
	require.NoError(t, err)

	actualMessage := actualFrame.(MessageFrame).Message.(StreamDataMessage)

	assert.Equal(t, frame.ID, actualFrame.(MessageFrame).ID)
	assert.Equal(t, streamULID, actualMessage.StreamID)

	assert.NotNil(t, actualMessage.Data)
	assert.True(t, originalRecord.Schema().Equal(actualMessage.Data.Schema()))
	assert.Equal(t, originalRecord.NumRows(), actualMessage.Data.NumRows())
	assert.Equal(t, originalRecord.NumCols(), actualMessage.Data.NumCols())
}

func TestProtoMapper_TaskStates(t *testing.T) {
	taskULID := ulid.Make()

	states := []workflow.TaskState{
		workflow.TaskStateCreated,
		workflow.TaskStatePending,
		workflow.TaskStateRunning,
		workflow.TaskStateCompleted,
		workflow.TaskStateCancelled,
		workflow.TaskStateFailed,
	}

	mapper := &protoMapper{memory.DefaultAllocator}

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

			pbFrame, err := mapper.frameToPbFrame(frame)
			require.NoError(t, err)

			actualFrame, err := mapper.frameFromPbFrame(pbFrame)
			require.NoError(t, err)

			actualMessage := actualFrame.(MessageFrame).Message.(TaskStatusMessage)
			assert.Equal(t, state, actualMessage.Status.State)
		})
	}
}

func TestProtoMapper_StreamStates(t *testing.T) {
	streamULID := ulid.Make()

	states := []workflow.StreamState{
		workflow.StreamStateIdle,
		workflow.StreamStateOpen,
		workflow.StreamStateBlocked,
		workflow.StreamStateClosed,
	}

	mapper := &protoMapper{memory.DefaultAllocator}

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

			pbFrame, err := mapper.frameToPbFrame(frame)
			require.NoError(t, err)

			actualFrame, err := mapper.frameFromPbFrame(pbFrame)
			require.NoError(t, err)

			actualMessage := actualFrame.(MessageFrame).Message.(StreamStatusMessage)
			assert.Equal(t, state, actualMessage.State)
		})
	}
}

func TestProtoMapper_ErrorCases(t *testing.T) {
	mapper := &protoMapper{memory.DefaultAllocator}

	t.Run("nil frame to protobuf", func(t *testing.T) {
		_, err := mapper.frameToPbFrame(nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "nil frame")
	})

	t.Run("nil frame from protobuf", func(t *testing.T) {
		_, err := mapper.frameFromPbFrame(nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "nil frame")
	})

	t.Run("nil message to protobuf", func(t *testing.T) {
		_, err := mapper.messageToPbMessage(nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "nil message")
	})

	t.Run("nil task to protobuf", func(t *testing.T) {
		_, err := mapper.taskToPbTask(nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "nil task")
	})

	t.Run("nil arrow record serialization", func(t *testing.T) {
		_, err := mapper.serializeArrowRecord(nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "nil arrow record")
	})

	t.Run("empty arrow data deserialization", func(t *testing.T) {
		_, err := mapper.deserializeArrowRecord([]byte{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "empty arrow data")
	})
}

func TestProtoMapper_ArrowRecordSerialization(t *testing.T) {
	mapper := &protoMapper{memory.DefaultAllocator}

	tests := map[string]struct {
		createRecord func() arrow.Record
	}{
		"simple int64 record": {
			createRecord: createTestArrowRecord,
		},
		"empty record": {
			createRecord: func() arrow.Record {
				schema := arrow.NewSchema([]arrow.Field{
					{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
				}, nil)

				builder := array.NewInt64Builder(memory.DefaultAllocator)
				data := builder.NewArray()

				return array.NewRecord(schema, []arrow.Array{data}, 0)
			},
		},
		"multiple columns": {
			createRecord: func() arrow.Record {
				schema := arrow.NewSchema([]arrow.Field{
					{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
					{Name: "value", Type: arrow.PrimitiveTypes.Float64, Nullable: false},
				}, nil)

				idBuilder := array.NewInt64Builder(memory.DefaultAllocator)
				idBuilder.Append(1)
				idBuilder.Append(2)

				valBuilder := array.NewFloat64Builder(memory.DefaultAllocator)
				valBuilder.Append(1.5)
				valBuilder.Append(2.5)

				idData := idBuilder.NewArray()

				valData := valBuilder.NewArray()

				return array.NewRecord(schema, []arrow.Array{idData, valData}, 2)
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			original := tt.createRecord()

			data, err := mapper.serializeArrowRecord(original)
			require.NoError(t, err)
			require.NotEmpty(t, data)

			deserialized, err := mapper.deserializeArrowRecord(data)
			require.NoError(t, err)
			require.NotNil(t, deserialized)

			assert.True(t, original.Schema().Equal(deserialized.Schema()))
			assert.Equal(t, original.NumRows(), deserialized.NumRows())
			assert.Equal(t, original.NumCols(), deserialized.NumCols())
		})
	}
}

func createTestArrowRecord() arrow.Record {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
	}, nil)

	builder := array.NewInt64Builder(memory.DefaultAllocator)

	builder.Append(1)
	builder.Append(2)
	builder.Append(3)

	data := builder.NewArray()

	return array.NewRecord(schema, []arrow.Array{data}, 3)
}
