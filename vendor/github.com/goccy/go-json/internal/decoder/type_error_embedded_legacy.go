//go:build !go1.24

package decoder

// embeddedFieldNames is whether the path of a type error has the names of the embedded fields which a field
// is promoted through: encoding/json before Go 1.24 reports the name of the field only ( see typeErrorPath ).
const embeddedFieldNames = false
