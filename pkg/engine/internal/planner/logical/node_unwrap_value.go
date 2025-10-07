package logical

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/schema"
)

// UnwrapValue represents an unwrap instruction that extracts numeric values from log labels.
// It takes a table relation as input and produces a new table relation with
// an additional column for the projected numeric values.
type UnwrapValue struct {
	id string

	Table           Value  // The table relation to unwrap from
	Identifier      string // Label name to extract value from
	UnwrapOperation string // Optional operation (e.g., "duration_seconds", "bytes")
}

// Name returns an identifier for the Unwrap operation.
func (u *UnwrapValue) Name() string {
	if u.id != "" {
		return u.id
	}
	return fmt.Sprintf("%p", u)
}

// String returns the string representation of the Unwrap instruction
func (u *UnwrapValue) String() string {
	if u.UnwrapOperation != "" {
		return fmt.Sprintf("UNWRAP %s [operation=%s, identifier=%s]", u.Table.Name(), u.UnwrapOperation, u.Identifier)
	}
	return fmt.Sprintf("UNWRAP %s [identifier=%s]", u.Table.Name(), u.Identifier)
}

// Schema returns the schema of the Unwrap operation.
func (u *UnwrapValue) Schema() *schema.Schema {
	//TODO(twhitney): it is unclear to me how schema is used, and if this needs to be extended to add
	//the new, unwrapped columns? If so the Schema of the Parse node likely needs to be changes as well.
	return u.Table.Schema()
}

// isInstruction marks Unwrap as an Instruction
func (u *UnwrapValue) isInstruction() {}

// isValue marks Unwrap as a Value
func (u *UnwrapValue) isValue() {}
