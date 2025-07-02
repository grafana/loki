package host

import (
	"context"
	"fmt"
	"log"
	"unsafe"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

type Exchange struct {
	allStringImportArgs []string
	offset              uint32
}

func NewExchange() *Exchange {
	exchange := Exchange{}
	exchange.Reset()
	return &exchange
}

func (e *Exchange) StringImportArg() string {
	if len(e.allStringImportArgs) == 0 {
		panic("No string arguments provided before calling StringImportArg")
	}
	var arg string
	arg, e.allStringImportArgs = e.allStringImportArgs[0], e.allStringImportArgs[1:]
	return arg
}

func (e *Exchange) StringImportArgs() []string {
	args := e.allStringImportArgs
	e.allStringImportArgs = nil
	return args
}

func (e *Exchange) AddImports(builder wazero.HostModuleBuilder) {
	builder.
		NewFunctionBuilder().
		WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, m api.Module, stack []uint64) {
			offset := api.DecodeU32(stack[0])
			byteCount := api.DecodeU32(stack[1])
			e.allStringImportArgs = append(e.allStringImportArgs, getString(m, offset, byteCount))
		}), []api.ValueType{api.ValueTypeI32, api.ValueTypeI32}, []api.ValueType{}).
		Export("pushString_import")
}

func (e *Exchange) Reset() {
	e.allStringImportArgs = make([]string, 0)
	e.offset = 3000 // some safe offset to start writing strings
}

func getString(m api.Module, offset uint32, byteCount uint32) string {
	buf, ok := m.Memory().Read(offset, byteCount)
	name := string(buf)
	if !ok {
		log.Panicf("Memory.Read(%d, %d) out of range", offset, byteCount)
	}
	return name
}

func (e *Exchange) PushString(m api.Module, s string) uint64 {
	unsafeByteSlice := unsafe.Slice(unsafe.StringData(s), len(s))
	ok := m.Memory().Write(e.offset, unsafeByteSlice)
	if !ok {
		fmt.Printf("Failed to write '%s' to memory\n", s)
		return 0
	}

	// offset is high 32 bits, size is low 32 bits
	res := (uint64(e.offset) << 32) | uint64(len(unsafeByteSlice))
	e.offset += uint32(len(unsafeByteSlice))
	return res
}
