//go:build !debug

package debug

import "runtime"

func SetFinalizer(obj, finalizer any) { runtime.SetFinalizer(obj, finalizer) }
