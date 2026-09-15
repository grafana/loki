package layout

import "github.com/grafana/loki/v3/pkg/expr"

// collectColumns returns the set of distinct Column names referenced by e.
func collectColumns(e expr.Expression) map[string]struct{} {
	cols := make(map[string]struct{})
	expr.Walk(e, func(node expr.Expression) {
		if col, ok := node.(*expr.Column); ok {
			cols[col.Name] = struct{}{}
		}
	})
	return cols
}

// rewriteColumnsToIdentity returns a copy of e with all Column nodes matching
// name replaced by Identity. This is the scope-rewrite step: when the struct
// reader pushes a sub-expression to a child reader, it replaces Column
// references to that child's name with Identity so the expression operates in
// the child's namespace.
func rewriteColumnsToIdentity(e expr.Expression, name string) expr.Expression {
	if e == nil {
		return nil
	}
	if col, ok := e.(*expr.Column); ok && col.Name == name {
		return &expr.Identity{}
	}
	return expr.Map(e, func(child expr.Expression) expr.Expression {
		return rewriteColumnsToIdentity(child, name)
	})
}

// simplifyProjection rewrites an expression tree so that all field references
// appear as Column nodes. It applies the following rules bottom-up:
//
//   - Identity{} → MakeStruct{fields, [Column{f} for each f]}
//   - Extract{name, MakeStruct{..., name: X, ...}} → X
//   - Include{names, MakeStruct{...}} → MakeStruct with only named fields
//   - Exclude{names, MakeStruct{...}} → MakeStruct with named fields removed
//
// The fields parameter provides the field names for expanding Identity at the
// current struct level. It may be nil when no Identity nodes are expected.
func simplifyProjection(e expr.Expression, fields []string) expr.Expression {
	if e == nil {
		return nil
	}

	if _, ok := e.(*expr.Identity); ok {
		names := make([]string, len(fields))
		copy(names, fields)
		values := make([]expr.Expression, len(fields))
		for i, f := range fields {
			values[i] = &expr.Column{Name: f}
		}
		return &expr.MakeStruct{Names: names, Values: values}
	}

	e = expr.Map(e, func(child expr.Expression) expr.Expression {
		return simplifyProjection(child, fields)
	})

	return simplifyFolds(e)
}

// simplifyFolds applies fold rules to an already-recursed expression node.
func simplifyFolds(e expr.Expression) expr.Expression {
	switch e := e.(type) {
	case *expr.Extract:
		if ms, ok := e.Value.(*expr.MakeStruct); ok {
			for i, name := range ms.Names {
				if name == e.Name {
					return ms.Values[i]
				}
			}
		}

	case *expr.Include:
		if ms, ok := e.Value.(*expr.MakeStruct); ok {
			return includeMakeStruct(e.Names, ms)
		}

	case *expr.Exclude:
		if ms, ok := e.Value.(*expr.MakeStruct); ok {
			return excludeMakeStruct(e.Names, ms)
		}
	}

	return e
}

func includeMakeStruct(names []string, ms *expr.MakeStruct) *expr.MakeStruct {
	resultNames := make([]string, 0, len(names))
	resultValues := make([]expr.Expression, 0, len(names))
	for _, name := range names {
		for i, msName := range ms.Names {
			if msName == name {
				resultNames = append(resultNames, name)
				resultValues = append(resultValues, ms.Values[i])
				break
			}
		}
	}
	return &expr.MakeStruct{Names: resultNames, Values: resultValues}
}

func excludeMakeStruct(names []string, ms *expr.MakeStruct) *expr.MakeStruct {
	excludeSet := make(map[string]struct{}, len(names))
	for _, name := range names {
		excludeSet[name] = struct{}{}
	}
	resultNames := make([]string, 0, len(ms.Names))
	resultValues := make([]expr.Expression, 0, len(ms.Names))
	for i, name := range ms.Names {
		if _, excluded := excludeSet[name]; !excluded {
			resultNames = append(resultNames, name)
			resultValues = append(resultValues, ms.Values[i])
		}
	}
	return &expr.MakeStruct{Names: resultNames, Values: resultValues}
}
