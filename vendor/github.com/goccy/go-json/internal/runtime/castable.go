package runtime

import "reflect"

// CastableTo reports whether a value of the type from is given to a function of WithMarshalers or WithUnmarshalers
// of encoding/json/v2 whose argument is of the type to: as itself, as its address if to is the pointer to it, or as
// its address in an interface value if its pointer type implements to.
func CastableTo(from, to reflect.Type) bool {
	switch to.Kind() {
	case reflect.Interface:
		return reflect.PointerTo(from).Implements(to)
	case reflect.Pointer:
		return reflect.PointerTo(from) == to
	default:
		return from == to
	}
}
