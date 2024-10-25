// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !cgo

package fakecgo

import "unsafe"

//go:nosplit
//go:norace
func _cgo_sys_thread_start(ts *ThreadStart) {
	var attr pthread_attr_t
	var ign, oset sigset_t
	var p pthread_t
	var size size_t
	var err int

	sigfillset(&ign)
	pthread_sigmask(SIG_SETMASK, &ign, &oset)

	size = pthread_get_stacksize_np(pthread_self())
	pthread_attr_init(&attr)
	pthread_attr_setstacksize(&attr, size)
	// Leave stacklo=0 and set stackhi=size; mstart will do the rest.
	ts.g.stackhi = uintptr(size)

	err = _cgo_try_pthread_create(&p, &attr, unsafe.Pointer(threadentry_trampolineABI0), ts)

	pthread_sigmask(SIG_SETMASK, &oset, nil)

	if err != 0 {
		print("fakecgo: pthread_create failed: ")
		println(err)
		abort()
	}
}

// threadentry_trampolineABI0 maps the C ABI to Go ABI then calls the Go function
//
//go:linkname x_threadentry_trampoline threadentry_trampoline
var x_threadentry_trampoline byte
var threadentry_trampolineABI0 = &x_threadentry_trampoline

//go:nosplit
//go:norace
func threadentry(v unsafe.Pointer) unsafe.Pointer {
	ts := *(*ThreadStart)(v)
	free(v)

	setg_trampoline(setg_func, uintptr(unsafe.Pointer(ts.g)))

	// faking funcs in go is a bit a... involved - but the following works :)
	fn := uintptr(unsafe.Pointer(&ts.fn))
	(*(*func())(unsafe.Pointer(&fn)))()

	return nil
}

// here we will store a pointer to the provided setg func
var setg_func uintptr

//go:nosplit
//go:norace
func x_cgo_init(g *G, setg uintptr) {
	var size size_t

	setg_func = setg

	size = pthread_get_stacksize_np(pthread_self())
	g.stacklo = uintptr(unsafe.Add(unsafe.Pointer(&size), -size+4096))
}
