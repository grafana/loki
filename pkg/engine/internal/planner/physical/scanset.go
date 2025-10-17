package physical

import (
	"fmt"
)

// ScanTarget represents a target of a [ScanSet].
type ScanTarget struct {
	Type ScanType

	// DataObj is non-nil if Type is [ScanTypeDataObject]. Despite DataObjScan
	// implementing [Node], the value is not inserted into the graph as a node.
	DataObject *DataObjScan
}

// Clone returns a copy of the scan target.
func (t *ScanTarget) Clone() *ScanTarget {
	res := &ScanTarget{Type: t.Type}
	if t.DataObject != nil {
		res.DataObject = t.DataObject.Clone().(*DataObjScan)
	}
	return res
}

// ScanType represents the data being scanned in a target of a [ScanSet].
type ScanType int

const (
	ScanTypeInvalid ScanType = iota
	ScanTypeDataObject
)

// String returns a string representation of the scan type.
func (ty ScanType) String() string {
	switch ty {
	case ScanTypeInvalid:
		return "ScanTypeInvalid"
	case ScanTypeDataObject:
		return "ScanTypeDataObject"
	default:
		return fmt.Sprintf("ScanType(%d)", ty)
	}
}

// ScanSet represents a physical plan operation for reading data from targets.
type ScanSet struct {
	id string

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
}

// ID returns a string that uniquely identifies the node in the plan.
func (s *ScanSet) ID() string {
	if s.id == "" {
		return fmt.Sprintf("%p", s)
	}
	return s.id
}

// Clone returns a deep copy of the node (minus its ID).
func (s *ScanSet) Clone() Node {
	newTargets := make([]*ScanTarget, 0, len(s.Targets))
	for _, target := range s.Targets {
		newTargets = append(newTargets, target.Clone())
	}

	return &ScanSet{Targets: newTargets}
}

// Type returns [NodeTypeScanSet].
func (s *ScanSet) Type() NodeType {
	return NodeTypeScanSet
}

// Accept dispatches s to the provided [Visitor] v.
func (s *ScanSet) Accept(v Visitor) error {
	return v.VisitScanSet(s)
}
