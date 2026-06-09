package testutils

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/require"
)

// checkEnumLookup verifies that a proto enum, its corresponding types.* enum,
// and the marshal/unmarshal lookup maps are consistent. It asserts that:
//   - the proto enum and the types.* enum have the same number of values
//     (catches "added to one side but not the other");
//   - both lookup maps have one entry per proto enum value (catches "added to
//     both enums but forgot to update the lookup");
//   - every proto enum value is a key in the marshal lookup;
//   - the marshal and unmarshal lookup maps are inverses of each other (which,
//     combined with the cardinality checks, implies every types.* value is
//     also covered);
//   - every proto enum value round-trips through MarshalType / UnmarshalType
//     to its original value.
func CheckEnumLookup[Proto, Native comparable](
	t *testing.T,
	name string,
	protoNameMap map[int32]string,
	typesEnumCount int,
	nativeLookup map[Proto]Native,
	protoLookup map[Native]Proto,
	intToProto func(int32) Proto,
	marshalFn func(Proto) (Native, error),
	unmarshalFn func(Native) (Proto, error),
) {
	t.Helper()
	protoCount := len(protoNameMap)

	require.Equalf(t, typesEnumCount, protoCount,
		"%s: types.* enum has %d constants but proto enum has %d values - did you add a value to one side without the other? Check pkg/engine/internal/types/*.go and the .proto file",
		name, typesEnumCount, protoCount)

	require.Equalf(t, protoCount, len(nativeLookup),
		"%s: marshal lookup has %d entries but proto enum has %d values - missing entry in marshal_types.go?",
		name, len(nativeLookup), protoCount)
	require.Equalf(t, protoCount, len(protoLookup),
		"%s: unmarshal lookup has %d entries but proto enum has %d values - missing entry in unmarshal_types.go?",
		name, len(protoLookup), protoCount)

	for v := range protoNameMap {
		p := intToProto(v)
		_, ok := nativeLookup[p]
		require.Truef(t, ok, "%s: marshal lookup missing entry for proto value %v", name, p)
	}

	for protoVal, nativeVal := range nativeLookup {
		backProto, ok := protoLookup[nativeVal]
		require.Truef(t, ok,
			"%s: unmarshal lookup missing entry for native value %v (mapped from %v in marshal lookup)",
			name, nativeVal, protoVal)
		require.Equalf(t, protoVal, backProto,
			"%s: lookups are not inverses - marshal[%v]=%v but unmarshal[%v]=%v",
			name, protoVal, nativeVal, nativeVal, backProto)
	}

	for v := range protoNameMap {
		p := intToProto(v)
		n, err := marshalFn(p)
		require.NoErrorf(t, err, "%s: MarshalType failed for %v", name, p)
		back, err := unmarshalFn(n)
		require.NoErrorf(t, err, "%s: UnmarshalType failed for %v (marshaled from %v)", name, n, p)
		require.Equalf(t, p, back,
			"%s: round-trip mismatch %v -> %v -> %v", name, p, n, back)
	}
}

// countConstantsOfType returns the number of top-level constants of the named
// type declared in the given Go source file. It walks const blocks and tracks
// type inheritance: inside a const block, a spec that has neither an explicit
// type nor an explicit value inherits both from the previous spec, so a single
// type declaration at the top of an iota block applies to all subsequent
// identifiers.
func CountConstantsOfType(t *testing.T, file string, typeName string) int {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, nil, 0)
	require.NoErrorf(t, err, "parsing %s", file)

	var count int
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		var currentType string
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			switch {
			case vs.Type != nil:
				if id, ok := vs.Type.(*ast.Ident); ok {
					currentType = id.Name
				} else {
					currentType = ""
				}
			case vs.Values != nil:
				// An explicit value with no explicit type breaks the inherited
				// type carried over from the previous spec.
				currentType = ""
			}
			if currentType == typeName {
				count += len(vs.Names)
			}
		}
	}
	require.NotZerof(t, count, "no constants of type %s found in %s", typeName, file)
	return count
}
