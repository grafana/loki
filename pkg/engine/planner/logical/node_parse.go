package logical

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/engine/planner/schema"
)

// ParserKind represents the type of parser to use
type ParserKind int

const (
	ParserLogfmt ParserKind = iota
	ParserJSON
)

// Parse represents a parsing instruction that extracts fields from log lines.
// It takes a table relation as input and produces a new table relation with
// additional columns for the parsed fields.
type Parse struct {
	id string

	Table         Value // The table relation to parse from
	Kind          ParserKind
	RequestedKeys []string
}

// Name returns an identifier for the Parse operation.
func (p *Parse) Name() string {
	if p.id != "" {
		return p.id
	}
	return fmt.Sprintf("%p", p)
}

// String returns the string representation of the Parse instruction
func (p *Parse) String() string {
	return fmt.Sprintf("PARSE %s [kind=%v, keys=%v]", p.Table.Name(), p.Kind, p.RequestedKeys)
}

// Schema returns the schema of the Parse operation.
// Parse adds columns for the requested keys to the input table schema.
func (p *Parse) Schema() *schema.Schema {
	// For now, return the input schema. We'll enhance this when we implement column registration.
	return p.Table.Schema()
}

// isInstruction marks Parse as an Instruction
func (p *Parse) isInstruction() {}

// isValue marks Parse as a Value
func (p *Parse) isValue() {}
