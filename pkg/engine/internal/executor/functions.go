package executor

import (
	"github.com/apache/arrow-go/v18/arrow"

	"github.com/grafana/loki/v3/pkg/engine/internal/functions"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

// The generic expression functions (comparisons, arithmetic, string matching,
// logical operations) live in [functions] together with the signature catalog.
// Only the cast and parse functions are registered here, because their
// implementations depend on the executor's error tracking and parser code.
func init() {
	// Cast functions
	functions.Unary.Register(types.UnaryOpCastFloat, arrow.BinaryTypes.String, castFn(types.UnaryOpCastFloat))
	functions.Unary.Register(types.UnaryOpCastBytes, arrow.BinaryTypes.String, castFn(types.UnaryOpCastBytes))
	functions.Unary.Register(types.UnaryOpCastDuration, arrow.BinaryTypes.String, castFn(types.UnaryOpCastDuration))

	// Parse functions
	functions.Variadic.Register(types.VariadicOpParseLogfmt, parseFn(types.VariadicOpParseLogfmt))
	functions.Variadic.Register(types.VariadicOpParseJSON, parseFn(types.VariadicOpParseJSON))
	functions.Variadic.Register(types.VariadicOpParseRegexp, parseFn(types.VariadicOpParseRegexp))
	functions.Variadic.Register(types.VariadicOpParseLabelfmt, parseFn(types.VariadicOpParseLabelfmt))
	functions.Variadic.Register(types.VariadicOpParseLinefmt, parseFn(types.VariadicOpParseLinefmt))
}
