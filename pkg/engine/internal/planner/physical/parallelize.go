package physical

import "fmt"

// Parallelize represents a hint to the engine to partition and parallelize the
// children branches of the Parallelize and emit results as a single sequence
// with no guaranteed order.
type Parallelize struct {
	id string
}

// ID returns a string that uniquely identifies the node in the plan.
func (p *Parallelize) ID() string {
	if p.id == "" {
		return fmt.Sprintf("%p", p)
	}
	return p.id
}

// Clone returns a deep copy of the node (minus its ID).
func (p *Parallelize) Clone() Node {
	return &Parallelize{ /* nothing to clone */ }
}

// Type returns [NodeTypeParallelize].
func (p *Parallelize) Type() NodeType { return NodeTypeParallelize }

// Accept implements the [Node] interface, dispatching itself to v.
func (p *Parallelize) Accept(v Visitor) error { return v.VisitParallelize(p) }
