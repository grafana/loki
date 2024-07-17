package metastore

import (
	"fmt"
	"io"
	"reflect"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/gogo/protobuf/proto"
	"github.com/hashicorp/raft"

	metastorepb "github.com/grafana/loki/v3/pkg/ingester-rf1/metastore/metastorepb"
	"github.com/grafana/loki/v3/pkg/ingester-rf1/metastore/raftlogpb"
)

// The map is used to determine the type of the given command,
// when the request is converted to a Raft log entry.
var commandTypeMap = map[reflect.Type]raftlogpb.CommandType{
	reflect.TypeOf(new(metastorepb.AddBlockRequest)): raftlogpb.COMMAND_TYPE_ADD_BLOCK,
	reflect.TypeOf(new(raftlogpb.TruncateCommand)):   raftlogpb.COMMAND_TYPE_TRUNCATE,
}

// The map is used to determine the handler for the given command,
// read from the Raft log entry.
var commandHandlers = map[raftlogpb.CommandType]commandHandler{
	raftlogpb.COMMAND_TYPE_ADD_BLOCK: func(fsm *FSM, raw []byte) fsmResponse {
		return handleCommand(raw, fsm.state.applyAddBlock)
	},
	raftlogpb.COMMAND_TYPE_TRUNCATE: func(fsm *FSM, raw []byte) fsmResponse {
		return handleCommand(raw, fsm.state.applyTruncate)
	},
}

// TODO: Add registration functions.

type FSM struct {
	logger log.Logger
	state  *metastoreState
	db     *boltdb
}

type fsmResponse struct {
	msg proto.Message
	err error
}

type fsmError struct {
	log *raft.Log
	err error
}

func errResponse(l *raft.Log, err error) fsmResponse {
	return fsmResponse{err: &fsmError{log: l, err: err}}
}

func (e *fsmError) Error() string {
	if e.err == nil {
		return ""
	}
	if e.log == nil {
		return e.err.Error()
	}
	return fmt.Sprintf("term: %d; index: %d; appended_at: %v; error: %v",
		e.log.Index, e.log.Term, e.log.AppendedAt, e.err)
}

type commandHandler func(*FSM, []byte) fsmResponse

// TODO(kolesnikovae): replace commandCall with interface:
// type command[Req, Resp proto.Message] interface {
//   apply(Req) (Resp, error)
// }

type commandCall[Req, Resp proto.Message] func(Req) (Resp, error)

func newFSM(logger log.Logger, db *boltdb, state *metastoreState) *FSM {
	return &FSM{
		logger: logger,
		db:     db,
		state:  state,
	}
}

// TODO(kolesnikovae): Implement BatchingFSM.

func (fsm *FSM) Apply(l *raft.Log) interface{} {
	switch l.Type {
	case raft.LogNoop:
	case raft.LogBarrier:
	case raft.LogConfiguration:
	case raft.LogCommand:
		return fsm.applyCommand(l)
	default:
		_ = level.Warn(fsm.logger).Log("msg", "unexpected log entry, ignoring", "type", l.Type.String())
	}
	return nil
}

// applyCommand receives raw command from the raft log (FSM.Apply),
// and calls the corresponding handler on the _local_ FSM, based on
// the command type.
func (fsm *FSM) applyCommand(l *raft.Log) interface{} {
	var e raftlogpb.RaftLogEntry
	if err := proto.Unmarshal(l.Data, &e); err != nil {
		return errResponse(l, err)
	}
	if handler, ok := commandHandlers[e.Type]; ok {
		return handler(fsm, e.Payload)
	}
	return errResponse(l, fmt.Errorf("unknown command type: %v", e.Type.String()))
}

// handleCommand receives payload of the command from the raft log (FSM.Apply),
// and the function that processes the command. Returned response is wrapped in
// fsmResponse and is available to the FSM.Apply caller.
func handleCommand[Req, Resp proto.Message](raw []byte, call commandCall[Req, Resp]) fsmResponse {
	var resp fsmResponse
	defer func() {
		if r := recover(); r != nil {
			resp.err = panicError(r)
		}
	}()
	req := newProto[Req]()
	if resp.err = proto.Unmarshal(raw, req); resp.err != nil {
		return resp
	}
	resp.msg, resp.err = call(req)
	return resp
}

func newProto[T proto.Message]() T {
	var msg T
	msgType := reflect.TypeOf(msg).Elem()
	return reflect.New(msgType).Interface().(T)
}

func (fsm *FSM) Snapshot() (raft.FSMSnapshot, error) {
	// Snapshot should only capture a pointer to the state, and any
	// expensive IO should happen as part of FSMSnapshot.Persist.
	return fsm.db.createSnapshot()
}

func (fsm *FSM) Restore(snapshot io.ReadCloser) error {
	_ = level.Info(fsm.logger).Log("msg", "restoring snapshot")
	defer func() {
		_ = snapshot.Close()
	}()
	if err := fsm.db.restore(snapshot); err != nil {
		return fmt.Errorf("failed to restore from snapshot: %w", err)
	}
	if err := fsm.state.restore(fsm.db); err != nil {
		return fmt.Errorf("failed to restore state: %w", err)
	}
	return nil
}

// applyCommand issues the command to the raft log based on the request type,
// and returns the response of FSM.Apply.
func applyCommand[Req, Resp proto.Message](
	log *raft.Raft,
	req Req,
	timeout time.Duration,
) (
	future raft.ApplyFuture,
	resp Resp,
	err error,
) {
	defer func() {
		if r := recover(); r != nil {
			err = panicError(r)
		}
	}()
	raw, err := marshallRequest(req)
	if err != nil {
		return nil, resp, err
	}
	future = log.Apply(raw, timeout)
	if err = future.Error(); err != nil {
		return nil, resp, err
	}
	fsmResp := future.Response().(fsmResponse)
	if fsmResp.msg != nil {
		resp, _ = fsmResp.msg.(Resp)
	}
	return future, resp, fsmResp.err
}

func marshallRequest[Req proto.Message](req Req) ([]byte, error) {
	cmdType, ok := commandTypeMap[reflect.TypeOf(req)]
	if !ok {
		return nil, fmt.Errorf("unknown command type: %T", req)
	}
	var err error
	entry := raftlogpb.RaftLogEntry{Type: cmdType}
	entry.Payload, err = proto.Marshal(req)
	if err != nil {
		return nil, err
	}
	raw, err := proto.Marshal(&entry)
	if err != nil {
		return nil, err
	}
	return raw, nil
}
