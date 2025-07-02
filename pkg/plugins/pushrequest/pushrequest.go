package pushrequest

import (
	"context"
	"fmt"
	"unsafe"

	"github.com/grafana/loki/pkg/push"
	"github.com/tetratelabs/wazero/api"

	"github.com/grafana/loki/v3/pkg/plugins/host"
)

//go:generate go run ../bindgen/bindgen.go github.com/grafana/loki/v3/pkg/plugins/pushrequest PluginPushRequest
type PluginPushRequest interface {
	IterateLines(ctx context.Context, m api.Module, reqPtr uint64)
	AddStructuredMetadata(reqPtr uint64, streamIdx, entryIdx uint64, name, value string)
}

type HostPluginPushRequest struct {
	Exchange *host.Exchange
}

func (h HostPluginPushRequest) IterateLines(ctx context.Context, m api.Module, reqPtr uint64) {
	pushReq := (*push.PushRequest)(unsafe.Pointer(uintptr(reqPtr)))
	processLine := m.ExportedFunction("process_line")
	if processLine == nil {
		return
	}

	stack := make([]uint64, 4)
	for streamIdx, stream := range pushReq.Streams {
		for entryIdx, entry := range stream.Entries {
			stack[0] = reqPtr
			stack[1] = uint64(streamIdx)
			stack[2] = uint64(entryIdx)
			stack[3] = h.Exchange.PushString(m, entry.Line)

			err := processLine.CallWithStack(ctx, stack)
			if err != nil {
				panic(fmt.Errorf("Failed to call process_label: %w\n", err))
			}
		}
	}
}

func (h HostPluginPushRequest) AddStructuredMetadata(reqPtr uint64, streamIdx, entryIdx uint64, name, value string) {
	pushReq := (*push.PushRequest)(unsafe.Pointer(uintptr(reqPtr)))
	if len(pushReq.Streams) <= int(streamIdx) {
		panic(fmt.Errorf("invalid stream index %d, only %d streams available", streamIdx, len(pushReq.Streams)))
	}
	if len(pushReq.Streams[streamIdx].Entries) <= int(entryIdx) {
		panic(fmt.Errorf("invalid entry index %d for stream %d, only %d entries available", entryIdx, streamIdx, len(pushReq.Streams[streamIdx].Entries)))
	}

	stream := &pushReq.Streams[streamIdx]
	entry := &stream.Entries[entryIdx]

	if entry.StructuredMetadata == nil {
		entry.StructuredMetadata = make([]push.LabelAdapter, 0, 1)
	}

	entry.StructuredMetadata = append(entry.StructuredMetadata, push.LabelAdapter{
		Name:  name,
		Value: value,
	})
}
