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
	"sync"
	"unsafe"
)

// Define constants matching Fluent Bit core
const (
	FLB_ERROR = C.FLB_ERROR
	FLB_OK    = C.FLB_OK
	FLB_RETRY = C.FLB_RETRY

	FLB_PROXY_OUTPUT_PLUGIN = C.FLB_PROXY_OUTPUT_PLUGIN
	FLB_PROXY_GOLANG        = C.FLB_PROXY_GOLANG
	FLB_OUTPUT_LOGS         = C.FLB_OUTPUT_LOGS
	FLB_OUTPUT_METRICS      = C.FLB_OUTPUT_METRICS
	FLB_OUTPUT_TRACES       = C.FLB_OUTPUT_TRACES

	// Configuration map property types.
	FLB_CONFIG_MAP_STR        = C.FLB_CONFIG_MAP_STR
	FLB_CONFIG_MAP_STR_PREFIX = C.FLB_CONFIG_MAP_STR_PREFIX
	FLB_CONFIG_MAP_INT        = C.FLB_CONFIG_MAP_INT
	FLB_CONFIG_MAP_BOOL       = C.FLB_CONFIG_MAP_BOOL
	FLB_CONFIG_MAP_DOUBLE     = C.FLB_CONFIG_MAP_DOUBLE
	FLB_CONFIG_MAP_SIZE       = C.FLB_CONFIG_MAP_SIZE
	FLB_CONFIG_MAP_TIME       = C.FLB_CONFIG_MAP_TIME
	FLB_CONFIG_MAP_DEPRECATED = C.FLB_CONFIG_MAP_DEPRECATED
	FLB_CONFIG_MAP_CLIST      = C.FLB_CONFIG_MAP_CLIST
	FLB_CONFIG_MAP_CLIST_1    = C.FLB_CONFIG_MAP_CLIST_1
	FLB_CONFIG_MAP_CLIST_2    = C.FLB_CONFIG_MAP_CLIST_2
	FLB_CONFIG_MAP_CLIST_3    = C.FLB_CONFIG_MAP_CLIST_3
	FLB_CONFIG_MAP_CLIST_4    = C.FLB_CONFIG_MAP_CLIST_4
	FLB_CONFIG_MAP_SLIST      = C.FLB_CONFIG_MAP_SLIST
	FLB_CONFIG_MAP_SLIST_1    = C.FLB_CONFIG_MAP_SLIST_1
	FLB_CONFIG_MAP_SLIST_2    = C.FLB_CONFIG_MAP_SLIST_2
	FLB_CONFIG_MAP_SLIST_3    = C.FLB_CONFIG_MAP_SLIST_3
	FLB_CONFIG_MAP_SLIST_4    = C.FLB_CONFIG_MAP_SLIST_4
	FLB_CONFIG_MAP_VARIANT    = C.FLB_CONFIG_MAP_VARIANT

	// Configuration map property flags.
	FLB_CONFIG_MAP_MULT        = C.FLB_CONFIG_MAP_MULT
	FLB_CONFIG_MAP_DYNAMIC_ENV = C.FLB_CONFIG_MAP_DYNAMIC_ENV
)

// Local type to define a plugin definition
type FLBPluginProxyDef C.struct_flb_plugin_proxy_def
type FLBOutPlugin C.struct_flbgo_output_plugin

// ConfigMap describes a single typed configuration property that a plugin
// exposes. It mirrors the public registration fields of the C struct flb_config_map.
type ConfigMap struct {
	// Type is one of the FLB_CONFIG_MAP_* property types.
	Type int
	// Name is the property identifier as written in the configuration.
	Name string
	// DefValue is the default value applied when the property is not set.
	DefValue string
	// Flags is a bitmask of FLB_CONFIG_MAP_* flags.
	Flags int
	// Desc is a human readable description of the property.
	Desc string
}

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
	p.event_type = 0
	return 0
}

func FLBPluginRegisterWithEventType(def unsafe.Pointer, eventType int, name, desc string) int {
	p := (*FLBPluginProxyDef)(def)
	p._type = FLB_PROXY_OUTPUT_PLUGIN
	p.proxy = FLB_PROXY_GOLANG
	p.flags = 0
	p.name = C.CString(name)
	p.description = C.CString(desc)
	p.event_type = C.int(eventType)
	return 0
}

func FLBPluginRegisterWithConfigMap(def unsafe.Pointer, name, desc string, cmap []ConfigMap) int {
	p := (*FLBPluginProxyDef)(def)
	p._type = FLB_PROXY_OUTPUT_PLUGIN
	p.proxy = FLB_PROXY_GOLANG
	p.flags = 0
	p.name = C.CString(name)
	p.description = C.CString(desc)
	p.event_type = 0
	setConfigMap(p, cmap)
	return 0
}

func FLBPluginRegisterWithEventTypeAndConfigMap(def unsafe.Pointer, eventType int, name, desc string, cmap []ConfigMap) int {
	p := (*FLBPluginProxyDef)(def)
	p._type = FLB_PROXY_OUTPUT_PLUGIN
	p.proxy = FLB_PROXY_GOLANG
	p.flags = 0
	p.name = C.CString(name)
	p.description = C.CString(desc)
	p.event_type = C.int(eventType)
	setConfigMap(p, cmap)
	return 0
}

// setConfigMap attaches a typed configuration schema to the plugin definition.
func setConfigMap(p *FLBPluginProxyDef, cmap []ConfigMap) {
	if len(cmap) == 0 {
		return
	}

	cfg := (*C.struct_flb_config_map)(C.calloc(C.size_t(len(cmap)+1), C.sizeof_struct_flb_config_map))
	entries := (*[1 << 28]C.struct_flb_config_map)(unsafe.Pointer(cfg))[:len(cmap):len(cmap)]
	for i, m := range cmap {
		entries[i]._type = C.int(m.Type)
		entries[i].name = C.CString(m.Name)
		entries[i].flags = C.int(m.Flags)
		entries[i].def_value = C.CString(m.DefValue)
		entries[i].desc = C.CString(m.Desc)
	}

	p.config_map = cfg
}

// Release resources allocated by the plugin initialization
func FLBPluginUnregister(def unsafe.Pointer) {
	p := (*FLBPluginProxyDef)(def)
	C.free(unsafe.Pointer(p.name))
	C.free(unsafe.Pointer(p.description))
}

func FLBPluginConfigKey(plugin unsafe.Pointer, key string) string {
	_key := C.CString(key)
	value := C.GoString(C.output_get_property(_key, plugin))
	C.free(unsafe.Pointer(_key))
	return value
}

var contexts sync.Map

// FLBPluginSetContext sets the context for plugin to ctx.
//
// Limit FLBPluginSetContext calls to once per plugin instance for best performance.
func FLBPluginSetContext(plugin unsafe.Pointer, ctx interface{}) {
	// Allocate a byte of memory in the C heap and fill it with '\0',
	// then convert its pointer into the C type void*, represented by unsafe.Pointer.
	// The C string is not managed by Go GC, so it will not be freed automatically.
	i := unsafe.Pointer(C.CString(""))
	// uintptr(i) returns the memory address of i, which is unique in the heap.
	contexts.Store(uintptr(i), ctx)
	p := (*FLBOutPlugin)(plugin)
	p.context.remote_context = i
}

// FLBPluginGetContext reads the context associated with proxyCtx.
func FLBPluginGetContext(proxyCtx unsafe.Pointer) interface{} {
	v, _ := contexts.Load(uintptr(proxyCtx))
	return v
}
