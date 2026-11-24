package compactor

import (
	"encoding/base64"
	"fmt"
	"sync"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/proto/physicalpb"
)

// planDump logs the plan of one compaction task at most once, so that a
// developer can run the same task again.
//
// The coordinator logs the plan, not the worker. A worker that runs out of
// memory or crashes cannot log, but the scheduler then fails the task, and
// the coordinator sees the failure.
//
// planDump is safe for concurrent use.
type planDump struct {
	logger log.Logger
	plan   *physical.Plan
	root   physical.Node

	once sync.Once
}

func newPlanDump(logger log.Logger, plan *physical.Plan, root physical.Node) *planDump {
	return &planDump{logger: logger, plan: plan, root: root}
}

// log logs the plan with reason and keyvals, unless an earlier call logged
// it. The plan field holds the physicalpb.Plan protobuf message in standard
// base64.
func (d *planDump) log(reason string, keyvals ...any) {
	d.once.Do(func() {
		fields := []any{"msg", "compaction task plan dump", "reason", reason}
		fields = append(fields, physical.LogFields(d.root)...)
		fields = append(fields, keyvals...)
		raw, err := marshalPlan(d.plan)
		if err != nil {
			fields = append(fields, "marshal_err", err)
		} else {
			fields = append(fields, "plan_bytes", len(raw), "plan", base64.StdEncoding.EncodeToString(raw))
		}
		level.Warn(d.logger).Log(fields...)
	})
}

// marshalPlan returns plan as an encoded physicalpb.Plan protobuf message.
func marshalPlan(plan *physical.Plan) ([]byte, error) {
	var msg physicalpb.Plan
	if err := msg.UnmarshalPhysical(plan); err != nil {
		return nil, fmt.Errorf("converting plan to protobuf: %w", err)
	}
	raw, err := msg.Marshal()
	if err != nil {
		return nil, fmt.Errorf("marshaling plan: %w", err)
	}
	return raw, nil
}
