package worker

import (
	"encoding/json"
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

// packedPipelineNode is one operator in the packed per-task pipeline trace.
// Parent is the array index of the operator's parent, or -1 when it has no
// operator parent.
type packedPipelineNode struct {
	Op         string `json:"op"`
	Parent     int    `json:"parent"`
	SelfMS     int64  `json:"self_ms"`
	BatchesIn  int64  `json:"batches_in"`
	BatchesOut int64  `json:"batches_out"`
	RowsOut    int64  `json:"rows_out"`
}

// packPipelineNodes converts the per-operator records into the packed form
// emitted in the pipeline trace, replacing each operator's parent ID with the
// parent's index in the returned slice (-1 when it has no operator parent).
func packPipelineNodes(nodes []pipelineNode) []packedPipelineNode {
	index := make(map[xcap.ID]int, len(nodes))
	for i, n := range nodes {
		index[n.OperatorID] = i
	}

	packed := make([]packedPipelineNode, len(nodes))
	for i, n := range nodes {
		parent := -1
		if !n.ParentOperatorID.IsZero() {
			if p, ok := index[n.ParentOperatorID]; ok {
				parent = p
			}
		}
		packed[i] = packedPipelineNode{
			Op:         n.OpType,
			Parent:     parent,
			SelfMS:     n.SelfDuration.Milliseconds(),
			BatchesIn:  n.BatchesIn,
			BatchesOut: n.BatchesOut,
			RowsOut:    n.RowsOut,
		}
	}
	return packed
}

// logPipelineTrace emits one always-on line for a task that executed operators,
// packing its operator tree (see packedPipelineNode) as a JSON array. It reads
// only already-collected statistics, so it adds no per-batch work. self_ms is
// wall-clock self-time, not CPU; join on task_id (carried by the logger) to the
// per-task summary to recover query_id.
func logPipelineTrace(logger log.Logger, capture *xcap.Capture) {
	nodes := buildPipelineNodes(pipelineRegionsFromCapture(capture))
	if len(nodes) == 0 {
		return
	}

	packed, err := json.Marshal(packPipelineNodes(nodes))
	if err != nil {
		level.Warn(logger).Log("msg", "failed to encode pipeline trace", "err", err)
		return
	}
	level.Info(logger).Log("msg", "pipeline-trace", "pipeline", string(packed))
}
