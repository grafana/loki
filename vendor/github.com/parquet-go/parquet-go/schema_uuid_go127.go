//go:build go1.27

package parquet

import (
	"reflect"
	"uuid"
)

func isStdlibUUID(t reflect.Type) bool {
	return t == reflect.TypeFor[uuid.UUID]()
}
