//  Fluent Bit Go!
//  ==============
//  Copyright (C) 2015-2017 Treasure Data Inc.
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.
//

package output

/*
#include <stdlib.h>
#include "flb_plugin.h"
#include "flb_output.h"
*/
import "C"
import (
	"unsafe"
)

// Define constants matching Fluent Bit core
const (
	FLB_ERROR = C.FLB_ERROR
	FLB_OK    = C.FLB_OK
	FLB_RETRY = C.FLB_RETRY

	FLB_PROXY_OUTPUT_PLUGIN = C.FLB_PROXY_OUTPUT_PLUGIN
	FLB_PROXY_GOLANG        = C.FLB_PROXY_GOLANG
)

// Local type to define a plugin definition
type FLBPluginProxyDef C.struct_flb_plugin_proxy_def
type FLBOutPlugin C.struct_flbgo_output_plugin

// When the FLBPluginInit is triggered by Fluent Bit, a plugin context
// is passed and the next step is to invoke this FLBPluginRegister() function
// to fill the required information: type, proxy type, flags name and
// description.
func FLBPluginRegister(def unsafe.Pointer, name, desc string) int {
	p := (*FLBPluginProxyDef)(def)
	p._type = FLB_PROXY_OUTPUT_PLUGIN
	p.proxy = FLB_PROXY_GOLANG
	p.flags = 0
	p.name = C.CString(name)
	p.description = C.CString(desc)
	return 0
}

// Release resources allocated by the plugin initialization
func FLBPluginUnregister(def unsafe.Pointer) {
	p := (*FLBPluginProxyDef)(def)
	C.free(unsafe.Pointer(p.name))
	C.free(unsafe.Pointer(p.description))
}

func FLBPluginConfigKey(plugin unsafe.Pointer, key string) string {
	_key := C.CString(key)
	return C.GoString(C.output_get_property(_key, plugin))
}

var contexts []interface{}

func FLBPluginSetContext(plugin unsafe.Pointer, ctx interface{}) {
	i := len(contexts)
	contexts = append(contexts, ctx)
	p := (*FLBOutPlugin)(plugin)
	p.context.remote_context = unsafe.Pointer(uintptr(i))
}

func FLBPluginGetContext(i unsafe.Pointer) interface{} {
	return contexts[int(uintptr(i))]
}
