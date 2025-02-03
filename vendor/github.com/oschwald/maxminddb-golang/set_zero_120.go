//go:build go1.20
// +build go1.20

package maxminddb

import "reflect"

func reflectSetZero(v reflect.Value) {
	v.SetZero()
}
