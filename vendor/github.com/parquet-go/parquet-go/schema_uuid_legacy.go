//go:build !go1.27

package parquet

import "reflect"

func isStdlibUUID(reflect.Type) bool { return false }
