package stats

import "fmt"

// GoString implementations for the message types that gogo-generated code in
// other packages (pkg/logproto, pkg/querier/queryrange) embeds by value:
// gogoslick generates GoString methods that delegate to the embedded type's
// GoString, which wiresmith does not generate. The local alias types keep
// fmt's %#v from recursing back into these methods.

// GoString implements fmt.GoStringer.
func (r *Result) GoString() string {
	type plain Result
	return fmt.Sprintf("%#v", (*plain)(r))
}

// GoString implements fmt.GoStringer.
func (i *Ingester) GoString() string {
	type plain Ingester
	return fmt.Sprintf("%#v", (*plain)(i))
}

// GoString implements fmt.GoStringer.
func (i *Index) GoString() string {
	type plain Index
	return fmt.Sprintf("%#v", (*plain)(i))
}
