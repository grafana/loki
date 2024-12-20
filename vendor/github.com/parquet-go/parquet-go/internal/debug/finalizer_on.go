//go:build !debug

package debug

import "runtime"

func SetFinalizer(obj, finalizer interface{}) { runtime.SetFinalizer(obj, finalizer) }
