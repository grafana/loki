//go:build go1.24

package decoder

// embeddedFieldNames is whether the path of a type error has the names of the embedded fields which a field
// is promoted through, which encoding/json of Go 1.24 to 1.26 reports ( see typeErrorPath ).
const embeddedFieldNames = true
