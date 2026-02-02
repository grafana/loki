package physical

import (
	"fmt"
	"iter"

	"github.com/oklog/ulid/v2"
)

// ScanTarget represents a target of a [ScanSet].
type ScanTarget struct {
	Type ScanType

	// DataObj is non-nil if Type is [ScanTypeDataObject]. Despite DataObjScan
	// implementing [Node], the value is not inserted into the graph as a node.
	DataObject *DataObjScan

	// Pointers is non-nil if Type is [ScanTypePointers]. Despite PointersScan
	// implementing [Node], the value is not inserted into the graph as a node.
	Pointers *PointersScan
}

// Clone returns a copy of the scan target.
func (t *ScanTarget) Clone() *ScanTarget {
	res := &ScanTarget{Type: t.Type}
	if t.DataObject != nil {
		res.DataObject = t.DataObject.Clone().(*DataObjScan)
	}
	if t.Pointers != nil {
		res.Pointers = t.Pointers.Clone().(*PointersScan)
	}
	return res
}

// ScanType represents the data being scanned in a target of a [ScanSet].
type ScanType int

const (
	ScanTypeInvalid ScanType = iota
	ScanTypeDataObject
	ScanTypePointers
)

// String returns a string representation of the scan type.
func (ty ScanType) String() string {
	switch ty {
	case ScanTypeInvalid:
		return "ScanTypeInvalid"
	case ScanTypeDataObject:
		return "ScanTypeDataObject"
	case ScanTypePointers:
		return "ScanTypePointers"
	default:
		return fmt.Sprintf("ScanType(%d)", ty)
	}
}

// ScanSet represents a physical plan operation for reading data from targets.
type ScanSet struct {
	NodeID ulid.ULID

	// Targets to scan.
	Targets []*ScanTarget

	// Projections are used to limit the columns that are read to the ones
	// provided in the column expressions to reduce the amount of data that
	// needs to be processed.
	Projections []ColumnExpression

	// Predicates are used to filter rows to reduce the amount of rows that are
	// returned. Predicates would almost always contain a time range filter to
	// only read the logs for the requested time range.
	Predicates []Expression

	// ShardBatchSize, when > 0, causes Shards() to yield ScanSet nodes with up to
	// ShardBatchSize targets each (workflow-level batching). Applies to all target types.
	// 0 means one shard per target.
	ShardBatchSize int
}

// ID returns the ULID that uniquely identifies the node in the plan.
func (s *ScanSet) ID() ulid.ULID { return s.NodeID }

// Clone returns a deep copy of the node with a new unique ID.
func (s *ScanSet) Clone() Node {
	newTargets := make([]*ScanTarget, 0, len(s.Targets))
	for _, target := range s.Targets {
		newTargets = append(newTargets, target.Clone())
	}

	return &ScanSet{
		NodeID:         ulid.Make(),
		Targets:        newTargets,
		ShardBatchSize: s.ShardBatchSize,
	}
}

// Type returns [NodeTypeScanSet].
func (s *ScanSet) Type() NodeType {
	return NodeTypeScanSet
}

// Shards returns an iterator over the shards of the scan. Each emitted shard
// will be a clone. Projections and predicates on the ScanSet are cloned and
// applied to each shard.
//
// When ShardBatchSize > 0, yields ScanSet nodes each with up to ShardBatchSize
// targets (workflow-level batching). Otherwise yields one node per target
// (DataObjScan or PointersScan).
//
// Shards panics if one of the targets is invalid.
func (s *ScanSet) Shards() iter.Seq[Node] {
	return func(yield func(Node) bool) {
		// Batched sharding: when ShardBatchSize > 0, yield one ScanSet per chunk of targets.
		if s.ShardBatchSize > 0 {
			for i := 0; i < len(s.Targets); i += s.ShardBatchSize {
				end := i + s.ShardBatchSize
				if end > len(s.Targets) {
					end = len(s.Targets)
				}
				chunk := s.Targets[i:end]
				batchedTargets := make([]*ScanTarget, len(chunk))
				for j, t := range chunk {
					batchedTargets[j] = t.Clone()
				}
				shardSet := &ScanSet{
					NodeID:         ulid.Make(),
					Targets:        batchedTargets,
					Projections:    s.Projections,
					Predicates:     s.Predicates,
					ShardBatchSize: 0, // injected ScanSet does not re-shard
				}
				if !yield(shardSet) {
					return
				}
			}
			return
		}

		// One shard per target (current behavior).
		for _, target := range s.Targets {
			switch target.Type {
			case ScanTypeDataObject:
				node := target.DataObject.Clone().(*DataObjScan)
				node.Projections = cloneExpressions(append(s.Projections, node.Projections...))
				node.Predicates = cloneExpressions(append(s.Predicates, node.Predicates...))

				// Preserve the original NodeID from the target's DataObjScan to
				// maintain traceability. This allows sharded nodes to be traced
				// back to their originating target.
				node.NodeID = target.DataObject.NodeID

				if !yield(node) {
					return
				}
			case ScanTypePointers:
				node := target.Pointers.Clone().(*PointersScan)
				node.NodeID = target.Pointers.NodeID
				if !yield(node) {
					return
				}
			default:
				panic(fmt.Sprintf("invalid scan type %s", target.Type))
			}
		}
	}
}
