package worker

import (
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	"github.com/grafana/loki/v3/pkg/xcap"
)

// pipelineRegionSuffix is the suffix the executor appends to an operator's type
// name when it wraps the operator in an xcap region. observedPipeline names its
// per-operator read region "<op_type>.Read" (see executor.NewObservedPipeline),
// so the suffix both identifies operator regions in a capture and is stripped to
// recover the operator type.
const pipelineRegionSuffix = ".Read"

// pipelineRegionStat holds the per-operator statistics observedPipeline already
// collected for a single executor node, addressed by its xcap region
// identifiers. It is the cold-path input to [buildPipelineNodes]: every field is
// read from a finalized capture after execution, so assembling these records
// touches no per-batch hot path.
type pipelineRegionStat struct {
	// ID is the operator region's own identifier.
	ID xcap.ID
	// ParentID is the identifier of the operator region's parent. It may refer
	// to a non-operator region (such as the task root), in which case the
	// operator is treated as having no operator parent.
	ParentID xcap.ID
	// Name is the operator region's name, e.g. "DataObjScan.Read".
	Name string
	// ReadCalls is xcap.StatPipelineReadCalls: batches produced by this operator.
	ReadCalls int64
	// RowsOut is xcap.StatPipelineRowsOut: rows produced by this operator.
	RowsOut int64
	// ReadDuration is xcap.StatPipelineReadDuration: wall time around this
	// operator's Read, inclusive of its children.
	ReadDuration time.Duration
}

// pipelineNode is one per-operator record describing this task's operator tree,
// derived from collected statistics by [buildPipelineNodes].
type pipelineNode struct {
	// OperatorID is the operator region's own identifier.
	OperatorID xcap.ID
	// ParentOperatorID is the identifier of the parent operator region, or the
	// zero ID when this operator has no operator parent.
	ParentOperatorID xcap.ID
	// OpType is the operator type: the region name without the ".Read" suffix.
	OpType string
	// SelfDuration is the exclusive wall-clock self-time of this operator: its
	// ReadDuration minus the sum of its direct children's ReadDuration, clamped
	// at zero. It is wall-clock time, not true CPU time — it still includes time
	// the operator spent blocked (e.g. on I/O or a downstream sink) and only
	// excludes time attributed to its direct children.
	SelfDuration time.Duration
	// BatchesOut is the number of batches this operator produced.
	BatchesOut int64
	// BatchesIn is the sum of batches produced by this operator's direct
	// children.
	BatchesIn int64
	// RowsOut is the number of rows this operator produced.
	RowsOut int64
}

// buildPipelineNodes assembles one [pipelineNode] per operator region from the
// statistics collected during execution. It is a pure function: it reads only
// the supplied records and performs no I/O.
//
// Parent/child relationships are resolved within the supplied set. A record's
// ParentOperatorID is preserved only when it refers to another record in
// regions, so an operator whose parent is a non-operator region (such as the
// task root) reports the zero ID. BatchesIn and the exclusive SelfDuration are
// computed from each operator's direct children within the set.
func buildPipelineNodes(regions []pipelineRegionStat) []pipelineNode {
	// Index regions by ID and group direct children by parent so each operator's
	// children can be resolved in a single pass.
	known := make(map[xcap.ID]struct{}, len(regions))
	for _, r := range regions {
		known[r.ID] = struct{}{}
	}
	children := make(map[xcap.ID][]pipelineRegionStat, len(regions))
	for _, r := range regions {
		if _, ok := known[r.ParentID]; ok {
			children[r.ParentID] = append(children[r.ParentID], r)
		}
	}

	nodes := make([]pipelineNode, 0, len(regions))
	for _, r := range regions {
		var (
			childReadDuration time.Duration
			batchesIn         int64
		)
		for _, c := range children[r.ID] {
			childReadDuration += c.ReadDuration
			batchesIn += c.ReadCalls
		}

		self := r.ReadDuration - childReadDuration
		if self < 0 {
			self = 0
		}

		var parent xcap.ID
		if _, ok := known[r.ParentID]; ok {
			parent = r.ParentID
		}

		nodes = append(nodes, pipelineNode{
			OperatorID:       r.ID,
			ParentOperatorID: parent,
			OpType:           strings.TrimSuffix(r.Name, pipelineRegionSuffix),
			SelfDuration:     self,
			BatchesOut:       r.ReadCalls,
			BatchesIn:        batchesIn,
			RowsOut:          r.RowsOut,
		})
	}
	return nodes
}

// pipelineRegionsFromCapture extracts the operator regions from a finalized
// capture. Operator regions are those observedPipeline created, identified by
// the ".Read" suffix the executor gives them. Statistics are read from the
// already-aggregated observations, so this runs entirely on the cold path.
func pipelineRegionsFromCapture(capture *xcap.Capture) []pipelineRegionStat {
	if capture == nil {
		return nil
	}

	var out []pipelineRegionStat
	for _, region := range capture.Regions() {
		name := region.Name()
		if !strings.HasSuffix(name, pipelineRegionSuffix) {
			continue
		}

		stat := pipelineRegionStat{
			ID:       region.ID(),
			ParentID: region.ParentID(),
			Name:     name,
		}
		for _, obs := range region.Observations() {
			switch obs.Statistic.Key() {
			case xcap.StatPipelineReadCalls.Key():
				if v, ok := obs.Int64(); ok {
					stat.ReadCalls = v
				}
			case xcap.StatPipelineRowsOut.Key():
				if v, ok := obs.Int64(); ok {
					stat.RowsOut = v
				}
			case xcap.StatPipelineReadDuration.Key():
				if v, ok := obs.Float64(); ok {
					stat.ReadDuration = time.Duration(v * float64(time.Second))
				}
			}
		}
		out = append(out, stat)
	}
	return out
}

// logPipelineNodes emits one pipeline-node debug log line per operator in the
// task's operator tree, read from the task's finalized capture. It reads only
// already-collected statistics and adds nothing to the per-batch hot path.
// Emission is gated at debug level, like the execution-plan-detail log, because
// there is one line per operator per task.
//
// op_self_ms is wall-clock self-time, not true CPU. operator_id /
// parent_operator_id are the per-task xcap region identifiers describing this
// task's operator tree, not the physical-plan node ids. query_id is omitted
// because it isn't available to the worker; the logger already carries task_id,
// and the per-task summary log carries query_id, so join on task_id to recover it.
func logPipelineNodes(logger log.Logger, capture *xcap.Capture) {
	for _, node := range buildPipelineNodes(pipelineRegionsFromCapture(capture)) {
		var parent string
		if !node.ParentOperatorID.IsZero() {
			parent = node.ParentOperatorID.String()
		}
		level.Debug(logger).Log(
			"msg", "pipeline-node",
			"operator_id", node.OperatorID.String(),
			"parent_operator_id", parent,
			"op_type", node.OpType,
			"op_self_ms", node.SelfDuration.Milliseconds(),
			"batches_out", node.BatchesOut,
			"batches_in", node.BatchesIn,
			"rows_out", node.RowsOut,
		)
	}
}
